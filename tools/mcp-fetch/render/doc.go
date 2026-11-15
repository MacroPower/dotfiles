// Package render executes a page's JavaScript in an embedded headless
// browser (gost-dom with the pure-Go sobek engine) and serializes the
// resulting DOM, so client-rendered pages (SPAs) produce readable
// content instead of an empty application shell.
//
// Rendering is best-effort: gost-dom implements a subset of the web
// platform, so real-world scripts may throw on missing APIs or drive
// the simulated event loop into a state it refuses to run (a
// setInterval task, for example, re-queues itself forever and makes the
// clock panic). Script errors and event-loop panics never fail a
// render: the DOM is serialized as it stands. Only failing to open the
// page, an unrecovered browser panic, or the render budget expiring
// return an error. On budget expiry the browser goroutine is abandoned
// until the script engine yields, because sobek executions are not
// context-interruptible.
//
// Scripts compile without their source maps. sobek fails a script
// whose source map does not load, and it cannot load one over HTTP.
// With source maps on, the `//# sourceMappingURL=` comment at the end
// of a bundle stops the bundle from running.
//
// Every subresource request (scripts, fetch/XHR) is routed through a
// policy handler that enforces URL rules and resource caps on top of
// the shared HTTP client, so rendered pages obey the same network
// policy as plain fetches. Subresources are limited to GET and HEAD
// with an allowlisted header set, so page scripts cannot make
// state-changing requests or smuggle headers through that client. The
// caller checks robots.txt for the top-level document before
// rendering; subresource requests are not robots-checked themselves
// (browsers do not consult robots.txt for subresources), but their
// redirects follow the shared client's policy, which re-validates
// rules on every hop and robots.txt on cross-host hops.
//
// The resource caps bound the number of subresource requests in one
// render and the size of each response. A render skips every
// subresource whose response is larger than the byte cap, because a
// truncated script does not parse. The browser logs a skipped script
// only as a load that ended in a 403 status. The policy handler
// therefore logs the cap at Warn, and [Result.Oversized] names each
// skipped subresource for the caller.
package render
