// Package loot names the places where each craftable legendary weapon
// drops, such as a raid, a destination, or a vendor.
//
// The Destiny definitions give each weapon the source text of its
// collectible. Update 9.7.0, Monument of Triumph, moved the drops of many
// weapons but left the text as it stood. For example, the text sends
// the weapons of Season of Plunder to Ketchcrash and Expeditions, but Xûr
// sells them. The package therefore holds a table of the drops that
// community sources collected after the update.
//
// # Lookup
//
// A lookup takes the name of a weapon, such as the name of its pattern
// record. It folds letter case and diacritics as [fold.Name] does, and it
// reads a curly apostrophe as a straight one. A weapon can drop from
// several places. The first place is the place that both community
// sources name for the weapon, and each later place comes from one of
// them. A weapon that the table does not list, such as an exotic weapon,
// does not have any drop.
//
// # Data
//
// The file loot.json lists each legendary weapon that both community
// sources call craftable. The two sources are the Legendary Weapons
// Sources database of Blueberries.gg and the Monument of Triumph
// Legendary Loot Sources sheet of Sax. Each drop of the file names the
// sources that list it. A drop counts only where a source marks the
// version of the weapon that drops there as craftable. For example,
// Onslaught drops a version of Succession that the pattern does not
// craft, so the file does not name Onslaught for Succession.
//
// The sources describe the game at Update 9.7.0.2, which made Xûr sell
// Imperial Decree, Goldtusk, Death's Razor, and Throne-Cleaver. The patch
// notes of Update 9.7.0.3, Update 9.7.0.5, and Update 9.7.0.6 do not
// change any drop of a craftable weapon, and Bungie did not publish notes
// for Update 9.7.0.4. Each weapon of the file therefore names Update
// 9.7.0.6 as the update of its last check.
//
// The two sources agree on the place of each weapon except
// Syncopation-53 and Palmyra-B. One tab of the sheet of Sax names
// Savathûn's Throne World for them. Another tab of that sheet and the
// database of Blueberries.gg name Banshee-44, and the file follows them.
// The note of each of the two weapons in the file records the choice.
//
// The sheet of Sax also disagrees with itself about Brigand's Law,
// Planck's Stride, Sailspy Pitchglass, and Targeted Redaction. Its
// Vendors tab marks the version that Xûr sells as not craftable. Its
// Craftable Weapon Sources tab and the database of Blueberries.gg name
// Xûr for each of them, and Blueberries.gg marks each of them craftable.
// The Destiny definitions hold one weapon with each of these names, and
// that weapon names a recipe item, so Xûr sells the craftable version.
// The file keeps Xûr as the drop of each of the four weapons, and the
// note of each of them records the choice.
package loot
