// Package fold folds the names that the tools of the server look up. A
// lookup folds the name that it searches and the text that the user
// passes, and it compares the two folded texts.
//
// [Name] removes letter case and diacritics, so the texts xur and Xur both
// match the vendor name Xûr. It follows plainString of Destiny Item
// Manager (DIM) for a Latin-based language. The web client of Agg's
// Destiny Vault Manager always searches in English, which DIM marks as
// Latin-based. The name filters of the autotag rule queries fold with
// [Name], so they match a name as the web client does.
//
// [Lookalike] folds more. Some names of the game write a digit in place
// of a letter, such as St0mp-EE5, M3-D1 Shell, and S733k Pack. [Lookalike]
// folds as [Name] does and then reads each such digit as its letter. The
// texts stomp and st0mp therefore both match St0mp-EE5. The digit 7
// stands for v in one name and for l in another, so [Lookalike] reads a
// text in two ways. Two texts match when they match in one of the two.
//
// Each lookup of a text that the user passes uses [Lookalike], except the
// name filters. A lookup that filters a list, such as the query of
// list_items, folds with [Lookalike]. That fold also reads a digit of a
// number as a letter, so the query ooo matches 1000 Yard Stare. A lookup
// that picks one name, such as the vendor of list_vendors, applies the
// folds of [Passes] in order. A name that matches after the fold of [Name]
// therefore never competes with a name that matches only under
// [Lookalike].
//
// A lookup that compares two names from game data folds with [Name],
// because the user types neither name. The clarity package finds the
// base perk of an enhanced perk with such a lookup.
package fold
