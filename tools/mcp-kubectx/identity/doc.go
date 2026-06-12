// Package identity mints and validates the 16-hex identifiers that
// scope orphan-sweep ownership: the per-`serve` instance id tagged
// on every provisioned ServiceAccount, and the persistent
// per-user-per-env host id that bounds a sweep to resources its own
// host provisioned.
//
// The host id is persisted under the mcp-kubectx state dir as
// `host.id` or `guest.id` depending on the env tag. The id is
// per-env because the state dir is shared with the Lima guest
// through the bind mount while sockets are not: each env's liveness
// discovery can only vouch for its own env's serves, so a shared id
// would let a host-side sweep delete a concurrent guest serve's live
// resources (and vice versa).
package identity
