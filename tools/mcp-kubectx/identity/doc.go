// Package identity mints and validates the 16-hex identifiers that
// scope orphan-sweep ownership: the per-`serve` instance id tagged
// on every provisioned ServiceAccount, and the persistent
// per-user-per-env host id that bounds a sweep to resources its own
// host provisioned.
//
// [LoadOrCreateHost] persists the host id in [statedir.RunDir] of
// the calling process, as `host.key` or `guest.key` depending on the
// env tag. workmux mounts no part of that directory into a Lima
// guest. A guest-side serve resolves the directory in the guest. So
// it keeps `guest.key` on the disk of its Lima guest VM, and each
// guest VM has a `guest.key` of its own.
//
// The host id is the secret of the serves that hold it. The labels
// of each provisioned resource and the names of the provision
// records carry only its [Digest]. The `host` subcommands take the
// host id from their caller and derive the digest. So a caller acts
// on the resources of a host id only when it holds the id. A Lima
// guest that runs the `host` subcommands through `workmux
// host-exec` holds only the `guest.key` of its own VM.
//
// Builds before [Digest] kept the id in `host.id` or `guest.id` and
// put the plain id on each label, where every guest with a `view`
// token can read it. [HostPath] names another file, so each env
// mints a fresh id, and no id of those builds reaches a resource of
// this build.
//
// The id is per-env because the liveness discovery of each env
// vouches only for the serves of that env. A shared id lets a
// host-side sweep delete the live resources of a concurrent guest
// serve, and the other way around. Serves of both envs can resolve
// the same state dir, such as when a serve in a Lima guest runs
// without WM_SANDBOX_GUEST=1. The env tag in the file name then
// keeps the two ids apart.
package identity
