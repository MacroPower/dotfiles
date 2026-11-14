// Package autotag evaluates the autotag rules of Agg's Destiny Vault
// Manager the way the web client runs them. An autotag rule holds a search
// query, and it can carry a tag, the vault flag, or both. The web client
// claims the items that the account acquired since the last claim. It
// gives each new item to the first rule that takes the item. A rule with a
// tag sets the tag on its items, and a rule with the vault flag moves its
// items to the vault.
//
// A caller loads the stored rules and an inventory snapshot, and the
// package changes neither. The caller drops the removed rules and
// classifies the live rules first. It then walks the new items through the
// classified rules, or it counts the owned items that each rule matches.
// The caller claims the items, writes the tags, and moves the items
// itself.
//
// # Rule Classes
//
// Before it reads any item, the package puts each live rule in one of
// these classes:
//
//   - Evaluated: the package evaluates the query with the filters of
//     package search.
//   - Skipped: the web client skips the rule, so the rule takes no item.
//     The web client skips most queries that its parser rejects, and each
//     query that holds a value that it rejects.
//   - Blocked: the web client runs the rule, or might run it, but the
//     package cannot evaluate it. The package cannot evaluate a query that
//     the web client reads differently from its text. It also cannot
//     evaluate a query with a term outside the filters of package search,
//     or a query that reads a game mode without ratings.
//
// The first problem of a query decides the class, and the class ignores
// whether the rule is enabled. A caller passes a function that loads the
// rating source of each game mode. The package calls the function at most
// one time, and only when a query without another problem reads a game
// mode.
//
// # Claim Mark
//
// The service stores the mark of the last claim. A mark is an instance id
// that the package pads on the left with zeros to 20 digits. The web
// client and the service compare marks as text, so the padding lets that
// comparison order ids of different lengths by their numbers. The next
// claim covers each owned item whose mark is higher than the stored mark.
// It moves the stored mark to the highest mark of the owned items, the
// vault included. The web client offers the rules only the new items
// outside the vault.
//
// # First Match
//
// The web client gives each new item to the first enabled rule, in rule
// order, whose query matches the item. The walk passes each disabled rule
// and each skipped rule. A rule without a tag and without the vault flag
// still takes its items, so no later rule sees them.
//
// The walk stops at the first enabled rule that blocks. That rule might
// take any item that reaches it, so the package cannot tell which items
// reach the later rules. An item that an enabled rule leaves undecided
// stops at that rule for the same reason, because the rule might take the
// item. The walk reports the items that reach the blocking rule and the
// undecided items apart from the items that each rule takes.
//
// # Owned Counts
//
// The rule editor of the web client counts the owned items that each rule
// matches, the vault included. The package counts them the same way. It
// walks the rules in order and counts each evaluated rule over the items
// that no earlier enabled rule took. An enabled rule then takes the items
// that it matches, and a disabled rule takes nothing. A skipped rule
// counts nothing and takes nothing.
//
// After an enabled rule that blocks or that leaves an item undecided, the
// package cannot tell which items reach the later rules. It marks each
// later evaluated or blocked rule as unknown, and it counts nothing for
// that rule.
//
// # Vault Rules
//
// A rule with the vault flag moves the items that it takes to the vault. The
// web client leaves such an item in place when a character has it equipped, or
// when Bungie gives it the transfer status NotTransferrable alone. It also
// leaves an item in place when the definition puts the item in the bucket of
// the distorted engrams or in a Postmaster bucket of Destiny Item Manager
// (DIM). The package also leaves in place an item that
// [inventory.Item.Transferable] does not mark, which [inventory.Snapshot.Move]
// refuses to transfer. The reason of such an item says that the web client
// tries the transfer. Each item that stays in place carries its reason, and the
// caller moves the rest.
package autotag
