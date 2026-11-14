// Package dvmtest holds the fake service of Agg's Destiny Vault Manager and
// the builders of the stat pages that tests share. The fake answers the
// config request with an artifact index of the files that a test lists. It
// answers each session refresh with new tokens. It answers each GET with the
// body that a test stores for the path, and it accepts each POST. A test can
// also make the fake reject a stale session, answer a claim of new items,
// serve another autotag body after a claim, or fail a tag write.
//
// The fake records each request that it receives. It reads its knobs on
// every request, so a test sets a knob before the call that reads it. The
// test reads the recorded requests after the call. The fake knows no
// account, so a test stores the bodies that name the items of the account
// under test.
//
// The stat page builders encode one page of the profile changes that the
// web client records, as the stats endpoint of the service returns it. A
// test serves the pages from the fake or from a server of its own.
package dvmtest
