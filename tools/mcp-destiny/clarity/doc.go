// Package clarity reads the perk descriptions of the Clarity database for
// the mcp-destiny server. Clarity is a community project that tests how
// the perks of Destiny 2 play. Its descriptions give the numbers that the
// in-game text of Bungie leaves out, such as the size and the duration of
// a damage bonus. Destiny Item Manager and the web client of Agg's
// Destiny Vault Manager show the same descriptions.
//
// The Clarity repository carries no license file. The server therefore
// downloads the database when it runs, as the two clients do, and this
// module holds no copy of it. Each output that shows a description names
// Clarity as the source.
//
// # Database
//
// The site of Clarity publishes two files. The descriptions file maps the
// item hash of the plug of each covered perk to its description. The
// versions file names the version of the descriptions file. A caller
// looks a perk up by the item hash of its plug. The database covers
// weapon perks, weapon frames, weapon and armor mods, the perks of exotic
// items, aspects, fragments, and class abilities.
//
// Clarity stores a description as lines of text runs with display
// classes. The decode joins each line into plain text that a tool can
// print under the name of the perk. The text keeps the bullets of
// Clarity. It marks a value that applies only against players and a
// value that an enhanced perk improves.
//
// An enhanced perk has a plug of its own, and Clarity gives many of them
// no entry. The web client then shows the description of the base perk,
// which it finds in a table of Destiny Item Manager. A caller of this
// package finds the base perk by name instead. Bungie gives an enhanced
// perk the name of its base perk, at times with the word Enhanced after
// it. The lookup covers the weapon traits, the origin traits, and the
// perks of the other weapon columns, such as barrels and magazines. It
// skips the weapon frames, because Bungie gives one frame name to the
// frames of several weapon types that play differently.
//
// # Store
//
// A caller keeps one store for the process and asks it for the database
// on each call that shows a description. The store checks the version of
// the site at most one time each hour, and it downloads the descriptions
// file only for another version. It keeps the downloaded file in a cache
// file in the cache directory, with its version. A new process therefore
// downloads only the versions file. Several processes can share the
// cache directory, such as the servers of two sessions, and each one
// reads the file that another downloaded.
//
// Clarity is an optional source. A check that does not succeed keeps the
// database that the store holds, so the store serves an older version in
// place of none. The store checks again after five minutes. A check that
// ends because the caller canceled its call does not count, so the next
// call checks again. When the store holds no database, it returns the
// reason, and the caller shows only the text of Bungie.
//
// # Final Patch
//
// Destiny 2 Update 9.7.0 and its hotfixes made the last changes to how
// the perks play. A description that Clarity last updated before such a
// change might describe the old behavior. The package holds the list of
// the perks and the exotic items that those patches changed, which comes
// from the patch notes of Bungie. A caller asks a description whether a
// patch changed its perk after the last upload, and it says so beside the
// text. Clarity copies the text of a base perk into the entry of each of
// its enhanced perks. The entry of the enhanced perk keeps the time of
// its own upload. The decode therefore dates the text of an enhanced
// perk at the later upload of the two entries.
package clarity
