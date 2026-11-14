// Package httpbody closes the bodies of HTTP responses. A caller closes a
// response body, or a reader that a client opened over one, after it reads
// the content. A failed close goes to the debug log and not to the caller,
// because the caller already holds what it read.
package httpbody
