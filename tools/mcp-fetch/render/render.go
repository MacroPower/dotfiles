package render

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gost-dom/browser"
	"github.com/gost-dom/browser/html"
	"github.com/gost-dom/browser/scripting/sobekengine"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
)

const (
	defaultBudget              = 10 * time.Second
	defaultMaxSubresources     = 50
	defaultMaxSubresourceBytes = 2 * 1024 * 1024 // 2 MiB

	// serializeGraceDivisor reserves a fraction of the remaining budget
	// for DOM serialization: event pumping stops at deadline-minus-grace
	// so the serialized DOM still makes it out before the budget expires.
	serializeGraceDivisor = 5

	// pumpTaskBudget caps how many event-loop tasks one pump round may
	// execute. The clock's own ProcessEvents loops forever on pages
	// that use setInterval (a repeating task re-queues itself), so the
	// pump bounds every round by task count as well as by deadline.
	pumpTaskBudget = 500
)

// ErrRenderFailed is returned when the page could not be rendered at
// all; the caller should fall back to the non-rendered content.
var ErrRenderFailed = errors.New("JavaScript rendering unavailable")

// Renderer executes pages in a gost-dom browser with the sobek script
// engine. A single Renderer is safe for concurrent use: every Render
// call creates a fresh browser with its own script context.
type Renderer struct {
	client              *http.Client
	rules               *rules.Rules
	log                 *slog.Logger
	userAgent           string
	budget              time.Duration
	maxSubresources     int
	maxSubresourceBytes int64
}

// Option configures a [Renderer] via [New].
//
// Functions of this type:
//   - [WithRules]
//   - [WithLogger]
//   - [WithUserAgent]
//   - [WithBudget]
//   - [WithMaxSubresources]
//   - [WithMaxSubresourceBytes]
type Option func(*Renderer)

// WithRules sets the URL allow/deny rules applied to every subresource
// request. See [Option].
func WithRules(r *rules.Rules) Option {
	return func(rd *Renderer) { rd.rules = r }
}

// WithLogger sets the structured logger. See [Option].
func WithLogger(l *slog.Logger) Option {
	return func(rd *Renderer) { rd.log = l }
}

// WithUserAgent sets the User-Agent header on subresource requests. See
// [Option].
func WithUserAgent(ua string) Option {
	return func(rd *Renderer) { rd.userAgent = ua }
}

// WithBudget sets the wall-clock budget for a whole render, including
// event settling and DOM serialization (default 10s). See [Option].
func WithBudget(d time.Duration) Option {
	return func(rd *Renderer) { rd.budget = d }
}

// WithMaxSubresources caps how many subresource requests one render may
// issue (default 50). See [Option].
func WithMaxSubresources(n int) Option {
	return func(rd *Renderer) { rd.maxSubresources = n }
}

// WithMaxSubresourceBytes caps the bytes read from each subresource
// response (default 2 MiB). See [Option].
func WithMaxSubresourceBytes(n int64) Option {
	return func(rd *Renderer) { rd.maxSubresourceBytes = n }
}

