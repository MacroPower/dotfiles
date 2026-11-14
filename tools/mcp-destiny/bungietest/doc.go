// Package bungietest holds the fixtures, the fake server of the Bungie
// platform API, and the recording fake of the Bungie item actions and
// loadout actions that tests share. The fixtures describe one account with
// a Hunter and a Titan. They give the ids of the two characters and the
// hashes of the items and the vendors that the account sees. They also
// give the response bodies that Bungie returns for the account. A test
// builds the result that it expects from the same fixtures that the fake
// server serves.
//
// The fixtures hold hashes but do not hold the definitions of the items,
// the activities, and the vendors. Their comments name what each hash
// stands for and say which hashes lack a definition. The comments mean the
// definitions of package definitionstest, which describe the same account.
//
// The fake server serves the platform API and the content files of the
// manifest on one URL. A test therefore passes that URL to the client
// under test as the platform URL and as the content URL. The server
// records each request that it receives. A test sets the fields of the
// server that pick the answers before the call under test. It reads the
// recorded requests after the call.
//
// The step mover is a recording fake of the item actions and the loadout
// actions of the Bungie client, for code that takes those actions through
// an interface. It records each action as a short string and sends no
// request. A test compares the recorded steps with the actions that it
// expects, and it can make the mover refuse one action.
package bungietest
