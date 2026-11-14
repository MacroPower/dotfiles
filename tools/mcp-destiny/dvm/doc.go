// Package dvm is the client of the service of Agg's Destiny Vault Manager.
// The project lives at https://github.com/agg23/destiny-inventory. The
// service stores the data that the web client of the project keeps per
// Bungie account. That data covers the item tags, the notes, the perk
// preferences, and the autotag rules with the autotag state. It also
// covers the profile changes that the web client records.
//
// The service also publishes the public Bungie credentials that the web
// client reads, and it serves the Destiny definition artifacts. It brokers
// the Bungie OAuth exchange, because it holds the OAuth client secret.
//
// The reads of tags, notes, perk preferences, and autotag rules return
// every row, removed rows included, and the writes upsert rows. A removed
// row stays in the service with its Removed field set. A caller therefore
// removes a row by writing it with the field set, and it skips the removed
// rows that a read returns.
//
// # Sessions
//
// A code exchange and a token refresh each return a session token beside
// the Bungie tokens. Every call that reads or writes the data of an account
// takes that session, and the client sends it as a bearer token. The calls
// for the configuration, the artifacts, and the tokens do not take a
// session. The service rejects a stale session, and the caller then
// refreshes the tokens to get a new one.
//
// # Errors
//
// Callers test the errors of the client against its sentinel errors with
// [errors.Is]. [ErrService] marks an answer that the client cannot use,
// such as a failure status other than the 401 of a JSON API call, or a body
// that does not decode. [ErrUnauthorized] marks a call that the service
// answered with status 401 because it rejected the session. The caller
// refreshes the tokens and retries the call with the new session. An error
// that matches [ErrUnauthorized] does not match [ErrService].
//
// [ErrGrantRejected] marks a token request that Bungie refused, such as a
// refresh with a revoked refresh token, and the user signs in again to
// recover. An error that matches [ErrGrantRejected] also matches
// [ErrService]. A failure of the transport, such as a refused connection,
// matches no sentinel.
package dvm
