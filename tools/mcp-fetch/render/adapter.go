package render

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"

	"go.jacobcolvin.com/dotfiles/tools/mcp-fetch/rules"
)

// subresourceHeaders are the request headers forwarded on subresource
// requests. Everything else the page sets is dropped so scripts cannot
// smuggle arbitrary headers through the shared client.
var subresourceHeaders = []string{"Accept", "Accept-Language"}

// policyHandler is the [http.Handler] the browser sends every request
// through: the top-level page, script elements, and JS-initiated
// fetch/XHR. It serves the pre-fetched seed for the page itself and
// applies URL rules plus resource caps to everything else before
// performing the request with the shared validated client.
// Subresource requests are limited to GET and HEAD so page scripts
// cannot make state-changing requests to rules-allowed hosts.
type policyHandler struct {
	ctx       context.Context
	client    *http.Client
	rules     *rules.Rules
	log       *slog.Logger
	userAgent string
	pageURL   string
	seed      Seed
	perBytes  int64
	remaining atomic.Int64
}

func newPolicyHandler(ctx context.Context, r *Renderer, pageURL string, seed Seed) *policyHandler {
	h := &policyHandler{
		ctx:       ctx,
		client:    r.client,
		rules:     r.rules,
		log:       r.log,
		userAgent: r.userAgent,
		pageURL:   pageURL,
		seed:      seed,
		perBytes:  r.maxSubresourceBytes,
	}
	h.remaining.Store(int64(r.maxSubresources))

	return h
}

//nolint:contextcheck // logs deliberately carry the render context, not the browser's
func (h *policyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqURL := req.URL.String()

	if reqURL == h.pageURL && req.Method == http.MethodGet {
		if h.seed.ContentType != "" {
			w.Header().Set("Content-Type", h.seed.ContentType)
		}

		// The browser rejects anything but exactly 200 for the
		// top-level document, so the seed status is normalized here;
		// the caller already accepted the original response.
		w.WriteHeader(http.StatusOK)

		_, err := w.Write(h.seed.Body)
		if err != nil {
			h.log.DebugContext(h.ctx, "render: writing seed", slog.Any("error", err))
		}

		return
	}

	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		h.log.DebugContext(h.ctx, "render: subresource method rejected",
			slog.String("url", reqURL),
			slog.String("method", req.Method),
		)
		http.Error(w, "only GET and HEAD subresources are allowed", http.StatusMethodNotAllowed)

		return
	}

	if !rules.AllowedScheme(req.URL.Scheme) {
		h.deny(w, req, "unsupported scheme")

		return
	}

	if reason := h.rules.Check(req.URL); reason != "" {
		h.deny(w, req, reason)

		return
	}

	if h.remaining.Add(-1) < 0 {
		h.deny(w, req, "subresource limit reached")

		return
	}

	h.proxy(w, req)
}

// proxy performs the subresource request with the shared client. The
// request carries the render context, not the browser's, so the render
// budget cancels in-flight I/O: JS fetch calls inherit the browser
// context but script-element loads do not.
//
//nolint:contextcheck,gosec // deliberate render context (see above); G704 is gated by ServeHTTP's rules checks
func (h *policyHandler) proxy(w http.ResponseWriter, req *http.Request) {
	out, err := http.NewRequestWithContext(h.ctx, req.Method, req.URL.String(), http.NoBody)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	for _, name := range subresourceHeaders {
		if v := req.Header.Get(name); v != "" {
			out.Header.Set(name, v)
		}
	}

	out.Header.Set("User-Agent", h.userAgent)

	resp, err := h.client.Do(out)
	if err != nil {
		h.log.DebugContext(h.ctx, "render: subresource request",
			slog.String("url", req.URL.String()),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}

	defer func() {
		err := resp.Body.Close()
		if err != nil {
			h.log.DebugContext(h.ctx, "render: closing subresource body",
				slog.String("url", req.URL.String()),
				slog.Any("error", err),
			)
		}
	}()

	// Read one byte past the cap so an oversized resource is detected
	// and the load fails outright: serving a mid-token truncation would
	// hand the script engine corrupted JavaScript that parses as a
	// SyntaxError while the render still reports success.
	body, err := io.ReadAll(io.LimitReader(resp.Body, h.perBytes+1))
	if err != nil {
		h.log.DebugContext(h.ctx, "render: reading subresource",
			slog.String("url", req.URL.String()),
			slog.Any("error", err),
		)
		http.Error(w, err.Error(), http.StatusBadGateway)

		return
	}

	if int64(len(body)) > h.perBytes {
		h.deny(w, req, "subresource exceeds byte cap")

		return
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	w.WriteHeader(resp.StatusCode)

	_, err = w.Write(body)
	if err != nil {
		h.log.DebugContext(h.ctx, "render: writing subresource",
			slog.String("url", req.URL.String()),
			slog.Any("error", err),
		)
	}
}

func (h *policyHandler) deny(w http.ResponseWriter, req *http.Request, reason string) {
	h.log.DebugContext(h.ctx, "render: subresource denied",
		slog.String("url", req.URL.String()),
		slog.String("reason", reason),
	)
	http.Error(w, reason, http.StatusForbidden)
}
