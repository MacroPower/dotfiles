package main

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
)

// maxLLMsProbeBytes caps the body drained from an llms.txt probe.
// The body is discarded; the limit only bounds the read used to enable
// connection reuse.
const maxLLMsProbeBytes = 64 * 1024

// findLLMsTxt returns the full llms.txt URL for the given origin when
// one is published, or an empty string otherwise. Results are cached
// per-origin (positive and negative) using the handler's llmsCache.
//
// The cache distinguishes "never probed" (Get returns ok=false) from
// "probed and not present" (ok=true, value=""), so a missing llms.txt
// is recorded once and subsequent calls reuse the negative result
// without issuing another HTTP request.
func (h *fetchHandler) findLLMsTxt(ctx context.Context, origin string) string {
	cached, ok := h.llmsCache.Get(origin)
	if ok {
		return cached
	}

	llmsURL := h.fetchLLMsTxt(ctx, origin)

	h.llmsCache.Add(origin, llmsURL)

	return llmsURL
}

// fetchLLMsTxt probes origin + "/llms.txt" through the handler's HTTP
// client and returns the full URL when the response is 200 OK with an
// acceptable text content type. All other outcomes return an empty
// string. The probe sets the handler's User-Agent so origins can serve
// a UA-specific llms.txt and identify the request in their logs.
func (h *fetchHandler) fetchLLMsTxt(ctx context.Context, origin string) string {
	llmsURL := origin + "/llms.txt"

	//nolint:gosec // origin is derived from a validated URL
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmsURL, http.NoBody)
	if err != nil {
		return ""
	}

	req.Header.Set("User-Agent", h.userAgent)

	resp, err := h.client.Do(req) //nolint:gosec // tainted from above
	if err != nil {
		return ""
	}
	defer h.closeBody(ctx, resp.Body, llmsURL)

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	if !acceptableLLMsContentType(resp.Header.Get("Content-Type")) {
		return ""
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxLLMsProbeBytes))

	return llmsURL
}

// acceptableLLMsContentType reports whether the given Content-Type header
// is plausible for an llms.txt file. Empty values are accepted (some
// origins serve raw text without a Content-Type). Otherwise, the media
// type must be in the text/* family and not text/html, which guards
// against SPA catch-all routes that serve the index page for any path.
func acceptableLLMsContentType(ct string) bool {
	if ct == "" {
		return true
	}

	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}

	return strings.HasPrefix(mediaType, "text/") && mediaType != "text/html"
}
