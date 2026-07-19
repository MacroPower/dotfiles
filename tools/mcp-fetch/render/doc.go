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
package render
