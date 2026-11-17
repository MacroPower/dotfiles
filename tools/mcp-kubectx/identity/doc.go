// Package identity mints and validates the 16-hex identifiers that
// scope orphan-sweep ownership: the per-`serve` instance id tagged
// on every provisioned ServiceAccount, and the persistent
// per-user-per-env host id that bounds a sweep to resources its own
// host provisioned.
//
// [LoadOrCreateHost] persists the host id in [statedir.RunDir] of
// the calling process, as `host.id` or `guest.id` depending on the
// env tag. workmux mounts no part of that directory into a Lima
// guest. So no guest can change the `host.id` of host-side serves
// through the mount. Every guest can still overwrite that file with
// a kubeconfig when it runs `host select` through `workmux
// host-exec` with an --out-path of its choice. A guest-side serve
// resolves the directory in the guest. So it keeps `guest.id` on the
// disk of its Lima guest VM, and each guest VM has a `guest.id` of
// its own.
//
// The id is per-env because the liveness discovery of each env
// vouches only for the serves of that env. A shared id lets a
// host-side sweep delete the live resources of a concurrent guest
// serve, and the other way around. Serves of both envs can resolve
// the same state dir, such as when a serve in a Lima guest runs
// without WM_SANDBOX_GUEST=1. The env tag in the file name then
// keeps the two ids apart.
package identity
