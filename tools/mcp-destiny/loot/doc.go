// Package loot names the places where each craftable legendary weapon
// drops, such as a raid, a destination, or a vendor. It also tells how a
// player gets red borders at each place.
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
// does not have any drop. Each drop carries the red border note of its
// place, as the Red Borders section describes.
//
// # Activities
//
// The table also names the activity of each raid, dungeon, and exotic
// mission that it lists, and of Dares of Eternity. A lookup by the name of
// an activity, such as a featured activity of the Portal, returns the
// place that holds the activity. The name of an activity matches a place
// when it equals the activity name of the place or continues it with ": "
// or " (". The activity "Vault of Glass: Master" therefore belongs to the
// Vault of Glass raid. A name that holds the activity name of a place
// after its start does not match. For example, the Pantheon activity
// "Featured Encore: Warpriest: The Pantheon" does not belong to the
// Encore mission. The Pantheon does not drop the weapons of that mission.
// A vendor and a destination do not have an activity.
//
// The activity names come from the activity definitions of the Destiny
// manifest version 245114.26.10.13.2000-1-bnet.66137. Most of them are the
// name of the place without its kind, such as Vault of Glass for the
// Vault of Glass raid. The definitions name the Avalon mission
// "//node.ovrd.AVALON//".
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
//
// # Red Borders
//
// Each place of the table carries a red border note. The note tells how a
// player gets copies with Deepsight Resonance, the red border of the
// game, at the place. Every weapon that drops at one place shares the
// note of the place. For example, the Classic Collector modifier of
// Pinnacle Ops makes each run of an exotic mission drop 2 red borders for
// a player who owns the season of the mission.
//
// The notes come from these sources:
//
//   - The patch notes of Update 9.7.0 give the rewards of each raid in a
//     week that the weekly rotator features it and in other weeks. They
//     also add the Classic Collector modifier to the exotic missions, for
//     the players who own the season of a mission through its Season Pass
//     or a Deluxe Edition. The text of the modifier in the game limits
//     the legacy rewards to the players who own access to the original
//     content. The note of each mission follows these two sources rather
//     than Sax and Item Level Gaming, which name the expansion of the
//     season. The Final Shape includes the Episodes of its year, so the
//     note of Encore names the expansion.
//   - A reply on a post on r/destiny2 says that Deep Stone Crypt, Garden
//     of Salvation, and Last Wish have no Master mode. In a week that the
//     rotator does not feature one of these raids, only its Standard mode
//     drops red borders, and only at a chance.
//   - Sax found in a post on r/DestinyTheGame and in a video that each
//     run with the Classic Collector modifier drops 2 red borders. He
//     also found that a Deepsight Harmonizer gives a red border to a copy
//     of any craftable weapon.
//   - In replies to the post of Sax, he wrote that players reported fewer
//     red borders from runs on Normal or Advanced difficulty. He advised
//     the highest difficulty that a player can clear quickly. He also
//     wrote that Duality drops red borders at random and that a week that
//     features the dungeon does not guarantee them. Another reply of his
//     says that Xûr can drop red borders of Death's Razor, one of the
//     weapons that Update 9.7.0.2 added to his engrams.
//   - The patch notes of Update 9.7.0.2 say that a Deepsight Harmonizer
//     unlocks the patterns of the four weapons that Xûr began to sell.
//   - Guides of Blueberries.gg name the activities and the reputation
//     ranks that give the weapons of the Throne World and of the Pale
//     Heart. A post on r/destiny2 names the activities of Neomuna and of
//     the Throne World. Replies to that post say that the Lightfall
//     campaign gives each character a red border of Round Robin on its
//     first completion. The video of Sax names the campaign of each
//     expansion as a source of the weapons of its destination.
//   - The sheet of Sax gives the reputation ranks of Nimbus as a source
//     of the weapons of Neomuna.
//
// The sources do not say how often a copy from Dares of Eternity, the
// Throne World, or the Pale Heart carries a red border. They also do not
// say whether a copy from the Legacy Focused Decoding of Banshee-44 can
// carry one. The note of each of these places says so and points to a
// Deepsight Harmonizer.
//
// Each source names the update of the game that it describes. A post that
// the file cannot date names Update 9.7.0, the first update that it can
// describe. Each place names Update 9.7.0.6 as the update of its last
// check against the patch notes of the hotfixes:
//
//   - Update 9.7.0.2 added four weapons to the sale and the engrams of
//     Xûr. The note of Xûr cites its patch notes.
//   - Update 9.7.0.2 also fixed Duality, which dropped its craftable
//     weapons as tiered weapons until then. A reply on the post of Sax
//     shows that he wrote about Duality before the fix. The Duality note
//     therefore leaves out the drop rate that he gives, and it keeps the
//     claims that the fix does not touch.
//   - The patch notes of Update 9.7.0.1, Update 9.7.0.3, Update 9.7.0.5,
//     and Update 9.7.0.6 do not change how any place drops red borders.
//     Bungie did not publish notes for Update 9.7.0.4.
package loot
