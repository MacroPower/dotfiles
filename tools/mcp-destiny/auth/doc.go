// Package auth signs a user in to Bungie through Agg's Destiny Vault
// Manager and keeps the tokens of the sign-in usable. The Bungie access
// token authenticates each request to the Bungie platform API. The session
// token authenticates each request that reads or writes the data of an
// account in the service of Agg's Destiny Vault Manager. The token file also
// records the Destiny membership of the signed-in account.
//
// # Sign-in
//
// Bungie redirects a sign-in to the one URL that the deployment registered,
// which is the web client. A local process cannot receive that redirect.
// The sign-in therefore issues an OAuth state that the web client does not
// recognize. The web client refuses the redirect and leaves the
// authorization code unused in the address bar. The user copies that URL
// back, and a token broker exchanges the code for tokens. The broker is
// the service of Agg's Destiny Vault Manager, because the service holds the
// OAuth client secret.
//
// A caller sends the user to the authorization URL and passes the pasted
// URL, or the bare code, back to complete the sign-in. While an
// authorization URL of the same [*Auth] waits for its sign-in, the sign-in
// rejects a pasted URL that carries the state of a different attempt.
// Before each request that carries the access token or the session, the
// caller asks for the tokens.
//
// # Token File
//
// The token file holds the tokens of one sign-in. Every server process and
// the login subcommand share one file. A save writes a temporary file and
// renames it, so a reader sees either the previous tokens or the saved
// tokens. Each sign-in, refresh, membership record, and sign-out holds an
// exclusive advisory lock on a lock file beside the token file. The lock
// serializes these changes across processes.
//
// # Refresh
//
// A request for the tokens refreshes them first when the access token is
// within a minute of its expiry or when the file lacks a session. A caller
// also asks for a refresh after Bungie rejects the access token or the
// service rejects the session. When another process already replaced the
// tokens, the refresh returns the tokens from the file. A refresh keeps the
// membership that the file records.
//
// A refresh fails with [ErrNotSignedIn] when the refresh token expired,
// when the file has no refresh token, or when Bungie rejects the refresh
// token. The user then signs in again.
package auth
