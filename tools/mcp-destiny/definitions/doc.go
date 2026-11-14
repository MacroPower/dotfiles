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
package definitions
