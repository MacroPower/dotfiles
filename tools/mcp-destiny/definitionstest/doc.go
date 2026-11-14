// Package definitionstest holds the Destiny definitions that tests share
// for the fixture account of package bungietest. Those fixtures hold
// hashes, and these definitions give the hashes names, types, and buckets.
// A test passes the definitions to the code under test beside a fixture
// response of Bungie. It builds the output that it expects from the same
// names. A test gets a new value from each call, so it can change a
// definition without changing the definitions of another test.
//
// # Items
//
// The definitions name the weapons of the fixture profile, which are the
// legendary hand cannons Fatebringer and Midnight Coup and the exotic
// rocket launcher Gjallarhorn. They also name the Hunter helmet Celestial
// Nighthawk, the consumable Enhancement Core, Glimmer, and Bright Dust.
// The stat names cover the stats of the vault hand cannon and the helmet,
// except the stat with hash 7.
//
// The currency with hash 77 has no definition, and the service does not
// withhold it, so the manifest version of the definitions lacks it. The
// service withholds the definition of the stack [ItemWithheld], and no
// table names that stack.
//
// The plugs of the vault hand cannon are Explosive Payload, Firefly with
// its description, the Default Shader, and Rampage. The other plugs are
// the traits and the barrel that the tests of the perks and the perk
// preferences read. Kill Clip and Chill Clip each have a trait and an
// enhanced trait, two hashes with one name.
//
// # Buckets
//
// The definitions describe the vault, the Postmaster, the kinetic weapon,
// power weapon, and helmet slots, the engrams, and the consumables of the
// fixture profile. They also describe the materials and the quests, two
// buckets that no production code reads.
//
// # Activities
//
// The definitions name the activities of the fixture history and the
// activities that the characters can play. They also name the types, the
// places, and the destinations that those activities point at, except
// destination 701. The reference hashes of the second and the third
// Hunter runs have no definition, and neither has the activity 4005. The
// definition of 3334 has no name. The reward names leave out each reward
// that the fixtures list without a reward record.
//
// # Vendors
//
// The definitions name Banshee-44 with its failure strings and its display
// categories, Tess Everis, and the two vendors that share the name Xûr.
// The vendor with hash 1111 has a definition without a name, and the
// sub-vendor of Eververse has no definition. The vendor item names cover
// the stock that the item definitions leave out, such as Legendary Shards.
// Neither table names the item with hash 999 that Banshee-44 sells. The
// service withholds the definitions of the items that the vendor item
// names and the reward names cover, and of item 999.
package definitionstest
