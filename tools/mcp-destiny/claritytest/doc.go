// Package claritytest holds the fake of the Clarity site and the builder
// of its descriptions file that tests share. No test reads the real site,
// and no fixture copies a description of the real database.
//
// The fake site serves the versions file and the descriptions file on a
// local URL. A new fake names the fixture version and serves the fixture
// database, which describes one plug of the Bungie fixture profile of
// package bungietest. A test replaces the version or the descriptions
// file to model a release of Clarity. It sets a failure status to model a
// site that is down. The fake records each request, so a test counts the
// checks of the version and the downloads.
//
// The builder writes a descriptions file from a list of perks. Each perk
// carries the item hash of its plug, its names, the time of its upload,
// and its lines of text. A test of the decode rules writes the file by
// hand, because the builder gives each line one plain run of text.
package claritytest
