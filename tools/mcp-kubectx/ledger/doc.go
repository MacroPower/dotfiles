// Package ledger keeps one provision record on disk for each
// ServiceAccount that a `serve` instance provisions. The records
// tell the orphan sweep which ServiceAccounts a dead instance left
// behind, and on which clusters.
//
// A record locates one ServiceAccount by the kubeconfig file, the
// context, the namespace, and the name. It also says whether the
// binding of the ServiceAccount is a ClusterRoleBinding. The name
// of the record file holds the host id and the instance id of the
// `serve` that owns the ServiceAccount.
//
// `host select` adds the record before it creates the
// ServiceAccount, so a `host select` that dies between the two
// steps still leaves the record. `host release` removes the record
// after the cluster holds neither the ServiceAccount nor its
// binding. A `serve` that dies without its cleanup leaves its
// records behind. A later `host sweep` reads the records of the
// instances that are not alive and that name the kubeconfig of the
// sweep. It deletes each recorded ServiceAccount and its binding on
// the cluster of the record. It removes the record only after the
// cluster holds neither resource, and it keeps every record of
// another kubeconfig.
//
// Each record is one file named <host-id>.<instance-id>.<key>.json.
// The key is a hash of the location, so the name of the file holds
// only hex characters, whatever the context is called. The file
// holds the location as JSON. [Ledger.List] skips every file that
// it cannot parse and leaves the file in place.
package ledger
