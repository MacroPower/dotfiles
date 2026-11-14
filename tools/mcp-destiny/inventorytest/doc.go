// Package inventorytest holds the inventory snapshot of the fixture
// account that the tests of other packages share. The snapshot joins the
// fixture profile of package bungietest with the definitions of package
// definitionstest, as the server joins a live profile with the live
// definitions. Each call builds a new snapshot, so a test can change its
// snapshot without changing the snapshot of another test.
//
// The package also holds some of the weapon traits of the fixture
// definitions, as the snapshot resolves them into plugs. A test builds the
// perks that it expects from the same plugs. A test of the items that a
// call selects compares their instance ids, in order.
//
// The internal tests of package inventory cannot import this package,
// because it imports package inventory. They keep their own copies of the
// snapshot, the plugs, and the list of instance ids.
package inventorytest
