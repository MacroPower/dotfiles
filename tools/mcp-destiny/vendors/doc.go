// Package vendors reads the live stock of the vendors of one Destiny 2
// character. It joins a Bungie GetVendors response with the Destiny
// definitions, as the web client of Agg's Destiny Vault Manager does.
//
// # Vendors
//
// A GetVendors response describes the vendors of a character in three
// components. The vendors component gives the time when the stock of each
// vendor rotates next. It also marks each vendor that the game does not let
// the character reach. The sales component lists the sales of each vendor
// by vendor item index, with the costs and the status of each sale. The
// categories component lists the vendor item indexes that each display
// category of a vendor shows.
//
// A vendor joins its entries of the three components with its vendor
// definition. The definition names the vendor, its display categories, and
// the reasons why a sale fails. The read takes every vendor that the vendors
// component or the sales component names. The sub-vendors that hold the
// stock of Eververse appear only in the sales component. A vendor that the
// definitions do not name reads "Vendor <hash>".
//
// # Sales
//
// A sale joins one entry of the sales component with the definitions. The
// vendor item names of the definitions name the item first, and the item
// definitions name it second, in the order of the web client. Many vendors sell
// dummy items, and the item definitions leave them out. The read drops each
// sale that the vendor displays without selling the item. It also drops each
// sale whose item the definitions do not name, as the web client does. A sale
// that the character cannot buy carries the failure strings of its vendor
// definition that the failure indexes of the sale pick.
//
// # Sale Groups
//
// The groups of a vendor follow the display categories, ordered by display
// category index. Each group holds the sales that its category lists, in the
// order of the category, under the name that the vendor definition gives the
// category. A category that the definition does not name reads
// "Category <index>". The sales that no category lists follow in an
// Uncategorized group, ordered by vendor item index. A category without a
// kept sale gives no group.
//
// # Selection
//
// A caller reads the vendors of a character, ordered by name and then by
// hash. It lists them, or it selects one vendor by an argument that the
// user passes and lists the sales of that vendor. A decimal argument
// selects the vendor with that hash. Any other argument selects by name.
// An exact name ranks first, then a name that matches in any letter case,
// then a name that contains the argument in any letter case.
//
// The selection takes the first rank with a match. When that rank holds
// more than one vendor, the selection returns an error that names some of
// them with their hashes. The user can then pass a hash. An argument that
// matches no vendor also returns an error, and each such error wraps
// [ErrUnknownVendor].
package vendors
