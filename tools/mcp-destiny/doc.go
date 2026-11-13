// Mcp-destiny is an MCP server (stdio transport) for Agg's Destiny Vault
// Manager. The project lives at https://github.com/agg23/destiny-inventory.
// The tools read the Destiny 2 inventory of the signed-in account and edit
// the data that the web client keeps per account. They also move items,
// lock them, change their perks, and equip saved loadouts in the live
// game. The Bungie API has no dismantle request, so stage_junk moves the
// items with the junk tag into one slot of a character for the user to
// dismantle in the game.
//
// # Data Sources
//
// The server talks to two APIs. Agg's Destiny Vault Manager stores tags,
// notes, perk preferences, autotag rules, and the profile changes that the
// web client records. It also serves the Destiny definition artifacts and
// brokers the Bungie OAuth exchange. The Bungie platform API serves the
// inventory, the saved loadouts, the activity history, and the live stock
// of the vendors. It also serves the activities that each character can
// play, with the rewards that each activity still offers this week. The
// same API performs transfers, equips, lock changes, perk changes, and
// loadout equips. The server joins a Bungie profile with the definitions
// to give every item a name, a type, perks, and stats. The definitions
// also name each activity of the history, each vendor, and the items that
// the vendors sell. For each activity that a character can play, they give
// the name, the type, the location, and the names of the rewards.
//
// Tags follow the web client. An item carries at most one of the tags that
// the web client defines, and the favorite mark is a separate flag. The web
// client evaluates autotag rules, so the server stores a rule query without
// checking its syntax.
//
// The web client records profile changes only while it is open in a
// browser. It diffs each Bungie profile that it fetches against the one
// before, and it posts the changes of each 5-minute bucket after the bucket
// closes. The get_stats tool reads those buckets back from the service. It
// therefore reports only the changes that a browser recorded and posted.
//
// # Roll Ratings
//
// The service publishes a roll sheet for each game mode. The Aegis sheet
// rates weapons for PvE, and the service publishes it as the rolls
// artifact. The PvP sheet rates weapons for PvP, and the service publishes
// it as the pvprolls artifact. Each sheet gives a weapon a tier from S,
// the best tier, to F. It also picks the perks that it wants in each perk
// column of the weapon. A sheet lists each weapon in a category, such as
// the hand cannons. It can rank the weapon in that category, and it can
// give the weapon a note.
//
// The get_item tool rates the roll of the weapon copy that it shows
// against each sheet. The tier of a copy starts at the tier of the weapon
// on the sheet. It moves down one step for each perk column that the copy
// misses, and it stops at F. Two copies of one weapon can therefore
// differ by several tiers.
//
// The rating line of each game mode in the get_item output also places
// the weapon on the sheet. The line names the category in the text of the
// sheet file, such as "HCs" for the hand cannons of the Aegis sheet. When
// the sheet ranks the weapon, the line gives the rank and the number of
// weapons in the category, such as "rank 3 of 62 in HCs". The note of
// the weapon follows the lines of the perk columns, after the label
// "Sheet note". The note describes the weapon, so every copy of the
// weapon shows it, also a copy that reads "roll unknown". Only the
// get_item tool shows the category, the rank, and the note.
//
// The list_items tool shows the same rating of each weapon copy for each
// game mode when the caller sets include_ratings. The rating cell of a
// copy that has a tier gives that tier. It adds the tier of the weapon on
// the sheet when the two tiers differ. The get_item tool explains a
// rating column by column.
//
// The list_items tool also filters by the same rating. The inputs
// min_pve_rating and min_pvp_rating each take the lowest tier to include
// for one game mode. A copy must reach each minimum that the caller
// sets. Only a weapon copy whose roll has a tier reaches a minimum. A
// weapon that the sheet does not list, a weapon on a row without a
// tier, and a copy that reads "roll unknown" never match.
//
// A copy hits a perk column with each pick of the column that it has.
// The rating also counts the perk preferences, which the user sets for
// each game mode in the web client or with the set_perk_preferences
// tool. A disliked perk never counts in its game mode, also when the
// sheet picks it. A liked perk counts as a pick, and the line of its
// column marks it with "(liked)".
//
// A liked perk counts in the socket of its perk column. That socket is
// the first socket in which the copy rolled a pick of the column. When
// the copy rolled no pick of the column, the plug sets of the weapon
// definition name the socket. A plug set lists the plugs that one socket
// of the weapon can roll. The socket of the column is then the first
// socket whose plug set lists a pick of the column.
//
// A liked perk therefore also earns a column in which the copy rolled no
// pick, when it sits in the socket whose plug set lists a pick of the
// column. The web client finds the socket of a column in the same two
// steps. The column is a miss when the perk list of the copy leaves out
// the socket that the plug sets name.
//
// The server reads the plug sets from the detail artifact and the
// plugsets artifact of the service. On a deployment whose artifact index
// lacks one of the two artifacts, a liked perk earns a column only beside
// a pick that the copy rolled.
//
// Each column line of the get_item output also names the perks that the
// sheet picks for the column, after the words "The sheet picks". The
// line of a missed column therefore says which perks to look for in
// another copy. The sheet lists its picks across every version of the
// weapon, so a pick can be a perk that this version cannot roll. The
// list leaves out each perk that the user dislikes for the game mode.
// A column line without a pick to name ends after its verdict. Only the
// get_item tool names the picks.
//
// The web client rates the socket list that Destiny Item Manager (DIM)
// builds. The server rates the perks that get_item lists, so the user can
// check a rating against the perks in the same result. The two ratings of
// one copy can differ, because the server follows these rules:
//
//   - The rating reads the sockets that the perk list of the copy keeps.
//     The list leaves out each socket that the profile hides and each
//     socket whose active plug is not a perk, such as an empty socket or a
//     shader. It also leaves out each plug that the copy cannot insert.
//     DIM keeps those sockets and those plugs.
//   - The rating reads every plug that a kept socket can hold, so a pick
//     that sits beside the active perk in a socket counts. DIM reads
//     those plugs only for the sockets that it classes as perk sockets,
//     and it drops each socket that it cannot class.
//   - The rating counts every plug that the copy can insert. DIM drops a
//     masterwork plug and a ghost projection that are not active.
//   - A socket for which the profile lists no plugs counts only its
//     active plug. DIM takes the plugs of such a socket from the
//     definitions.
//   - For a column in which the copy rolled no pick, the rating reads the
//     plug set of every socket of the weapon definition. It stops at the
//     first socket whose plug set lists a pick. DIM drops each socket
//     that it cannot class, so the web client can find a later socket.
//
// The active plug of a kept socket always counts. When the profile lists
// no perks for a copy, the server did not see the roll. Such a copy reads
// "roll unknown" on a sheet row with perk columns. A row without perk
// columns, such as the row of an exotic, shows its tier for every copy.
// In the list_items table, a weapon on a row without a tier reads
// "no tier" for every copy, also for a copy without perks.
//
// The server reads the artifact index of the service on every call that
// needs a sheet. It identifies a loaded sheet by its file name, because
// the service names each artifact file after a hash of the content. The
// server therefore downloads a sheet only when the index lists a file name
// that differs from the name of the loaded sheet. It keeps a sheet only in
// memory, so each server process downloads each sheet.
//
// When a sheet does not load, the get_item tool reports the reason on
// the line of that game mode and still shows the rest of its output. The
// server does not remember the load, so the next call that needs the
// sheet downloads the file again.
//
// The server reads the perk preferences on every call that rates a roll,
// and it keeps no copy of them. A preference that the user changes in the
// web client therefore counts in the next call. When the server cannot
// read the preferences, each game mode whose sheet loaded reports that
// reason in place of its rating, for every weapon. A game mode whose
// sheet did not load keeps the reason of the sheet. The get_item tool
// still shows the rest of its output, because the item, its perks, and
// its stats are correct without a rating.
//
// The server takes the names of the picks and the plug sets from the
// Destiny definitions. It keeps the loaded definitions in memory for one
// hour and then asks the service for the manifest version again. It
// loads the definitions again when the version changed. A call that
// fetches the profile reads the definitions before the profile, and it
// returns an error result when that read does not succeed.
//
// A call that reuses the profile of an earlier call reads the
// definitions only for the ratings. When that read does not succeed,
// each game mode that has no other reason reports the reason of the
// definitions in place of its rating, for every weapon. A game mode
// whose sheet did not load keeps the reason of the sheet. When the
// server can read neither the preferences nor the definitions, the game
// mode reports the reason of the preferences. The get_item tool still
// shows the rest of its output.
//
// A game mode without ratings does not fail a list_items call that
// only shows the ratings. The rating cells of the mode read
// "unavailable" for every weapon, and one line before the table gives
// the reason. The line holds the text that the get_item tool prints for
// the mode.
//
// A game mode without ratings fails a list_items call that sets the
// minimum of that mode. The server cannot tell which copies pass, so
// the tool returns an error result that names the input and gives the
// reason. A result without items therefore always means that no copy
// reaches the minimum. The minimum of one game mode does not read the
// ratings of the other mode, so it works when only its own mode has
// ratings.
//
// The compare_duplicates tool shows the roll rating of each copy for
// each game mode in every table, in the rating cells of the list_items
// table. A game mode without ratings does not fail the call. The
// rating cells of the mode read "unavailable", and one line before the
// first table gives the reason.
//
// # Sign-in
//
// Bungie redirects a sign-in to the one URL registered for the deployment,
// which is the web client. The login tool and the login subcommand
// therefore return an authorization URL with an OAuth state that the web
// client rejects. The web client shows an error and leaves the
// authorization code unused. The user copies the redirected URL to the
// complete_login tool or to the login prompt, and the server exchanges the
// code through the service.
//
// The server keeps the tokens in tokens.json in the state directory and
// refreshes them until the Bungie refresh token expires.
//
// # Subcommands
//
//   - serve: run the MCP server. It is the default subcommand.
//   - login: sign in from a terminal.
//   - logout: delete the saved tokens.
//
// # Flags
//
//   - --service-url: base URL of Agg's Destiny Vault Manager
//   - --state-dir: directory of the token file (default:
//     $XDG_STATE_HOME/mcp-destiny)
//   - --cache-dir: directory of the definition cache (default:
//     $XDG_CACHE_HOME/mcp-destiny)
//   - --log-file: path to JSON log file (append), which records tool
//     errors, failed moves, failed lock changes, and failed loadout
//     equips
package main
