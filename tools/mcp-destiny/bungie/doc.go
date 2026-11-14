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
//
// # Client
//
// The client reads the profile, the vendors, the activities, the activity
// history, and the manifest of a Destiny account. It also performs the
// item actions and the loadout actions in the live game. The caller passes
// credentials with each platform request. Every platform request carries
// the API key of Agg's Destiny Vault Manager and the access token of the
// signed-in account. The client also downloads the content files that the
// manifest names, such as a definition table. Those requests go to the
// content base URL and carry no credentials.
//
// A [*PlatformError] carries the error code of a platform response. It
// unwraps to [ErrPlatform]. When the code reports an unusable access
// token, it unwraps to [ErrUnauthorized] as well. The caller refreshes the
// tokens and retries a request that fails with [ErrUnauthorized].
package bungie
