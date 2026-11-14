// Package dvmtest holds the fakes of Agg's Destiny Vault Manager and the
// builders of its stat pages that tests share. A fake service serves the
// API on a local URL, and an artifact fake serves the artifacts from memory.
//
// The fake service answers the config request with an artifact index of the
// files that a test lists. It answers each exchange of an authorization code
// and each session refresh with new tokens. It answers each GET with the body
// that a test stores for the path, and it accepts each POST. A test can
// also make the fake service reject a stale session, answer a claim of new
// items, serve another autotag body after a claim, or fail a tag write.
//
// The fake service records each request that it receives. It reads its
// knobs on every request, so a test sets a knob before the call that reads
// it. The test reads the recorded requests after the call. The fake service
// knows no account, so a test stores the bodies that name the items of the
// account under test.
//
// The artifact fake serves the artifact index and the artifact files from
// memory, for code that reads the artifacts through an interface in place
// of the client. It records each file that the code fetches, and it fails
// the fetch of a file that the test did not store. A test changes the
// index, the files, or the manifest version between two calls to model a
// new release of the service.
//
// The stat page builders encode one page of the profile changes that the
// web client records, as the stats endpoint of the service returns it. A
// test serves the pages from the fake service or from a server of its own.
package dvmtest
