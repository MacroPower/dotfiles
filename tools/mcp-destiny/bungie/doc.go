// Package bungie covers the Bungie platform API and the Destiny manifest.
// It holds the values that a profile item, a definition, or a request to
// the platform API carries. Such values include the hash of a bucket, the
// bits of an item state, and the value of a class. A caller compares a
// field of a profile item or of a definition with these values to classify
// the item. The names follow the enums of the Bungie platform API and the
// known values of Destiny Item Manager (DIM).
//
// An enum value is an untyped constant, so it compares with an int field
// and with a uint32 field alike. A hash has the type of the value that a
// caller compares it with.
package bungie
