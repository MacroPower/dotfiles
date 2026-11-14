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
// history, and the manifest of a Destiny account. It also reads the
// definition of one item from the live manifest. It also performs the
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
// tokens and retries a request that fails with [ErrUnauthorized]. A
// [*StatusError] reports a response without a platform response, such as
// the HTML page of a gateway error. It unwraps to [ErrPlatform] too.
//
// # Loadout Tables
//
// The loadout tables of the Destiny manifest give the name, the icon, and
// the color that the game shows for each saved loadout. A
// [*LoadoutTableStore] reads them through a [ManifestSource]. A caller
// adapts a [*Client] to that interface and adds the credentials to each
// manifest read. From the loaded tables, [ChooseLoadoutIdentifiers] picks
// the identifiers that a save of a loadout slot sends.
//
// The store reads the manifest on every call to [*LoadoutTableStore.Load],
// so each table follows the manifest that Bungie publishes. Each content
// path carries an identifier that Bungie changes with every manifest
// release. The store therefore downloads a table only when the manifest
// names a content path that differs from the path of the kept table. It
// keeps each table only in memory. The store has no lock, so its caller
// serializes the calls to Load.
package bungie
