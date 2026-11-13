// Mcp-destiny is an MCP server (stdio transport) for Agg's Destiny Vault
// Manager. The project lives at https://github.com/agg23/destiny-inventory.
// The tools read the Destiny 2 inventory of the signed-in account and edit
// the data that the web client keeps per account. They also move items,
// lock them, change their perks, and equip saved loadouts in the live
// game. The Bungie API has no dismantle request, so stage_junk moves the
// items with the junk tag into one slot of a character for the user to
// dismantle in the game. The Bungie API also has no request that builds a
// loadout from a list of items. The save_loadout tool therefore saves the
// gear that a character has equipped into a loadout slot, and
// clear_loadout empties a slot.
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
// the equips, saves, and clears of loadouts. The server joins a Bungie
// profile with the definitions to give every item a name, a type, perks,
// and stats. The definitions also name each activity of the history, each
// vendor, and the items that the vendors sell. For each activity that a
// character can play, they give the name, the type, the location, and the
// names of the rewards.
//
// The definition artifacts of the service leave out the loadout tables,
// which give the name, the icon, and the color of each saved loadout. The
// list_loadouts and save_loadout tools therefore read those tables from
// the Destiny manifest that Bungie publishes. The manifest gives the path
// of the content file of each definition table. Each path carries an
// identifier that Bungie changes with every manifest release, so a path
// names one version of the table. The server reads the manifest on every
// call that needs a loadout table.
//
// The server downloads a loadout table only when the manifest gives a path
// that differs from the path of the loaded table. It keeps each table only
// in memory. When the names do not load, list_loadouts gives the reason
// and lists the loadouts by number. The server keeps only a table that
// loaded, so a failed load leaves any kept table in place. The next call
// tries again, and it downloads the table unless the server already holds
// the table of the path that the manifest gives.
//
// The save_loadout tool reads the icon table and the color table only for
// an empty slot, which takes the first icon and the first color. The first
// entry of a table is the entry with the lowest index among the entries
// that Bungie shows, as in Destiny Item Manager. The tool refuses a save
// that needs a table that did not load, and the refusal gives the reason.
//
// Tags follow the web client. An item carries at most one of the tags that
// the web client defines, and the favorite mark is a separate flag.
//
// The web client evaluates autotag rules. The save_autotag_rule tool
// parses each query that a call passes with a port of the search parser of
// the web client. It refuses a query that the parser rejects, such as a
// query with a quote that never closes. It also refuses a query that the
// web client runs differently from its text. For example, the web client
// reads the unquoted word "or" in "is:weapon or" as a search for the word.
// The tool also refuses a filter value that the web client rejects, such
// as count:=<2, for the filters that preview_autotag_rule evaluates. It
// saves a query with other terms, because the web client might accept
// them, and its result names those terms.
//
// When a call passes a query that holds only the filters of the preview,
// the save_autotag_rule result counts the owned items that the query
// matches. The count covers that query alone, as if no other rule existed,
// so an earlier rule can take some of those items. The result also counts
// the items whose outcome the server cannot decide, which the Autotag
// Preview section describes. The tool saves a query that reads a game mode
// without ratings. Its result then gives the reason of the mode in place
// of the counts.
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
// # Autotag Preview
//
// The preview_autotag_rule tool lists the owned items that an autotag
// rule query matches, so that the user can test a rule before saving it.
// The tool evaluates the query over the items that the rule editor of the
// web client previews. Those items are the instanced weapons, armor, and
// other equipment in every location, the vault and the Postmaster
// included. The rule editor leaves out emotes, finishers, and the dummy
// items that DIM never shows, and so does the tool. The preview ignores
// the order of the rules, so an earlier rule can take an item that the
// preview lists.
//
// The tool evaluates a closed set of filters. One term outside the set
// voids the whole preview, because the web client voids a whole query
// when one term is invalid. The tool then lists nothing and names each
// such term. Bare words and quoted strings are terms outside the set. So
// are the is: words and the filter names that the list of filters leaves
// out. The web client might still accept such a term.
//
// A filter value that the web client rejects, such as count:=<2 or
// dupe:foo, also voids the preview. The web client skips an autotag rule
// whose query holds such a value, even a value that its search bar
// completes, such as dupe:ite. The tool returns an error that says so
// and names each such term. The error leaves out the terms outside the
// set, because the web client skips the rule whatever those terms mean. A
// dupe: value that the web client accepts but the tool does not evaluate,
// such as dupe:stats, is a term outside the set.
//
// The list of filters gives the meaning of each one. Each is: word also
// works as not:<word>.
//
//   - is:weapon: an item of the kinetic, energy, or power weapon bucket.
//   - is:armor: an item of the helmet, gauntlets, chest armor, leg armor,
//     or class armor bucket.
//   - is:equipment and is:equippable: an item that DIM marks as equipment.
//     Every item of the preview is equipment.
//   - is:equipped: an item that a character has equipped.
//   - is:invault: an item in the vault.
//   - is:postmaster and is:inpostmaster: a lost item at the Postmaster,
//     or an item in the Engrams, Messages, or Special Orders bucket.
//   - is:locked: a locked item that the game can lock.
//   - is:unlocked: an unlocked item that the game can lock.
//   - is:titan, is:hunter, and is:warlock: an item of that class.
//   - is:favorite: an item with the favorite mark.
//   - is:autorifle, is:handcannon, is:pulserifle, is:scoutrifle,
//     is:sniperrifle, is:shotgun, is:rocketlauncher, is:sidearm, is:sword,
//     is:tracerifle, is:bow, and is:glaive: a weapon of the type that the
//     word names.
//   - is:fusionrifle: a fusion rifle. A linear fusion rifle does not
//     match.
//   - is:machinegun and is:lmg: a machine gun.
//   - is:linearfusionrifle and is:lfr: a linear fusion rifle.
//   - is:submachine and is:smg: a submachine gun.
//   - is:grenadelauncher: a grenade launcher of any ammo type.
//   - is:heavygrenadelauncher: a grenade launcher of the power weapon
//     category.
//   - is:specialgrenadelauncher: a grenade launcher outside the power
//     weapon category.
//   - is:transmat: a transmat effect.
//   - is:weaponmod: a weapon mod.
//   - is:armormod: an armor mod.
//   - is:reptoken: a reputation token.
//   - is:primary, is:special, and is:heavy: an item that takes the ammo
//     type that the word names. An item whose definition gives no ammo
//     type, such as armor, matches none of them.
//   - is:dupe and dupe:item: an item that shares its name, class, rarity,
//     and bucket with another item of the account, in any location. The
//     web client keeps the suffix (Adept), (Timelost), or (Harrowed) in a
//     weapon name, so such a weapon never matches its base weapon. Only
//     equipment that the game can lock matches, except emblems, artifacts,
//     and the default shader.
//   - is:incurrentchar: an item of the character that the player played
//     last, or an item in an account-wide bucket other than the vault.
//   - is:currentclass: an item of the class of the character that the
//     player played last.
//   - is:common and is:white: an item of the rarity Common.
//   - is:uncommon and is:green: an item of the rarity Uncommon.
//   - is:rare and is:blue: an item of the rarity Rare.
//   - is:legendary and is:purple: an item of the rarity Legendary.
//   - is:exotic and is:yellow: an item of the rarity Exotic.
//   - is:kinetic, is:arc, is:solar, is:void, is:stasis, and is:strand: an
//     item whose instance deals the damage type that the word names.
//   - is:light: an item whose instance deals Arc, Solar, or Void damage.
//   - is:dark: an item whose instance deals Stasis or Strand damage.
//   - is:engrams, is:lostitems, is:messages, and is:specialorders: an
//     item whose definition names the Postmaster bucket of the word. A
//     weapon at the Postmaster matches its weapon slot and not
//     is:lostitems.
//   - is:kineticslot, is:energy, is:power, is:helmet, is:gauntlets,
//     is:chest, is:leg, and is:classitem: an item whose definition names
//     the weapon or armor bucket of the word.
//   - is:subclass, is:seasonalartifacts, is:ghost, is:emblems, is:ships,
//     is:vehicle, is:emotes, is:finishers, is:accessories,
//     is:consumables, and is:modifications: an item whose definition
//     names the bucket of the word. The preview leaves out emotes and
//     finishers, so is:emotes and is:finishers match nothing.
//   - is:haspower and is:haslight: an item with a power that DIM counts.
//   - is:autotagged: an item whose tag an autotag rule set. A tag that a
//     person sets, such as with set_tag, clears the mark.
//   - tag:<value>: an item whose tag is the value. Only the tags that the
//     web client defines count, so tag:none matches nothing. The value
//     favorite matches an item with the favorite mark.
//   - count:<range>: an item whose number of copies is in the range, such
//     as count:>=2. The copies are the items of the account with the name,
//     class, rarity, and bucket of the item. Only equipment that the game can
//     lock counts, except emblems. The range is an operator and a number.
//     The operator is =, <, <=, >, >=, or empty, and the empty operator
//     tests equality.
//   - id:<value>: the item with that instance id.
//   - hash:<value>: an item with that item hash. The tool reads the value
//     as the web client does, so hash:12abc is hash:12.
//   - name:<text>: an item with a word in its name that starts with the
//     text, so name:hunger matches Gnawing Hunger and name:unger does not.
//     When the folded text starts with a character other than an ASCII
//     letter, an ASCII digit, or an underscore, the match can start
//     anywhere in the name.
//   - exactname:<text>: an item whose whole name is the text, so
//     exactname:"gnawing hunger" does not match Gnawing Hunger (Adept).
//   - power:<range> and light:<range>: an item whose power is in the
//     range, such as power:>=1900. The range has the syntax of count:. An
//     item without a power that DIM counts matches no range, so
//     power:<10 skips it. DIM also reads the words powerfloor, softcap,
//     powerfulcap, and pinnaclecap as the power levels of the season. The
//     tool lacks those levels, so a range with such a word is a term
//     outside the set.
//   - rate:<range> and rating:<range>: a copy whose roll has a tier in the
//     range on the Aegis sheet. The tier of the roll is the tier that the
//     get_item tool gives the copy. The web client also tests the tier of
//     each set bonus of the item.
//   - ratebase:<range> and ratingbase:<range>: an item whose row on the
//     Aegis sheet has a tier in the range, whatever the copy rolled. The
//     web client also tests the tier of each set bonus of the item.
//   - misses:<range>: a copy whose count of missed perk columns is in the
//     range, such as misses:>=1. Only a copy on a row with perk columns
//     has the count.
//   - pvprate:, pvprating:, pvpratebase:, pvpratingbase:, and pvpmisses:
//     each match what the filter without the prefix pvp matches, but they
//     read the PvP sheet.
//
// The rarity filters read the tier type of the definition, as DIM does,
// and not the tier name that list_items shows. DIM reads a definition
// without a tier type as Common. The damage type filters read only the
// damage type of the instance, as the web client does, so an item whose
// instance has none matches none of them. DIM counts the primary stat of
// an item as its power when the stat is Attack, Defense, or Power. It
// does not count the primary stat of a subclass, of an artifact, or of an
// item whose definition hides the primary stat.
//
// The range of rate:, ratebase:, and their other names takes a tier
// letter or a number. Each tier stands for a number, from 6 for S to 0 for
// F, so rate:>=b matches B or better and rate:>=5 matches A or better. The
// range of misses: takes only a number. A weapon that the sheet does not
// list and a weapon on a row without a tier match no range of rate: or
// ratebase:. The rating filters rate each roll as the Roll Ratings section
// describes. A rating can therefore differ from the rating of the web
// client in the ways that the section lists.
//
// Each rating filter reads the sheet, the perk preferences, and the
// definitions of its game mode. A query that reads a game mode without
// ratings voids the whole preview. The tool then returns an error result
// that gives the reason of the mode, as a minimum rating of list_items
// does. A query reads only the game modes of its rating filters, so the
// other mode can lack ratings. The tool reads no sheet for a query without
// a rating filter.
//
// The server cannot decide the outcome of some items. The preview lists
// such items after the matches, in a section of their own, with the reason
// of each. It never counts them as matches. The section shows at most
// limit items, and the offset pages only the matches. The server leaves an
// item undecided for these reasons:
//
//   - The server does not read set bonuses, which rate: and ratebase: also
//     test. It treats a weapon as an item without set bonuses. Every other
//     item is undecided at those filters, unless the tier of its roll or
//     its row passes the range.
//   - The profile lists no perks for a copy on a row with perk columns.
//     The web client can still rate the copy from the definitions, so the
//     copy is undecided at rate: and misses:. The server decides such a
//     copy at ratebase:, and at rate: on a row without a tier.
//
// The operators combine an undecided term with the other terms. An and
// does not match when one of its terms does not match, and an or matches
// when one of its terms matches. Otherwise an undecided term makes the and
// or the or undecided. A negated undecided term stays undecided. The query
// is:weapon rate:>=a therefore decides every item outside the weapon
// buckets. Among the weapons, it leaves undecided only a copy without perks
// on a row with a tier and perk columns.
//
// The name filters fold the item name and the text before they compare
// them, as the web client does. The fold removes letter case and
// diacritics, so name:deja matches a name that writes deja with accents.
// The filters also read the curly quotes U+2018 through U+201A and U+201C
// through U+201E in the name as ASCII quotes, as the parser reads them in
// the query. A straight apostrophe in the text therefore matches a curly
// one in the name.
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
// # Upstream
//
// The server follows the web client and the service of Agg's Destiny
// Vault Manager at the commit that passthru.upstream.rev in package.nix
// names. The server mirrors a subset of upstream on purpose, so the
// commit records the upstream code that the server was last checked
// against. The upstream repository vendors DIM as the vendor/DIM
// submodule. The DIM commit that the server follows is the pin of that
// submodule in the upstream commit. The command git ls-tree <rev>
// vendor/DIM prints it in a clone of the upstream repository. For the
// current pin, the DIM commit is dd9fc5d01041a218fcebd536f4de11678057d8ae.
//
// An upgrade diffs upstream from the pinned commit to the head of its
// default branch. It also diffs DIM from the submodule pin of the old
// commit to the submodule pin of the new commit. It then moves
// passthru.upstream.rev to the new commit and sets the DIM commit in
// this section to the submodule pin of the new commit.
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
