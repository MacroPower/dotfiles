// Mcp-destiny is an MCP server (stdio transport) for Agg's Destiny Vault
// Manager. The project lives at https://github.com/agg23/destiny-inventory.
// The tools read the Destiny 2 inventory of the signed-in account and edit
// the data that the web client keeps per account. They also move items,
// lock them, change their perks, and equip saved loadouts in the live
// game.
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
// the best tier, to F. The get_item tool shows the tier that each sheet
// gives a weapon.
//
// The server reads the artifact index of the service on every call that
// needs a sheet. It identifies a loaded sheet by its file name, because
// the service names each artifact file after a hash of the content. The
// server therefore downloads a sheet only when the index lists a file name
// that differs from the name of the loaded sheet. It keeps a sheet only in
// memory, so each server process downloads each sheet.
//
// When a sheet does not load, the tool reports the reason on the line of
// that game mode and still shows the rest of its output. The server does
// not remember the load, so the next call that needs the sheet downloads
// the file again.
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
