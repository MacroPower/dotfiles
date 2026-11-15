// Package curves gives a weapon copy its reload time, its handling times,
// and its damage falloff distances in seconds and meters.
//
// Bungie publishes each weapon stat as a number from 0 to 100. The curve
// that turns a stat into seconds or meters differs for each weapon type
// and frame. The Bungie API does not publish the curves. Players measured
// them in the game, and D2Foundry's Oracle Engine collects the curves
// that they fitted. This package holds those curves and evaluates them at
// the stats of a copy.
//
// # Measures
//
// A caller describes a copy by its type, its item hash, the plugs in its
// sockets, and its stats. It gets one measure for each value. The reload
// time reads the Reload Speed stat. The ready time, the stow time, and
// the time to aim down sights read the Handling stat. The distance at
// which the damage falloff starts reads the Range stat. The distance
// while the copy aims down sights is the distance from the hip times a
// scale of the frame.
//
// The measures are the base values of the weapon. The Weapons stat of a
// character, an armor mod, and a perk with a timed bonus all change the
// values in the game. A perk that scales a time or a distance without a
// stat bonus, such as Snapshot Sights, changes them as well. The package
// reads none of these effects.
//
// A measure that the data cannot give holds a sentence for the user that
// says why. The sentence names the cause, such as a weapon type without
// curves, a copy without plugs or without the stat, or a game update that
// changed the value after the last measurement.
//
// # Lookup
//
// The data keys the curves of each weapon type by hash. The hash is the
// plug hash of a frame, the plug hash of the intrinsic perk of an exotic,
// or the item hash of a weapon. A lookup tries the item hash of the copy
// first and then the hash of each plug. A copy without plugs therefore
// finds curves only by its item hash. When the data does not list that
// hash, the frame of the copy is unknown, and the reason names the
// missing plugs.
//
// The game gives the frame of a crafted weapon a plug hash of its own, so
// one frame has many plug hashes. The data lists the plug hashes of each
// frame name. A copy whose plug is on such a list takes the curves that
// its weapon type gives the frame. It takes none when the hashes of the
// frame in that type disagree, as the Wave Frame and the Compressed Wave
// Frame of the grenade launchers do.
//
// A plug that keys the curves of another weapon type selects no frame.
// One list can hold two frame names, such as the Aggressive Frame and the
// Aggressive Burst. The data gives a weapon type the curves of only the
// names that the source measured for that type.
//
// # Data
//
// The file curves.json derives from Oracle Engine, and the NOTICE file
// holds the license. Oracle Engine describes the game at Update 8.2.6.
// Each curve of the file names its source and the last game update whose
// patch notes leave the curve as it stands. The patch notes of Bungie
// from Update 8.2.6.1 to Update 9.7.0.6 change some values. The file
// takes each new value that the notes state, such as the scale of
// High-Impact auto rifles in Update 9.7.0. It omits each value that the
// notes change without stating it, such as the reload time of glaives.
// It also omits a value when the notes do not say whether a change
// reaches the weapon, as for the scale of the exotic submachine guns.
// The reason of each omission names the update.
//
// In Oracle Engine, the damage of sniper rifles, bows, linear fusion
// rifles, grenade launchers, and rocket launchers does not fall off with
// distance. The damage of some exotics, such as Telesto and Xenophage,
// does not fall off either. The file omits the falloff distances of these
// weapons, and the reason of each omission says that the damage does not
// fall off.
//
// The file also omits a value that Oracle Engine does not measure. Oracle
// Engine holds no falloff distance for Cerberus+1 and Lord of Wolves, and
// it marks its scale for machine guns as a guess. It gives every shotgun
// one reload time, and it does not say whether the time covers one shell
// or a full reload. The file therefore omits the reload time of shotguns.
package curves
