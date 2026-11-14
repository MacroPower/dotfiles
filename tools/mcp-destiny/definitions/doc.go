// Package definitions builds the Destiny definitions that the mcp-destiny
// server reads. The definitions give names, types, buckets, and stats to
// the hashes of a Bungie profile and of the activity history. They also
// name the activities of a character and the vendors with their stock.
// They hold the plug sets of each weapon for the roll ratings.
//
// The definitions come from the service of Agg's Destiny Vault Manager. The
// artifact index of the service names the manifest version and lists the
// files of each definition artifact. The loader downloads the artifacts that
// the index lists and keeps only the fields that the server reads. The index
// must list the item artifact and the tables of the buckets, the damage
// types, the classes, and the stats. The other artifacts, such as those of
// the activities, the vendors, and the plug sets, are optional. The
// definitions leave the matching maps empty when the index lacks them.
//
// # Item Names
//
// The item artifact leaves out dummy items, classified items, and items
// without a name. The service names the dummy items that the vendors sell
// and the dummy rewards of the activities in artifacts of their own. The
// definitions name such an item from those artifacts. The service also
// lists the hashes of the items that it withholds. A caller can therefore
// tell an item that the service withholds from an item that the manifest
// version lacks. The game can hold an item that the manifest version
// lacks when Bungie added the item in a later version.
//
// A name store names the other items. It looks each such item up in the
// live manifest of Bungie, one request for each item. A caller passes the
// definitions and the item hashes of one call to the store. The store
// returns a copy of the definitions that also names the items that Bungie
// names. The copy records the outcome of each lookup, so a caller can say
// why an item still has no name. A call sends a bounded number of lookups
// and waits a bounded time for them, so a caller that renders many items
// stays fast.
//
// # Loading
//
// A caller makes one loader for the process and asks it for the definitions
// on each call that needs them. The loader serves the loaded definitions
// from memory for one hour and then asks the service for the manifest
// version again. It builds the definitions again only when the service
// publishes another version.
//
// # Cache
//
// The item artifact is tens of megabytes of JSON, so the loader keeps one
// cache file for each manifest version in a cache directory. A new process
// reads the file of the current version instead of downloading the
// artifacts. The name of each cache file carries the layout of the cached
// fields, so a process never decodes a file of another layout. A write of a
// cache file removes the files of every other version and every other
// layout. The files of an older layout therefore leave the directory with
// the first write after an upgrade.
//
// The name store keeps its own cache file for each manifest version in the
// same directory, with the answers of Bungie to its lookups. It writes the
// file after each call that got a new answer, and the write removes the
// name files of the other versions and layouts. The store keeps no failed
// lookup, so the next call looks the item up again.
//
// Several processes can share the cache directory, such as the servers of
// two sessions. Before a name store looks an item up, it reads the answers
// that the other processes saved to its file. A write adds the answers of
// the store to those that the file holds. A lock serializes the writes, so
// no process drops the answers of another. Each write of a cache file goes
// to a temporary file of its own, and that file then replaces the cache
// file. A reader therefore never sees half a file.
package definitions
