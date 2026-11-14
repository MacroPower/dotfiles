// Package activities lists the activities that a character can play, with
// the rewards that each activity still offers this week. It follows the
// Activities page of the web client of Agg's Destiny Vault Manager.
//
// # Rows
//
// Bungie reports the activities of each character in the character
// activities component of a profile. Each entry gives the hash of an
// activity definition, the recommended power, and the rewards that the
// activity still offers this week. Bungie drops a reward from the entry
// after the character claims it.
//
// A row joins one entry with the Destiny definitions. The definitions give
// the name of the activity and the name of its activity type. They also
// name the destination of the activity, or its place when the destination
// has no name. The row leaves out a location that equals the type, as the
// web client does, so no row reads "The Crucible in The Crucible".
//
// The uiStyle of each reward gives its role in the row. The game shows a
// reward with the extra engram style as a bonus drop, and the quantities of
// those rewards add up to the bonus drops. It shows a reward with the daily
// grind style as the focused reward, and the first such reward that the
// definitions name is the focus. Each named reward without a uiStyle and
// with a quantity of 1 or more is bonus loot. The rewards of the bonus loot
// that share a name merge into one entry.
//
// # Dropped Activities
//
// The list leaves out each activity that the game hides and each activity
// that the definitions do not name. It also leaves out the activities of
// the Story and Social modes, because they carry no loot. Bungie gives some
// activities of the Story types no mode, such as "Excision: Grandmaster".
// The list therefore also leaves out each activity whose type Bungie files
// as Story. The web client leaves out the same activities.
//
// # Ranking
//
// The rows follow the reward keys of the ranking on the Activities page.
// More bonus drops come first. Among activities with equal bonus drops,
// those with a focused reward come first, then those with bonus loot, then
// the featured ones. The name and the hash break the remaining ties. The
// web client also puts the Quickplay activities first and ranks by power,
// but the list leaves out both keys.
//
// # Usage
//
// A caller fetches the character activities from Bungie and loads the
// definitions. It reads the rows of one character and then filters them by
// a query and by whether each activity still offers a reward. The filter
// keeps the order of the rows, so the caller can page the result.
package activities
