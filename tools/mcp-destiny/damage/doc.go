// Package damage gives a weapon copy its time to kill a Guardian and its
// damage per second. It also shows how each active perk changes them.
//
// The Bungie API publishes neither number. It gives a weapon an Impact
// stat and a rate of fire, but not the damage of a shot. Players measured
// the damage of the frames in the game, and D2Foundry's Oracle Engine
// collects the measurements. The patch notes of Bungie from Update 8.2.6
// to Update 9.7.0.6 state the damage of some frames. This package holds
// the damage of the frames, the effect of some perks, and a port of the
// math of Oracle Engine.
//
// # Numbers
//
// Every number holds against a Guardian. A Guardian has 230 health, and
// the numbers give the Guardian no overshield and no damage resistance.
// Every round hits, and the target stands closer than the distance at
// which the damage falls off. The numbers leave out the Weapons stat of
// a character. The stat shortens the reload in the game, and a stat of
// more than 100 raises the damage against players. A combatant takes
// other damage, which depends on its rank and on the activity, and the
// package does not model it.
//
// A kill is the time from the first hit to the hit that brings the sum
// of the damage to the health of the Guardian. The body kill counts body
// shots alone. The optimal kill counts precision hits. A hit of the
// optimal kill counts as a body shot when the kill takes no more hits
// with it. A hand cannon can therefore kill with two precision hits and
// one body shot. When a kill takes more rounds than the magazine holds,
// it counts the reload.
//
// The damage per second is the damage of one magazine over the time of
// the magazine and of one reload. A copy that fires one magazine after
// another deals this damage per second. The time of the magazine counts
// the delay after each round, as Oracle Engine counts it. The package
// gives one damage per second for precision hits and one for body shots.
//
// # Perks
//
// A report holds the base numbers and one set of numbers for each state
// of each perk that the data models. The base numbers are the numbers of
// the copy while no timed perk is active. A state is a perk at one
// strength, such as Rampage at three stacks. Its numbers hold while the
// perk is that strong for every round. Each state stands alone, so the
// report does not combine two timed perks.
//
// A perk scales the damage of each hit, the time between two rounds, or
// the reload time. Target Lock scales the damage of a hit by the share of
// the magazine that hit before it. The Rapid-Fire Frame and Drop Mag
// always scale the reload time, so their effects are part of the base
// numbers.
//
// A perk can also raise the Reload Speed stat while it is active, as
// Frenzy raises it by 100. The numbers of its state then read the reload
// time at the raised stat. The caller gives the reload time at each
// bonus, and the curves package caps the raised stat as the game does.
//
// A stat bonus that a plug always gives does not need an effect in the
// data. The numbers read the Magazine stat and the reload time of the
// copy, and both hold the bonus.
//
// The report names each active plug whose effect the numbers leave out.
// A plug is left out when the data gives it no effect and does not list
// it as a plug without an effect on the numbers. The data lists a plug
// that way only by its name or by its type, with the reason and the
// source. A plug that the data does not know is therefore named, and no
// number ignores a perk without saying so. The data gives the enhanced
// plug of a perk its own effects, and it leaves out an enhanced plug
// that no source describes.
//
// # Lookup
//
// A copy takes the frame of its weapon type whose names hold the name of
// its frame plug. The frame plug is the plug of the type "Intrinsic" or
// "Enhanced Intrinsic". Two frames of one weapon type can share a name,
// as the two Adaptive Frames of sidearms do. The Rounds Per Minute stat
// of the copy then selects the frame.
//
// The stat also checks the data. A copy takes a frame only when its stat
// is within a twentieth of the rate of fire that the data gives the
// frame. The game rounds the stat, so 140 stands for a rate of 138.46.
// A copy whose stat is farther from the data has no numbers, and the
// reason names the stat. The check keeps a number from a frame whose
// rate of fire the game changed after the measurement.
//
// A copy has no numbers when the data lacks its weapon type or its
// frame, or when the copy lacks the plugs or one of the two stats. The
// report then holds a sentence for the user that says why. A number that
// reads the reload time is unknown when the reload time is unknown, and
// it carries the reason of the reload time.
//
// # Data
//
// The file damage.json holds the data, and the NOTICE file holds the
// license of the parts that derive from Oracle Engine. The file covers
// the legendary frames of auto rifles, hand cannons, pulse rifles, scout
// rifles, sidearms, and submachine guns that use Primary ammo, with these
// exceptions:
//
//   - Oracle Engine does not hold the Spread Shot frame of hand cannons,
//     the Aggressive Burst frame of submachine guns, or the Balanced Heat
//     Weapon and Dynamic Heat Weapon frames. The file omits these frames.
//   - The Support Frame of auto rifles raises the damage of the weapon
//     after it heals allies. The file has no effect for the frame, and a
//     report never names the frame among the plugs that the numbers leave
//     out. The file therefore omits the frame rather than give numbers
//     that leave out its bonus without saying so.
//
// The file holds no frame of an exotic weapon or of a weapon that uses
// Special ammo. Pulse rifles and sidearms with the Micro-Missile Frame
// use Special ammo, so the file omits that frame.
//
// The frame of the sidearm Drang is Together Forever, which Oracle Engine
// holds as an Adaptive Frame at 300 rounds per minute. The file gives
// Together Forever the numbers of that Adaptive Frame. Together Forever
// also reloads the hand cannon Sturm after a final blow, and it does not
// change the damage of the sidearm. The sidearm Unfall names its
// Micro-Missile Frame Together Forever as well. Its Rounds Per Minute
// stat differs from the rate of fire of the Adaptive Frame, so Unfall has
// no numbers.
//
// Oracle Engine describes the game at Update 8.2.6. The patch notes of
// Bungie from Update 8.2.6.1 to Update 9.7.0.6 change some of its values.
// The file takes each new value that the notes state, such as the damage
// of Adaptive auto rifles in Update 9.7.0. It omits a value that the
// notes change without stating it. Update 9.7.0 lowers the base damage of
// High-Impact auto rifles and does not state their precision damage, so
// these frames have no optimal kill.
//
// The file also takes the damage that the notes of Update 8.2.6 state
// where Oracle Engine differs from them. Oracle Engine stores a precision
// damage as a scale of the body damage in steps of 1/51, so it can miss
// a stated value. The notes give Precision auto rifles a precision damage
// of 34, and Oracle Engine gives them 33.922.
//
// A perk value comes from Oracle Engine, from an entry of the Clarity
// database, or from the patch notes that state it. The file takes a
// value only from a source that is not older than the last update whose
// notes change the value. Each value names its source and the last
// update whose patch notes leave the value as it stands. A value from
// Clarity also names its entry and the upload date of the entry. The
// file copies no text of the Clarity database.
//
// The description of a plug in the manifest of Bungie is the source of
// most plugs without an effect on the numbers. The description of an
// enhanced trait says what the enhanced trait improves. The file gives
// an enhanced trait the damage of the base trait when the description
// names only the duration.
package damage
