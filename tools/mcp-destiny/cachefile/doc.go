// Package cachefile writes the cache files that the mcp-destiny server
// keeps in its cache directory. Several processes can share the directory,
// such as the servers of two sessions.
//
// A caller encodes one value as JSON into one cache file. Each write goes
// to a temporary file of its own, and that file then replaces the cache
// file. A reader therefore never sees half a file, and two writes never
// mix.
//
// The name of a cache file carries what makes its content stale, such as
// the layout of its fields or the version of its source. After a write,
// the caller removes the files that the same pattern matches and that the
// write did not produce. The files of an older layout therefore leave the
// directory with the first write after an upgrade.
package cachefile
