package dvm

const (
	// DefaultUserAgent is the User-Agent header that a client sends when no
	// option sets one.
	DefaultUserAgent = defaultUserAgent

	// MaxResponseBytes caps a JSON API response body.
	MaxResponseBytes = maxResponseBytes

	// MaxStatChunkLength is the longest chunk body that the service
	// accepts, in UTF-16 code units.
	MaxStatChunkLength = maxStatChunkLength
)
