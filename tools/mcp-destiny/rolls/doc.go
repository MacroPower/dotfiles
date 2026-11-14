// Package rolls rates the roll of one weapon copy against the roll sheets
// that the service of Agg's Destiny Vault Manager publishes.
//
// A caller keeps one sheet store for the process and serializes its loads.
// Each call that rates rolls loads the sheet of each game mode and reads
// the perk preferences of the user. It then builds the rating source of
// each mode from the sheet, the perk stances of the mode, and the Destiny
// definitions. It rates each weapon copy against the source of each mode.
// A mode without a sheet carries the reason in its source, so every rating
// of the mode carries that reason.
//
// # Sheets
//
// The service publishes one roll sheet for each game mode. The Aegis sheet
// rates weapons for PvE, and the service publishes it as the rolls
// artifact. The PvP sheet rates weapons for PvP, and the service publishes
// it as the pvprolls artifact. Each sheet gives a weapon a tier from S,
// the best tier, to F. A row of the sheet can also leave out the tier. The
// row names the category of the weapon on the sheet. It can give the rank
// of the weapon in that category and a note. A sheet also gives a
// tier to some armor set bonuses, which it keys by the sandbox perk of each
// bonus.
//
// A sheet picks the perks that it wants in each perk column of a weapon.
// It fills a column from every version of the weapon, so a pick can be a
// perk that one version cannot roll. A reissue shares the row of the
// original weapon. The row of an exotic has no perk columns.
//
// # Ratings
//
// The tier of a copy starts at the tier of its row. It moves down one step
// for each perk column that the copy misses, and it stops at F. A copy
// hits a column with each pick of the column in the socket of the column.
// That socket is the first socket of the copy that holds a pick. When no
// socket of the copy holds a pick, the plug sets of the weapon definition
// name the socket. The column is a miss when the socket holds no perk that
// counts.
//
// A row without perk columns gives its tier to every copy. A copy without
// perks on a row with columns has an unknown roll, because the profile
// does not say which perks the copy rolled.
//
// # Perk Stances
//
// The user likes or dislikes perks for each game mode, and the rating of a
// mode reads only the stances of that mode. A disliked perk never counts,
// also when the sheet picks it, and the picks of its column leave it out.
// A liked perk counts in the socket of its column, also when the sheet
// does not pick it. An enhanced perk takes the stance of its base perk.
//
// # Store
//
// The service names each artifact file after a hash of the content, so a
// file name identifies a sheet. The store reads the artifact index of the
// service on every load. It downloads a sheet only when the index lists a
// file name that differs from the name of the sheet that it keeps for the
// mode. It keeps the last sheet that loaded for each mode in memory. A
// load that does not succeed changes nothing that the store keeps, so the
// next load fetches the file again. The reason of a mode without a sheet
// wraps [ErrNoRating].
package rolls
