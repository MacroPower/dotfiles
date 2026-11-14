// Package fold folds the names that the tools of the server look up. Every
// tool therefore matches a name that the user types in the same way. A
// lookup folds the name that it searches and the text that the user
// passes, and it compares the two folded texts. The fold removes letter
// case and diacritics, so the texts xur and Xur both match the vendor name
// Xûr.
//
// The fold follows plainString of Destiny Item Manager (DIM) for a
// Latin-based language. The web client of Agg's Destiny Vault Manager
// always searches in English, which DIM marks as Latin-based. The name
// filters of the autotag rule queries therefore fold a name as the web
// client does.
package fold