// New constructs a [*Renderer] that performs subresource requests with
// the given client, inheriting its timeout and redirect policy.
func New(client *http.Client, opts ...Option) *Renderer {
	r := &Renderer{
		client:              client,
		log:                 slog.New(slog.DiscardHandler),
		budget:              defaultBudget,
		maxSubresources:     defaultMaxSubresources,
		maxSubresourceBytes: defaultMaxSubresourceBytes,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Seed carries the already-fetched top-level document so a render does
// not hit the network a second time for the page itself.
type Seed struct {
	ContentType string
	Body        []byte
}

// Render opens pageURL in a fresh browser, executes its scripts, and
// returns the serialized post-JS DOM. pageURL must be the final URL
// after redirects so relative subresource URLs resolve correctly; seed
// is served for that URL instead of refetching it.
//
// Script errors and event-loop panics are logged and do not fail the
// render: the DOM is serialized as it stands. Only failing to open the
// page, an unrecovered browser panic, or the budget expiring return an
// error (wrapping [ErrRenderFailed]) that tells the caller to fall
// back to the non-rendered content.
func (r *Renderer) Render(ctx context.Context, pageURL string, seed Seed) ([]byte, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parsing page URL: %w", ErrRenderFailed, err)
	}

	// Normalize an empty path to "/" so the browser's request for the
	// page matches the policy handler's seed check byte for byte.
	if u.Path == "" {
		u.Path = "/"
	}

	pageURL = u.String()

	ctx, cancel := context.WithTimeout(ctx, r.budget)
	defer cancel()

	type outcome struct {
		err  error
		html []byte
	}

	// Buffered so the browser goroutine can finish (and release its
	// resources) even when the budget expired and nobody receives.
	done := make(chan outcome, 1)

	go func() {
		var out outcome

		// Browser panics are contained at the goroutine boundary so a
		// crashing page fails the render instead of the process.
		defer func() {
			if p := recover(); p != nil {
				out = outcome{err: fmt.Errorf("browser panic: %v", p)}
			}

			done <- out
		}()

		doc, err := r.render(ctx, pageURL, seed)
		out = outcome{html: doc, err: err}
	}()

	select {
	case out := <-done:
		if out.err != nil {
			return nil, fmt.Errorf("%w: %w", ErrRenderFailed, out.err)
		}

		return out.html, nil

	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", ErrRenderFailed, ctx.Err())
	}
}

// render runs on its own goroutine; all browser interaction stays here
// because gost-dom requires single-goroutine window access. The caller
// recovers browser panics at the goroutine boundary.
func (r *Renderer) render(ctx context.Context, pageURL string, seed Seed) ([]byte, error) {
	seed.Body = withCompatScript(seed.Body)

	// The watchdog interrupts the script VM when the budget expires;
	// without it a script that never yields would keep this goroutine
	// spinning after Render has already returned the budget error.
	watchdog := newInterruptWatchdog(ctx)
	defer watchdog.stop()

	b := browser.New(
		browser.WithScriptEngine(&interruptEngine{
			inner:    sobekengine.DefaultEngine(),
			register: watchdog.register,
		}),
		browser.WithHandler(newPolicyHandler(ctx, r, pageURL, seed)),
		browser.WithContext(ctx),
		browser.WithLogger(r.log),
	)
	defer b.Close()

	win, err := b.Open(pageURL)
	if err != nil {
		return nil, fmt.Errorf("opening page: %w", err)
	}

	r.pumpEvents(ctx, win)

	return []byte(win.Document().DocumentElement().OuterHTML()), nil
}

// pumpEvents settles pending async work: in-flight fetch/XHR responses
// and queued timer callbacks. gost-dom runs the event loop in simulated
// time under caller control, and its own settling primitives are unsafe
// on real-world pages: ProcessEvents loops forever when a setInterval
// task re-queues itself, and Advance panics when the task queue stops
// shrinking. The pump therefore alternates bounded task execution
// (ProcessEventsWhile with an iteration cap and deadline check; the
// predicate runs before every task) with Advance(0) rounds that flush
// promise microtasks, all panic-recovered: by the time anything fails
// the initial scripts have run, and the caller serializes the DOM as it
// stands.
func (r *Renderer) pumpEvents(ctx context.Context, win html.Window) {
	// Stop pumping short of the render deadline so DOM serialization
	// still fits inside the budget.
	if deadline, ok := ctx.Deadline(); ok {
		grace := time.Until(deadline) / serializeGraceDivisor

		var cancel context.CancelFunc

		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-grace))
		defer cancel()
	}

	clock := win.Clock()

	for range 2 {
		r.pumpSafely(ctx, "microtasks", func() error { return clock.Advance(0) })
		r.pumpSafely(ctx, "tasks", func() error {
			remaining := pumpTaskBudget

			//nolint:staticcheck // deprecated upstream with no replacement; the pump bounds it (see doc comment)
			return clock.ProcessEventsWhile(ctx, func() bool {
				remaining--

				return remaining >= 0 && ctx.Err() == nil
			})
		})
	}

	r.pumpSafely(ctx, "microtasks", func() error { return clock.Advance(0) })
}

// pumpSafely runs one pump stage, containing the panics and script
// errors the simulated event loop surfaces on pages it cannot settle;
// rendering continues with whatever the DOM holds.
func (r *Renderer) pumpSafely(ctx context.Context, stage string, fn func() error) {
	defer func() {
		if p := recover(); p != nil {
			r.log.DebugContext(ctx, "render: event loop stopped",
				slog.String("stage", stage),
				slog.Any("panic", p),
			)
		}
	}()

	if ctx.Err() != nil {
		return
	}

	err := fn()
	if err != nil {
		r.log.DebugContext(ctx, "render: script errors",
			slog.String("stage", stage),
			slog.Any("error", err),
		)
	}
}
