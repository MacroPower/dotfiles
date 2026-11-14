// Package bungietest holds the fixtures and the fake server of the Bungie
// platform API that tests share. The fixtures describe one account with a
// Hunter and a Titan. They give the ids of the two characters and the
// hashes of the items and the vendors that the account sees. They also
// give the response bodies that Bungie returns for the account. A test
// builds the result that it expects from the same fixtures that the fake
// server serves.
//
// The fixtures hold hashes but do not hold the definitions of the items,
// the activities, and the vendors. Their comments name what each hash
// stands for and say which hashes lack a definition. The comments mean the
// definitions that the tests of the mcp-destiny server build for the same
// account.
//
// The fake server serves the platform API and the content files of the
// manifest on one URL. A test therefore passes that URL to the client
// under test as the platform URL and as the content URL. The server
// records each request that it receives. A test sets the fields of the
// server that pick the answers before the call under test. It reads the
// recorded requests after the call.
package bungietest
