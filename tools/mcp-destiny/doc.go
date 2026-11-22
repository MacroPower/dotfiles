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
// The tools and the login subcommand print every time in the local time
// zone of the host that runs them. Each time ends with the abbreviation of
// the zone, such as EST, also when Bungie or the service sent the time in
// UTC.
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
// Bungie profile also holds the records of the account, and some of them
// track the progress of the weapon patterns. The same API performs
// transfers, equips, lock changes, perk changes, and the equips, saves,
// and clears of loadouts. The server joins a Bungie profile with the
// definitions to give every item a name, a type, perks, and stats. The
// definitions also tell which records track a weapon pattern. They name
// each activity of the history, each vendor, and the items that the
// vendors sell. For each activity that a character can play, they give
// the name, the type, the location, and the names of the rewards.
//
// The server also downloads the Clarity database, which holds the tested
// descriptions of the perks, from the site of that project. The Perk
// Descriptions section describes it.
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
// The web client evaluates autotag rules while it is open, and the
// run_autotag_rules tool evaluates them when a caller invokes it.
//
// The save_autotag_rule tool parses each query that a call passes with a
// port of the search parser of the web client. It refuses a query that the
// parser rejects, such as a query with a quote that never closes. It also
// refuses a query that the web client runs differently from its text. For
// example, the web client reads the unquoted word "or" in "is:weapon or" as
// a search for the word. The tool also refuses a filter value that the web
// client rejects, such as count:=<2, for the filters that
// preview_autotag_rule evaluates. It saves a query with other terms,
// because the web client might accept them, and its result names those
// terms.
//
// When a call passes a query that holds only the filters of the preview,
// the save_autotag_rule result counts the owned items that the query
// matches. The count covers that query alone, as if no other rule existed,
// so an earlier rule can take some of those items. The result also counts
// the items whose outcome the server cannot decide, which the Autotag
// Preview section describes. The counts leave out the items that the web
// client does not build, as the preview does, and a line after the counts
// gives the number of such items. The tool saves a query that reads a game
// mode without ratings. Its result then gives the reason of the mode in
// place of the counts.
//
// The web client records profile changes only while it is open in a
// browser. It diffs each Bungie profile that it fetches against the one
// before, and it posts the changes of each 5-minute bucket after the bucket
// closes. The get_stats tool reads those buckets back from the service. It
// therefore reports only the changes that a browser recorded and posted.
//
// The web client records every stack of the profile, also the dummy items
// whose definitions the service withholds. The get_stats tool names such a
// stack when a vendor sells it or an activity rewards it, because the
// service names those dummy items. It names every other such stack as the
// Item Lookups section describes. A row whose item hash still lacks a name
// reads "Unknown item <hash>". A line after the tables then gives the
// reason, such as an item that Bungie classifies or a lookup that failed.
//
// # Item Lookups
//
// The definitions of the service leave out the dummy items, the classified
// items, and the items without a name. They also lack each item that
// Bungie added after their manifest version. The server looks such an item
// up in the live manifest of Bungie, which gives the name that the game
// shows. The lookups name the items of every tool that reads the
// inventory, such as list_items, list_characters, and stage_junk. They
// also name the rows of get_stats.
//
// The server keeps each answer of Bungie for the manifest version of the
// definitions. It keeps the answers in memory and in a cache file beside
// the cache file of the definitions. The servers of several sessions that
// share the cache directory share the answers. An item that Bungie answers
// therefore costs one request for each manifest version, unless two
// servers look it up at the same time. A call sends at most 20 lookups,
// and it waits at most 10 seconds for them. The item of a lookup that the
// call skips or that fails reads "Unknown item <hash>".
//
// The tools that read the inventory reuse a copy of the profile for up to
// 30 seconds, and the copy keeps the outcome of each lookup. Such a tool
// looks up the item of a skipped or failed lookup again only when it
// fetches the profile from Bungie again. It fetches the profile again in
// cases such as a copy older than 30 seconds or a call that sets refresh.
// Every get_stats call looks up each such item again.
//
// # Name Lookups
//
// Several tools take a name that the user types, or a part of one. The
// text names an item, a slot, a perk, a stat, a weapon, a vendor, an
// activity, a loadout name, or a weapon trait. The tools are list_items,
// compare_duplicates, list_patterns, list_vendors, list_activities,
// stage_junk, set_perk, save_loadout, and set_perk_preferences. Each tool
// folds the name that
// it searches and the text before it compares them. The fold removes
// letter case and diacritics, so the text xur matches the name Xûr.
//
// Some names of the game write a digit in place of a letter, such as
// St0mp-EE5, M3-D1 Shell, and Se7enth. The fold of these tools therefore
// also reads each such digit as its letter. It reads 0 as o, 1 as i, 3 as
// e, 5 as s, and 9 as g. It reads 7 both as v and as l, because Se7enth
// writes 7 for v and S733k Pack writes it for l. The texts stomp and st0mp
// both match St0mp-EE5, and the text sleek matches S733k Pack. The fold
// keeps every other character. The text stomp-ees therefore matches the
// whole name St0mp-EE5, and the text stompees matches no part of it.
//
// The fold reads a digit as a letter wherever the digit stands, also in a
// number. An input that filters a list keeps every name that matches after
// the fold. Those inputs are the query, the slot, and the perk of
// list_items, the query of list_patterns, and the query of
// list_activities. The query ooo therefore
// matches 1000 Yard Stare, and the query 10 matches a name that contains
// the letters io.
//
// Every other input picks one name, such as the vendor of list_vendors and
// the name of compare_duplicates. Such an input first compares the names
// with each digit as it stands. It reads the digits as letters only when
// that comparison finds no name. A name that matches with its digits as
// they stand therefore never competes with a name that matches only with
// its digits as letters.
//
// The name: and exactname: filters of the autotag rule queries keep every
// digit, as the web client does. The Autotag Preview section describes
// them.
//
// # Perk Descriptions
//
// Only the get_item tool shows the descriptions of perks, and it shows
// two for each perk that Clarity covers. The first is the in-game text of
// Bungie, which the Destiny definitions hold. That text gives no numbers.
// The second is the description of the Clarity database, a community
// project that tests how the perks play. It gives the numbers, such as
// the size of a damage bonus, its duration, and how it stacks. The web
// client and Destiny Item Manager (DIM) show the same descriptions.
//
// The description of Clarity follows the text of Bungie under the perk,
// and its first line starts with "Clarity:". Clarity covers a part of the
// perks, so a perk without a description of Clarity shows only the text
// of Bungie. One line after the perk list names Clarity as the source.
// The text of Clarity marks two kinds of values:
//
//   - A value in square brackets after the word PvP applies only against
//     players. The value before the brackets applies against combatants.
//   - An up arrow precedes each value that the enhanced perk improves.
//
// The Clarity repository carries no license file. The server therefore
// downloads the database from the Clarity site when it runs, as the web
// client and DIM do. This module holds no copy of the database.
//
// The server keeps the download in a cache file in the cache directory,
// with the version that the site gives it. It asks the site for the
// version at most one time each hour, and it downloads the database
// again only for another version. The servers of several sessions that
// share the cache directory share the download.
//
// The server works without Clarity. When it holds no database, such as
// after a failed download into an empty cache directory, get_item shows
// only the text of Bungie. One line after the perk list then says that
// the tested descriptions are unavailable and gives the reason. The
// server asks the site again after five minutes. A server that holds an
// earlier download keeps showing it while the site does not answer.
//
// Destiny 2 Update 9.7.0 and its hotfixes made the last changes to how
// the perks play. Clarity records when it last updated each description.
// A description from before such a change might describe the old
// behavior. The server holds the list of the perks and the exotic items
// that those patches changed, which comes from the patch notes of Bungie.
// For a listed perk whose description predates the change, get_item adds
// a line that starts with "Might predate the final patch". The line
// names the patch and the perk or the exotic item that it changed.
// Clarity copies the text of a base perk into the entry of each of its
// enhanced perks. The entry of the enhanced perk keeps the date of its
// own upload. The server therefore dates the description of an enhanced
// perk at the later update of the two entries.
//
// Clarity gives many enhanced perks no entry of their own. For an
// enhanced trait, origin trait, barrel, or magazine without one, get_item
// shows the description of the base perk with the same name, as the web
// client does. Some of these descriptions mark the improved values of the
// enhanced perk with up arrows. The others give only the values of the
// base perk. An enhanced weapon frame without an entry shows only the
// text of Bungie. Bungie gives one frame name to the frames of several
// weapon types that play differently.
//
// # Subclass Swaps
//
// The get_item tool lists the plugs that each socket of an item can swap
// to, and the set_perk tool accepts each of them. For a weapon or an
// armor piece, the Bungie profile lists those plugs with the item. For a
// subclass, the profile lists none with the item. The sockets of a
// subclass hold plugs such as its super, its abilities, its aspects, and
// its fragments. Each of them takes its plugs from a plug set that the
// Destiny definitions name. The profile lists the plugs of each plug set,
// some with the profile and some with each character.
//
// The server offers a plug of the set as a swap when the profile or the
// character that holds the subclass lists it and Bungie allows its
// insert. It lists each other plug of the set after "Bungie reports as
// locked:". Bungie reports the unlocks of a subclass from the view of one
// character of the account, so on another character it can report an
// owned aspect or fragment as locked. DIM shows every plug of the set for
// that reason, and it cites issue 1572 of the Bungie API. The set_perk
// tool therefore accepts a locked plug as well, and Bungie refuses the
// insert when the plug is locked.
//
// The two aspect sockets of a subclass share a plug set, and so do its
// fragment sockets. Sockets that share a plug set never hold one plug
// twice in the game. The swaps of such a socket therefore leave out each
// plug that another socket of the set holds.
//
// Each aspect gives the number of fragment slots that it opens, which its
// definition holds as an investment stat. The aspects open the fragment
// sockets of a subclass up to the sum of their fragment slots. Bungie
// reports each other fragment socket as closed. The game does not apply a
// fragment in a closed socket, so the get_item tool leaves out each closed
// socket, whatever it holds. The open fragment sockets offer a fragment
// that a closed socket holds. An open empty aspect or fragment socket
// appears under the name of its placeholder, such as "Empty Fragment
// Socket", with the plugs that can fill it.
//
// No socket offers a placeholder as a swap, so the server cannot empty a
// socket. When several empty sockets of one plug set can take a plug, the
// set_perk tool fills the first of them. An aspect change can open or
// close fragment sockets. The server shows the open sockets from before
// the change until its next fetch of the profile.
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
// The list_patterns tool gives the tier of the weapon of each pattern on
// each sheet when the caller sets include_ratings. The tier belongs to
// the weapon and not to a copy, so the tool reads only the sheets and not
// the perk preferences. The tier is the tier of the craftable definition
// with the name of the pattern. A version that the pattern does not
// craft, such as the Onslaught version of a raid weapon, does not lend
// the weapon its tier.
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
// # Weapon Patterns
//
// The list_patterns tool lists the crafting pattern of each craftable
// weapon with its progress, such as 3 of 5 extractions. The record
// definitions of the service tell which records track a pattern, as
// Destiny Item Manager (DIM) tells them. The records of the Bungie
// profile give the progress of each pattern. The server reads the records
// of the profile first. For a record that the profile lacks, it reads the
// records of the character that played last.
//
// The definitions name each pattern record after its weapon. The server
// therefore matches a copy to its pattern by the name of the copy, as DIM
// does. It looks up the pattern of each copy whose definition names a
// recipe item, as the definition of each craftable weapon does. The game
// also counts the extractions of an Adept, Timelost, or Harrowed copy
// toward the pattern of the base weapon. The definitions of those
// versions name no recipe item, but the server gives each such copy the
// pattern of the base weapon. DIM leaves such a copy without a pattern.
//
// The list_patterns tool lists an Adept, Timelost, or Harrowed copy on the
// row of the base weapon. The get_item tool shows the progress of the
// pattern of each copy on its Pattern line.
//
// Each row of the list_patterns tool also names the places where the
// weapon drops, as the Weapon Sources section describes. With
// include_ratings, the row gives the tier of the weapon on the Aegis sheet
// and on the PvP sheet, as the Roll Ratings section describes. One call
// can therefore rank the patterns by the quality of the weapon and by the
// place that drops its copies. With this_week, the tool keeps only the
// patterns whose weapon a vendor of one character sells now or a featured
// activity of that character drops, as the Weekly Sources section
// describes.
//
// Two flags of list_items, get_item, and compare_duplicates give the
// Deepsight state of a copy. The server reads both from the crafting
// sockets of the copy, as DIM does:
//
//   - deepsight: the copy has Deepsight Resonance, the red border of the
//     game. The first socket of the copy that holds a plug of the
//     category that DIM names CraftingPlugsWeaponsModsMemories is
//     visible.
//   - harmonizable: a Deepsight Harmonizer can give the copy Deepsight
//     Resonance. A visible socket of the copy holds a plug of the category
//     that DIM names CraftingPlugsWeaponsModsExtractors, and the pattern
//     of the copy is incomplete.
//
// The list_patterns tool lists the instance ids of the copies with each
// flag. On a deployment whose artifact index lacks the Record artifact,
// the server cannot tell which records track a pattern. The list_patterns
// tool then returns an error result, and get_item shows no Pattern line.
// The flags still show. The harmonizable flag then counts the pattern of
// each copy as incomplete, as DIM counts the pattern of a copy without a
// pattern record.
//
// Each harmonizable copy also shows the materials that its activation
// spends, both in the flag and in the harmonizable column of
// list_patterns. An activation spends one Deepsight Harmonizer. The
// activation of a raid weapon also spends 15 Spoils of Conquest, so a
// copy reads "1 harmonizer" or "1 harmonizer + 15 Spoils". Bungie
// announced both costs in This Week at Bungie of 2023-05-04. The
// material requirement of the Deepsight Resonance plug leaves the count
// of Spoils of Conquest to the game. The profile therefore does not give
// the cost of one copy. The server tells a raid weapon by the source tags
// of DIM instead, which the ItemSource artifact of the service holds. On a
// deployment whose artifact index lacks that artifact, a copy reads
// "1 harmonizer + 15 Spoils if a raid weapon".
//
// A line before the table of list_patterns gives the Deepsight
// Harmonizers and the Spoils of Conquest that the account holds. Both
// materials stack in the Consumables bucket. The line also gives the
// stacks at the Postmaster, which the game does not spend until the user
// pulls them.
//
// # Game Data
//
// Destiny 2 is frozen, so the game data that the server embeds does not
// go stale. Bungie shipped its final major patch, Update 9.7.0, on
// 2026-06-09. Bungie has shipped only maintenance hotfixes since that
// patch, and the last hotfix is Update 9.7.0.6 of 2026-10-20. Game data
// such as perks, weapon frames, armor archetypes, and stat formulas
// stays as that hotfix left it.
//
// Bungie has announced plans for later Destiny 2 updates but has not set
// a release date for any of them. On 2026-09-17, Bungie announced work
// to bring vaulted campaigns, destinations, and raids back into the
// game. On 2026-10-29, Bungie said that the work stays in pre-production
// until about February 2027 and then needs about 12 months of
// production. Bungie also announced a Destiny 2 "Celebration" that
// brings new updates, and it plans to give more news about the
// Celebration in February 2027.
//
// The server embeds the game data that it takes from a source other
// than the Bungie API and the service, such as the curves that the
// Weapon Curves section describes. The embedded data follows the patch
// notes of Bungie up to the last hotfix. The tools send no request for
// it.
//
// # Weapon Curves
//
// The get_item tool shows a weapon copy in real units. It gives the
// reload time, the ready time, the stow time, and the time to aim down
// sights in seconds. It also gives the distance in meters at which the
// damage falloff starts, from the hip and while the copy aims down
// sights. The compare_duplicates tool gives each of these values in a
// column of its own, so two rolls compare in seconds and meters.
//
// Bungie publishes each weapon stat as a number from 0 to 100. Its API
// does not say how many seconds or meters a stat means. Players measured
// the seconds and the meters in the game for each weapon type and frame.
// The server holds the curves that D2Foundry's Oracle Engine collects
// from those measurements, and it evaluates them at the stats of the
// copy. The values are therefore community measurements and not numbers
// from Bungie. The curves are part of the server, so the tools send no
// request for them.
//
// The values are the base values of the weapon. The Weapons stat of a
// character, armor mods, and perks with a timed bonus change them in the
// game. Perks that scale a time or a distance without a stat bonus, such
// as Snapshot Sights, change them as well.
//
// Oracle Engine describes the game at Update 8.2.6. The patch notes of
// Bungie from Update 8.2.6.1 to Update 9.7.0.6 change some of its values.
// The server takes each new value that the notes state. It shows no
// value where the notes leave the new value open, such as the reload
// time of glaives after Update 9.7.0. The curves package records the
// source of each curve and the last update of its check.
//
// A value that the server does not have reads "unknown" in the get_item
// output, and the reason follows. The compare_duplicates table leaves the
// cell empty, and a line after the title of the group gives the reason.
// The line names the columns that the reason empties, or it opens with
// "Times and falloff" when the reason empties all of them. The reason is
// one of these:
//
//   - The weapon type has no curves, as a sword has none.
//   - The frame of the weapon has no curves, as a frame that the game
//     gained after Update 8.2.6 has none.
//   - The profile lists no perks for the copy, so the server cannot tell
//     its frame.
//   - The copy lacks the stat that the curve reads.
//   - In Oracle Engine, the damage of the weapon does not fall off with
//     distance, as for sniper rifles, bows, linear fusion rifles, and
//     launchers.
//   - Oracle Engine does not measure the value, such as the falloff
//     distance of Lord of Wolves.
//   - Oracle Engine does not say what the value covers. Its reload time
//     of a shotgun can cover one shell or a full reload.
//   - A game update changed the value after the last measurement, or
//     its patch notes do not say whether a change reaches the weapon.
//
// # Weapon Sources
//
// The list_patterns tool names the places where each craftable legendary
// weapon drops, such as a raid, a destination, or Xûr. A weapon can drop
// from several places, and the source cell joins them with commas. The
// Destiny definitions give each weapon the source text of its
// collectible. Update 9.7.0 moved the drops of many weapons but left the
// text as it stood, so the server holds a table of its own.
//
// The table comes from the Legendary Weapons Sources database of
// Blueberries.gg and from the Monument of Triumph Legendary Loot Sources
// sheet of Sax. Both sources describe the game after Update 9.7.0. The
// table lists each legendary weapon that both sources call craftable,
// and it follows the patch notes of Bungie up to the last hotfix. It
// names only the places that drop the version of the weapon that the
// pattern crafts. The source cell of a weapon that the table does not
// list, such as an exotic weapon, is empty. The table is part of the
// server, so the tools do not send any request for it.
//
// # Weekly Sources
//
// With this_week, the list_patterns tool keeps only the patterns whose
// weapon a vendor of one character sells now or a featured activity of
// that character drops. It reads the live vendor stock and the activities
// of the character, as list_vendors and list_activities read them. It
// adds two columns that name the sources of each pattern:
//
//   - sold by: each vendor that sells a copy that advances the pattern
//     now, by the name that list_vendors gives it. Xûr keeps his legendary
//     weapons in the sub-vendor Strange Gear Offers, so the column names
//     Strange Gear Offers for them. A copy of an Adept, Timelost, or
//     Harrowed version counts, as an owned copy does. A version that does
//     not advance the pattern, such as the Onslaught version of a raid
//     weapon, does not count. When another vendor of the character has
//     the same name, the column adds the hash of the vendor in
//     parentheses, such as "Focused Decoding (444807002)". list_vendors
//     selects such a vendor only by its hash. The label of a vendor ends
//     with "(unavailable)" when the character cannot buy any such copy
//     from that vendor. The column skips each vendor that Bungie reports
//     as not accessible.
//   - featured in: each featured activity that drops the weapon.
//
// The filter also keeps a pattern when the character cannot buy its
// weapon from any vendor that sells it. With status=complete or
// status=all, the filter also keeps the complete patterns of those
// weapons, which no source advances.
//
// The featured activities are those that list_activities flags as
// featured. Bungie puts most of them in the featured carousel of the
// Portal. It never puts a raid or a dungeon there, and it lists the
// weekly challenge of a featured raid or dungeon instead. A raid or a
// dungeon with a weekly challenge therefore counts as featured.
//
// The server finds the featured activities that drop a weapon through
// the table of the Weapon Sources section. The table names the activity
// of each raid, dungeon, and exotic mission that it lists, as the Destiny
// definitions name it. It also names the activity of Dares of Eternity. A
// featured activity belongs to such a place when its name equals the
// activity name of the place or continues it with a colon or a
// parenthesis. For example, "Vault of Glass: Master" belongs to the Vault
// of Glass raid. A destination of the table, such as Neomuna, does not
// have an activity. The featured in column therefore never names an
// activity for the weapons of a destination.
//
// A line before the table names every featured activity of the
// character, also when no pattern matches. The user can then tell a week
// without featured activities from a week whose featured activities do
// not drop a weapon with an incomplete pattern.
//
// # Time to Kill and Damage
//
// The get_item tool ends a weapon with its time to kill a Guardian and
// its damage per second. The Bungie API publishes neither number, and it
// does not publish the damage of a shot. The damage package computes the
// numbers from community measurements and from patch notes. D2Foundry's
// Oracle Engine and the Clarity database collect the measurements, and
// the patch notes of Bungie state some damage values. The numbers match
// Update 9.7.0 of the game, and the header of the block names the
// update.
//
// Every number holds against a Guardian with 230 health, without an
// overshield and without damage resistance. Every round hits, and the
// target stands closer than the distance at which the damage falls off.
// The numbers leave out the Weapons stat of a character. The stat
// shortens the reload in the game, and a stat of more than 100 raises the
// damage against players. A combatant takes other damage, which depends
// on its rank and on the activity, and the server does not model it.
//
// The time to kill runs from the first hit to the last one. The block
// gives it with precision hits and with body shots alone. The time with
// precision hits names the hits that can be body shots, such as "2
// precision hits and 1 body shot". The damage per second is the damage of
// one magazine divided by the time of the magazine and of one reload. It
// reads the Magazine stat and the reload time of the copy, so two rolls
// of one weapon can differ in it.
//
// The block gives the base numbers first. No timed perk is active in
// them. One entry then follows for each state of each active perk whose
// effect the server has, such as "With Rampage at 3 stacks". Its numbers
// hold while that perk alone is that strong. A perk that raises the
// Reload Speed stat while it is active, such as Frenzy, shortens the
// reload in the damage per second of its entry.
//
// The last line names each active perk whose effect the numbers leave
// out. The damage data gives some perks an effect, and it lists others
// as perks without an effect on the numbers. Every other perk is on the
// last line, so no number ignores a perk without the line.
//
// The server has the damage of most legendary frames of auto rifles, hand
// cannons, pulse rifles, scout rifles, sidearms, and submachine guns. It
// does not have the damage of these legendary frames:
//
//   - The Support Frame of auto rifles.
//   - The Spread Shot frame of hand cannons.
//   - The Aggressive Burst frame of submachine guns.
//   - The Balanced Heat Weapon and Dynamic Heat Weapon frames.
//   - The Micro-Missile Frame of pulse rifles and sidearms.
//
// A weapon without numbers shows one line that reads "unknown" with the
// reason. The reason is one of these:
//
//   - The damage data lacks the weapon type, as it lacks swords.
//   - The damage data lacks the frame. It lacks the frames of exotic
//     weapons, the Support Frame of auto rifles, the Spread Shot frame of
//     hand cannons, and the Aggressive Burst frame of submachine guns. It
//     also lacks the Balanced Heat Weapon and Dynamic Heat Weapon frames
//     and the Micro-Missile Frame of pulse rifles and sidearms.
//   - The profile lists no perks for the copy, or no frame among them.
//   - The copy lacks the Rounds Per Minute stat or the Magazine stat.
//   - The Rounds Per Minute stat of the copy differs from the rate of fire
//     that the damage data gives the frame. The data then describes
//     another frame, or the game changed the frame after the measurement.
//
// One value of a weapon with numbers can also be unknown, and its line
// gives the reason. A damage per second is unknown when the reload time
// of the copy is unknown. The numbers with precision hits are unknown for
// High-Impact auto rifles. Update 9.7.0 lowered their base damage and did
// not state their precision damage.
//
// Oracle Engine describes the game at Update 8.2.6. The damage package
// takes each value that the patch notes of Bungie state from that update
// to Update 9.7.0.6. It records the source of each value and the last
// update of its check.
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
// The web client builds no item whose item set the EquipableItemSet
// definitions lack. When the service publishes no EquipableItemSet
// artifact, it builds no item with an item set at all. No rule sees such
// an item, so the preview leaves it out too, also from the items that
// is:dupe, count:, and verdict: compare. The last line of the result
// counts the items that the preview leaves out for this reason and names
// the reason. The other tools, such as list_items, keep such items.
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
//     get_item tool gives the copy. An item with a set bonus whose tier is
//     in the range also matches.
//   - ratebase:<range> and ratingbase:<range>: an item whose row on the
//     Aegis sheet has a tier in the range, whatever the copy rolled. An
//     item with a set bonus whose tier is in the range also matches.
//   - misses:<range>: a copy whose count of missed perk columns is in the
//     range, such as misses:>=1. Only a copy on a row with perk columns
//     has the count.
//   - setbonus:<value>: an item with a set bonus that the Aegis sheet
//     rates at the tier of the value. The value is a tier from s to f, such
//     as setbonus:a. The value 2pc:<tier> or 4pc:<tier> also requires that
//     the bonus need 2 or 4 pieces of the set, such as setbonus:4pc:s. The
//     web client rejects every other value, such as setbonus:3pc:s.
//   - verdict:<value>: a copy whose verdict against the other copies on
//     its row of the Aegis sheet is the value. The value is better, same,
//     worse, only, or mixed. A weapon that the sheet does not list and a
//     copy on a row without perk columns match no value.
//   - pvprate:, pvprating:, pvpratebase:, pvpratingbase:, pvpmisses:,
//     pvpsetbonus:, and pvpverdict: each match what the filter without the
//     prefix pvp matches, but they read the PvP sheet.
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
// The set bonuses of an item are the bonuses of its item set that the
// sheet rates. The sheet rates each bonus by the sandbox perk that the
// set definition names. rate:, ratebase:, and their other names test the
// tier of each such bonus besides the tier of the roll or the row. An item
// therefore matches when one of its set bonuses has a tier in the range,
// whatever its roll. An item without an item set has no set bonus. The
// rating filters include setbonus: and pvpsetbonus:.
//
// The verdict filters compare the perk columns that a copy hits with the
// columns that each of its rivals hits, as the web client does. The
// rivals of a copy are the other copies of each weapon on the sheet row
// that gives the copy its perk columns, in every location, the vault
// included. A reissue that shares the row of its weapon is therefore a
// rival. The first of these rules that holds gives the verdict:
//
//   - only: the copy has no rival.
//   - worse: a rival hits each column that the copy hits, and another
//     column.
//   - same: a rival hits the columns that the copy hits and no other.
//   - better: the copy hits each column that each rival hits, and another
//     column.
//   - mixed: every other copy with rivals.
//
// Two copies whose columns come from different rows compare the columns
// by position, as the web client does. The rating filters include
// verdict: and pvpverdict:.
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
//   - The profile lists no perks for a copy on a row with perk columns.
//     The web client can still rate the copy from the definitions, so the
//     copy is undecided at rate: and misses:. The server decides such a
//     copy at ratebase:, and at rate: on a row without a tier. It also
//     decides such a copy at rate: when a set bonus of the item has a tier
//     in the range.
//   - The profile lists no perks for a copy on a row with perk columns,
//     or for one of its rivals. The web client gives no verdict to a copy
//     whose sockets come from the definitions, and it leaves such a rival
//     out. The copy is undecided at each value of verdict:, also when
//     another rival already makes the copy worse.
//
// The operators combine an undecided term with the other terms. An and
// does not match when one of its terms does not match, and an or matches
// when one of its terms matches. Otherwise an undecided term makes the and
// or the or undecided. A negated undecided term stays undecided. The query
// is:exotic rate:>=a therefore decides every item that is not exotic.
//
// The name filters fold the item name and the text before they compare
// them, as the web client does. The fold removes letter case and
// diacritics, so name:deja matches a name that writes deja with accents.
// It keeps every digit, so name:stomp does not match St0mp-EE5. The other
// tools that look up a name also read such a digit as a letter, as the
// Name Lookups section describes. The preview shows what a rule matches
// in the web client, so the name filters keep the fold of the web client.
// The filters also read the curly quotes U+2018 through U+201A and U+201C
// through U+201E in the name as ASCII quotes, as the parser reads them in
// the query. A straight apostrophe in the text therefore matches a curly
// one in the name.
//
// # New Item Matches
//
// The list_autotag_rules tool with matches=new shows the rule that takes
// each item that the next claim offers to the rules. It never claims, tags,
// or moves an item. The web client or the run_autotag_rules tool therefore
// claims the same items, whichever runs first with autotagging on. The tool
// reads a fresh inventory, because a cached one can miss the newest drops.
// Its answer describes the rules and the inventory at the moment of the
// call. While autotagging is off, the tool says so and reads no inventory,
// because the web client runs the rules only while autotagging is on.
//
// The claim covers the items whose instance id is higher than the mark
// that the service stores. The web client pads each id with zeros to 20
// digits and compares the padded ids as text, and so does the tool. It
// counts the items that the rule editor previews, which the Autotag
// Preview section describes. Emotes, finishers, and the items that the
// web client does not build therefore never count. The claim moves the
// mark to the highest such id, the vault included. The web client offers
// the rules only the new items outside the vault. The tool therefore
// counts the new items in the vault and gives them to no rule. It counts
// an equipment item in an account bucket as an item outside the vault.
// DIM files such an item under the vault when its bucket has no vault
// bucket. The web client then does not offer the item to the rules.
//
// Each new item goes to the first enabled rule, in rule order, whose query
// matches it. A rule without a tag and without the vault flag still takes
// its items. The filters of each rule read every item, the vault included,
// so is:dupe counts the copies in the vault. Before it reads any item, the
// tool puts each rule in one of three classes:
//
//   - Skipped: the web client skips the rule, so the rule takes nothing.
//     The web client skips a rule whose query has no term or holds a value
//     that the web client rejects. It also skips a rule whose query its
//     search parser rejects.
//   - Blocked: the web client runs the rule, or might run it, but the
//     server cannot evaluate it. The server cannot evaluate a query that
//     the web client runs differently from its text. It also cannot
//     evaluate a query with a term outside the filters of the preview, or
//     a query that reads a game mode without ratings.
//   - Evaluated: the server evaluates the query of the rule.
//
// The tool classifies a rule by the first problem of its query. The
// problems come in this order: a parse error, a value that the web client
// rejects, a term outside the filters of the preview, and a game mode
// without ratings. The tool cannot read the values of a query that the
// web client runs differently from its text. Such a rule therefore blocks
// even when the web client rejects one of its values and skips the rule.
//
// A blocked rule might take any item that reaches it. The tool therefore
// lists those items under the first enabled rule that blocks, and it
// evaluates no later rule. An item that an enabled rule leaves undecided
// stops at that rule for the same reason. The tool lists such an item with
// the rule and the reasons. The output names the reason of each skipped or
// blocked rule.
//
// # Owned Rule Counts
//
// The list_autotag_rules tool with matches=owned counts the owned items
// that each rule matches, as the rule editor of the web client counts them
// beside each rule. A model can therefore check an order of the rules
// before the next claim. The tool counts the items that the Autotag
// Preview section describes, the vault included. It reads an inventory up
// to 30 seconds old, so the counts describe the owned items and can miss
// the newest drops. The counts do not depend on the autotag state. The
// tool counts the stored rules. The editor counts the rules on screen,
// which might differ from the stored rules until the user saves them. An
// account without a character gets no counts, because the web client
// matches every item when it has no current character.
//
// The tool puts each rule in the classes that the New Item Matches section
// describes. It walks the rules in order and counts each evaluated rule
// over the items that no earlier enabled rule took. An enabled rule then
// takes the items that it counts, and a disabled rule takes nothing. A
// rule without a tag and without the vault flag still counts and takes
// items. The editor shows the missing action of such a rule in place of
// its count. The output names each skipped rule with its skip reason, and
// the rule takes nothing.
//
// The line of a blocked rule reads "Cannot count" and gives the reason.
// The server cannot tell which items an enabled rule takes when the rule
// blocks. Each later evaluated or blocked rule therefore reads "Not
// counted". An enabled rule that leaves an item undecided has the same
// effect, after its own count and the number of its undecided items. A
// later skipped rule keeps its skip line, because the web client counts
// nothing for it whatever the rules before it take. A disabled rule takes
// nothing, so the rules after it still count.
//
// # Autotag Runner
//
// The run_autotag_rules tool runs the autotag rules when a caller invokes
// it, so a user can tag the newest drops without the web client. The
// server never runs the rules on its own. The tool reads the autotag state
// and a fresh inventory, and it selects the new items that the New Item
// Matches section describes. It evaluates every enabled rule over those
// items before it claims anything.
//
// The tool claims nothing when the server cannot apply every enabled rule
// the way the web client does. It then returns an error result that lists
// every cause, so the user can fix every rule in one pass. These causes
// stop a claim:
//
//   - An enabled rule that the web client skips. A claim takes the new
//     items from every runner, and the user saved the rule to act on them.
//   - An enabled rule that blocks. Such a rule might take an item, so it
//     can change which rule takes each item after it.
//   - A new item that an enabled rule leaves undecided before any rule
//     matches it.
//
// The tool also claims nothing while autotagging is off. It claims
// nothing when no owned item has an instance id higher than the stored
// mark, and when the account has no character. The web client claims the
// new vault items of an account without a character. A claim by the tool
// takes those items and tags none of them.
//
// When no cause stops it, the tool claims the new items through the
// service. The service applies the claim only while autotagging is on and
// the stored mark still equals the mark that the tool read. An applied
// claim therefore covers the items that the tool evaluated. When the
// service does not apply the claim, another runner claimed the items
// first or a client turned autotagging off. The tool then tags nothing.
//
// After an applied claim, the tool tags each item that a rule with a tag
// takes. The tag row carries the id of the rule, as in the web client, so
// is:autotagged matches the item. The tool clears the other tag of the
// item and keeps the favorite mark. A tag that a person set on a new item
// therefore gives way to the tag of the rule. A rule without a tag takes
// its items and tags none of them.
//
// After the tag write, the tool moves the items of each enabled rule with
// the vault flag to the vault, as the web client does. The moves also run
// after a failed tag write, because the web client moves the items whatever
// its tag write gives. A rule with the vault flag and without a tag moves
// its items and tags none of them. The tool moves one item at a time, in
// the order of the instance ids. It pulls an item from the Postmaster
// before the transfer. The result lists the moved items.
//
// The tool leaves an item in place, and the result names the item with one
// of these reasons:
//
//   - A character has the item equipped.
//   - Bungie reports the item as not transferable. The web client reads
//     the transfer status of the item and compares it with the
//     NotTransferrable value alone.
//   - The definition puts the item in a bucket that the web client never
//     moves an item from. Those are the bucket of the distorted engrams
//     and the Engrams, Lost Items, Messages, and Special Orders buckets of
//     the Postmaster.
//   - The server treats the item as not transferable, but the web client
//     tries the transfer. The server also reads the definition and every
//     transfer status with the NotTransferrable bit, and it refuses to
//     transfer such an item.
//
// A failed move does not stop the next one. The result names the item with
// the error and the location of the item. It is an error result, so that
// the user moves the item by hand. The web client runs the move code of
// Destiny Item Manager, which moves another item out of a full vault to
// make room. The tool moves only the items that the rules take, so Bungie
// refuses a transfer to a full vault. The result then lists a failed move.
//
// A claim happens at most once. No runner offers the claimed items to a
// rule again, also when a step after the claim fails. The tool retries
// neither the claim, the tag write, nor a move. When the tag write fails,
// the result names each item with its tag, so that the user can set the
// tags with set_tag.
//
// A claim request that fails might still have moved the mark in the
// service. The tool then reads the autotag state again, and it tags
// nothing and moves nothing. When the mark is still the stored mark, the
// tool claimed nothing. When the mark moved or the read fails, the claim
// might have applied. The result then lists the items that the rules tag,
// each with its rule and its tag, for set_tag. It also lists the items
// that the tool moves to the vault after an applied claim, each with its
// rule, for move_items.
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
//   - --cache-dir: directory of the definition cache, the item name
//     cache, and the Clarity cache (default: $XDG_CACHE_HOME/mcp-destiny)
//   - --log-file: path to JSON log file (append), which records tool
//     errors, failed moves, failed lock changes, failed loadout equips,
//     failed item lookups, and failed loads of the Clarity database
package main
