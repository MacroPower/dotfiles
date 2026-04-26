// Cookie is a tiny self-contained fortune cookie generator with an
// optional cowsay-style renderer baked in.
//
// # Flags
//
//   - -s: only consider entries whose byte length is at most
//     [shortLen]. The dashboard relies on this to keep fortunes
//     inside the cowsay box.
//   - -cow: render the fortune in a cowsay-style speech bubble with
//     the named character. The reserved value [randomCow] picks one
//     uniformly from the embedded cows. Empty (the default) prints
//     the bare fortune.
//   - -w: wrap width for the bubble body. Defaults to
//     [defaultWidth]. Must be positive.
package main
