package llmstxt

import (
	"context"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
)

const (
	// maxProbeBytes caps the body drained from an llms.txt probe. The
	// body is discarded; the limit only bounds the read used to enable
	// connection reuse.
	maxProbeBytes = 64 * 1024

	cacheSize = 128
	cacheTTL  = time.Hour
)

// Finder probes origins for a published llms.txt, caching positive and
// negative results per origin.
type Finder struct {
	client    *http.Client
	cache     *expirable.LRU[string, string]
	log       *slog.Logger
	userAgent string
}

// Option configures a [Finder] via [New].
//
// Functions of this type:
//   - [WithLogger]
type Option func(*Finder)

// WithLogger sets the structured logger used for body-close warnings.
// See [Option].
func WithLogger(l *slog.Logger) Option {
	return func(f *Finder) { f.log = l }
}

// New constructs a [*Finder] that probes through client and identifies
// itself with userAgent so origins can serve a UA-specific llms.txt.
// The client should be the same one used for content fetches.
func New(client *http.Client, userAgent string, opts ...Option) *Finder {
	f := &Finder{
		client:    client,
		userAgent: userAgent,
		log:       slog.New(slog.DiscardHandler),
		cache:     expirable.NewLRU[string, string](cacheSize, nil, cacheTTL),
	}

	for _, opt := range opts {
		opt(f)
	}

	return f
}

// Find returns the full llms.txt URL for origin when one is published,
// or an empty string otherwise. Results are cached per-origin (positive
// and negative).
//
// The cache distinguishes "never probed" (Get returns ok=false) from
// "probed and not present" (ok=true, value=""), so a missing llms.txt
// is recorded once and subsequent calls reuse the negative result
// without issuing another HTTP request.
func (f *Finder) Find(ctx context.Context, origin string) string {
	cached, ok := f.cache.Get(origin)
	if ok {
		return cached
	}

	llmsURL := f.probe(ctx, origin)

	f.cache.Add(origin, llmsURL)

	return llmsURL
}

// probe requests origin + "/llms.txt" and returns the full URL when the
// response is 200 OK with an acceptable text content type. All other
// outcomes return an empty string.
func (f *Finder) probe(ctx context.Context, origin string) string {
	llmsURL := origin + "/llms.txt"

	//nolint:gosec // origin is derived from a validated URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmsURL, http.NoBody)
	if err != nil {
		return ""
	}

	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req) //nolint:gosec // tainted from above
	if err != nil {
		return ""
	}
	defer f.closeBody(ctx, resp.Body, llmsURL)

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	if !acceptableContentType(resp.Header.Get("Content-Type")) {
		return ""
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxProbeBytes))

	return llmsURL
}

func (f *Finder) closeBody(ctx context.Context, body io.Closer, label string) {
	err := body.Close()
	if err != nil {
		f.log.WarnContext(ctx, "closing response body",
			slog.String("url", label),
			slog.Any("error", err),
		)
	}
}

// acceptableContentType reports whether the given Content-Type header is
// plausible for an llms.txt file. Empty values are accepted (some
// origins serve raw text without a Content-Type). Otherwise, the media
// type must be in the text/* family and not text/html, which guards
// against SPA catch-all routes that serve the index page for any path.
func acceptableContentType(ct string) bool {
	if ct == "" {
		return true
	}

	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}

	return strings.HasPrefix(mediaType, "text/") && mediaType != "text/html"
}
