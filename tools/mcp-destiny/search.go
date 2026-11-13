package main

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Bucket hashes that the search filters and the preview population read.
// Each value is a BucketHashes value of Destiny Item Manager (DIM).
const (
	bucketKinetic       uint32 = 1498876634
	bucketEnergy        uint32 = 2465295065
	bucketPower         uint32 = 953998645
	bucketHelmet        uint32 = 3448274439
	bucketGauntlets     uint32 = 3551918588
	bucketChestArmor    uint32 = 14239492
	bucketLegArmor      uint32 = 20886954
	bucketClassArmor    uint32 = 1585787867
	bucketEngrams       uint32 = 375726501
	bucketMessages      uint32 = 3161908920
	bucketSpecialOrders uint32 = 1367666825
	bucketEmotes        uint32 = 1107761855
	bucketFinishers     uint32 = 3683254069
	bucketEmblems       uint32 = 4274335291
	bucketArtifacts     uint32 = 1506418338

	// bucketForbidden and bucketDummy hold the dummy items that DIM
	// leaves out of every store. DIM names them THE_FORBIDDEN_BUCKET
	// and SOME_OTHER_DUMMY_BUCKET.
	bucketForbidden uint32 = 2422292810
	bucketDummy     uint32 = 3621873013
)

// Item category hashes that the search filters and [itemCategories] read.
// Each value is an ItemCategoryHashes value of DIM. A special grenade
// launcher holds the negative of categoryGrenadeLaunchers.
const (
	categoryWeapon                  int64 = 1
	categoryPowerWeapon             int64 = 4
	categoryAutoRifle               int64 = 5
	categoryHandCannon              int64 = 6
	categoryPulseRifle              int64 = 7
	categoryScoutRifle              int64 = 8
	categoryFusionRifle             int64 = 9
	categorySniperRifle             int64 = 10
	categoryShotgun                 int64 = 11
	categoryMachineGun              int64 = 12
	categoryRocketLauncher          int64 = 13
	categorySidearm                 int64 = 14
	categorySword                   int64 = 54
	categoryGrenadeLaunchers        int64 = 153950757
	categoryShipModsTransmatEffects int64 = 208981632
	categoryWeaponMods              int64 = 610365472
	categoryLinearFusionRifles      int64 = 1504945536
	categoryReputationTokens        int64 = 2088636411
	categoryTraceRifles             int64 = 2489664120
	categoryDummies                 int64 = 3109687656
	categoryBows                    int64 = 3317538576
	categoryGlaives                 int64 = 3871742104
	categorySubmachineGuns          int64 = 3954685534
	categoryArmorMods               int64 = 4104513227
)

// hashDefaultShader is the item hash of the default shader, which DIM
// leaves out of the matches of its dupe filters. DIM names it
// DEFAULT_SHADER.
const hashDefaultShader uint32 = 4248210736

// Filter names of the search syntax that the preview evaluates.
const (
	filterIs        = "is"
	filterTag       = "tag"
	filterDupe      = "dupe"
	filterCount     = "count"
	filterID        = "id"
	filterHash      = "hash"
	filterName      = "name"
	filterExactName = "exactname"
)

// jsDot is a character class that matches what a dot matches in a
// JavaScript regular expression without the u flag. Such a dot matches one
// UTF-16 code unit other than the line terminators \n, \r, U+2028, and
// U+2029. A dot that an ASCII character follows therefore never matches a
// character outside the Basic Multilingual Plane, whose two code units it
// splits. The dot of Go matches every character except \n.
const jsDot = `[^\n\r\x{2028}\x{2029}\x{10000}-\x{10ffff}]`

var (
	// ErrUnsupportedQuery reports a search query with a term that the
	// preview does not evaluate.
	ErrUnsupportedQuery = errors.New("unsupported query")

	// errUnsupportedTerm reports a filter or a filter value that the
	// preview does not evaluate.
	errUnsupportedTerm = errors.New("the preview does not evaluate the term")

	// errInvalidValue reports a filter value that the web client rejects.
	errInvalidValue = errors.New("the web client rejects the value")

	// errNotRange reports a value that the range format of DIM rejects.
	errNotRange = errors.New("not a range")

	// errNoCharacter reports an account without a character. The web
	// client then has no current character, and its search matches every
	// item.
	//
	//nolint:staticcheck // ST1005: the tool result is this text, which ends a sentence.
	errNoCharacter = errors.New(
		"the account has no character, so the server previews nothing. " +
			"The web client matches every item when it has no current character.",
	)

	// The buckets of the categories of DIM that the search filters read.
	weaponBuckets     = []uint32{bucketKinetic, bucketEnergy, bucketPower}
	armorBuckets      = []uint32{bucketHelmet, bucketGauntlets, bucketChestArmor, bucketLegArmor, bucketClassArmor}
	postmasterBuckets = []uint32{bucketEngrams, bucketMessages, bucketSpecialOrders}

	// dupeTypes lists the values of the dupe: filter that DIM knows, which
	// are the keys of its table dupeTypeLookupRaw.
	dupeTypes = []string{
		"item", "stats", "untunedstats", "archetype", "tertiarystat", "nonzerostats",
		"tunedstat", "setbonus", "perks", "traits", "statlower", "customstatlower",
	}

	// rangeOverload and rangeNumber are the patterns of a range value in
	// DIM. A value is an operator of up to two characters before a word of
	// an overload table or before a number. The \w and \d of Go match the
	// same ASCII characters as those of JavaScript without the u flag.
	rangeOverload = regexp.MustCompile(`^([<=>]{0,2})(\w+)$`)
	rangeNumber   = regexp.MustCompile(`^([<=>]{0,2})(\d+(?:\.\d+)?)$`)

	// adeptPatterns are the patterns that stripAdept of DIM removes from a
	// weapon name, in its order. DIM builds each one with the flags gi from
	// a translation of Filter.Adept, Filter.Timelost, or Filter.Harrowed.
	// The web client translates each key to the key itself, so the patterns
	// match the keys and never the suffixes such as (Adept). Each letter is
	// a class of its two ASCII cases. The (?i) flag of Go also matches
	// U+017F for s, but the i flag of JavaScript without the u flag does
	// not.
	adeptPatterns = []*regexp.Regexp{
		regexp.MustCompile(`[Ff][Ii][Ll][Tt][Ee][Rr]` + jsDot + `[Aa][Dd][Ee][Pp][Tt]`),
		regexp.MustCompile(`[Ff][Ii][Ll][Tt][Ee][Rr]` + jsDot + `[Tt][Ii][Mm][Ee][Ll][Oo][Ss][Tt]`),
		regexp.MustCompile(`[Ff][Ii][Ll][Tt][Ee][Rr]` + jsDot + `[Hh][Aa][Rr][Rr][Oo][Ww][Ee][Dd]`),
	}

	// searchFilters lists the filters that the preview evaluates, in the
	// order that the text of [ErrUnsupportedQuery] names them.
	searchFilters = []searchFilter{
		{name: filterIs, word: "weapon", match: inBuckets(weaponBuckets)},
		{name: filterIs, word: "armor", match: inBuckets(armorBuckets)},
		{name: filterIs, word: "equipment", match: isEquipment},
		{name: filterIs, word: "equippable", match: isEquipment},
		{name: filterIs, word: "equipped", match: func(item *Item) bool { return item.Equipped }},
		{name: filterIs, word: "invault", match: func(item *Item) bool { return item.Owner == ownerVault }},
		{name: filterIs, word: "postmaster", match: inPostmaster},
		{name: filterIs, word: "inpostmaster", match: inPostmaster},
		{name: filterIs, word: "locked", match: func(item *Item) bool { return item.Lockable && item.Locked }},
		{name: filterIs, word: "unlocked", match: func(item *Item) bool { return item.Lockable && !item.Locked }},
		{name: filterIs, word: "titan", match: ofClass(classTitan)},
		{name: filterIs, word: "hunter", match: ofClass(classHunter)},
		{name: filterIs, word: "warlock", match: ofClass(classWarlock)},
		{name: filterIs, word: "favorite", match: func(item *Item) bool { return item.Favorite }},
		{name: filterIs, word: "autorifle", match: inCategory(categoryAutoRifle)},
		{name: filterIs, word: "handcannon", match: inCategory(categoryHandCannon)},
		{name: filterIs, word: "pulserifle", match: inCategory(categoryPulseRifle)},
		{name: filterIs, word: "scoutrifle", match: inCategory(categoryScoutRifle)},
		{name: filterIs, word: "fusionrifle", match: inCategory(categoryFusionRifle)},
		{name: filterIs, word: "sniperrifle", match: inCategory(categorySniperRifle)},
		{name: filterIs, word: "shotgun", match: inCategory(categoryShotgun)},
		{name: filterIs, word: "machinegun", match: inCategory(categoryMachineGun)},
		{name: filterIs, word: "lmg", match: inCategory(categoryMachineGun)},
		{name: filterIs, word: "rocketlauncher", match: inCategory(categoryRocketLauncher)},
		{name: filterIs, word: "sidearm", match: inCategory(categorySidearm)},
		{name: filterIs, word: "sword", match: inCategory(categorySword)},
		{
			name:  filterIs,
			word:  "grenadelauncher",
			match: inCategory(categoryGrenadeLaunchers, -categoryGrenadeLaunchers),
		},
		{name: filterIs, word: "heavygrenadelauncher", match: inCategory(categoryGrenadeLaunchers)},
		{name: filterIs, word: "specialgrenadelauncher", match: inCategory(-categoryGrenadeLaunchers)},
		{name: filterIs, word: "tracerifle", match: inCategory(categoryTraceRifles)},
		{name: filterIs, word: "linearfusionrifle", match: inCategory(categoryLinearFusionRifles)},
		{name: filterIs, word: "lfr", match: inCategory(categoryLinearFusionRifles)},
		{name: filterIs, word: "submachine", match: inCategory(categorySubmachineGuns)},
		{name: filterIs, word: "smg", match: inCategory(categorySubmachineGuns)},
		{name: filterIs, word: "bow", match: inCategory(categoryBows)},
		{name: filterIs, word: "glaive", match: inCategory(categoryGlaives)},
		{name: filterIs, word: "transmat", match: inCategory(categoryShipModsTransmatEffects)},
		{name: filterIs, word: "weaponmod", match: inCategory(categoryWeaponMods)},
		{name: filterIs, word: "armormod", match: inCategory(categoryArmorMods)},
		{name: filterIs, word: "reptoken", match: inCategory(categoryReputationTokens)},
		{name: filterIs, word: "primary", match: ofAmmo(ammoPrimary)},
		{name: filterIs, word: "special", match: ofAmmo(ammoSpecial)},
		{name: filterIs, word: "heavy", match: ofAmmo(ammoHeavy)},
		{name: filterIs, word: "dupe", build: isDupe},
		{name: filterIs, word: "incurrentchar", build: inCurrentChar},
		{name: filterIs, word: "currentclass", build: currentClass},
		{name: filterTag, value: itemValue(tagMatch)},
		{name: filterDupe, word: "item", value: dupeValue},
		{name: filterCount, value: countValue},
		{name: filterID, value: itemValue(idMatch)},
		{name: filterHash, value: itemValue(hashMatch)},
		{name: filterName, value: itemValue(nameMatch)},
		{name: filterExactName, value: itemValue(exactNameMatch)},
	}

	// isFilters maps each is: word of searchFilters to the builder of its
	// predicate.
	isFilters = isTable(searchFilters)

	// valueFilters maps the name of each filter of searchFilters that
	// takes a value to the function that reads the value.
	valueFilters = valueTable(searchFilters)

	// wordStart matches a text that starts with a character of \w. The \w
	// of Go and the \w of a JavaScript regular expression without the u
	// flag both match an ASCII letter, an ASCII digit, or an underscore.
	wordStart = regexp.MustCompile(`^\w`)
)

// queryTest builds the predicate of a query, or of one term of a query,
// for a search context. A filter that reads other items, such as is:dupe,
// groups the items of the context when it builds the predicate, as DIM
// does when it builds the filter.
type queryTest func(ctx searchContext) func(*Item) bool

// searchContext holds what the filters read besides the tested item, as
// the filter context of DIM does.
type searchContext struct {
	// items holds every item that DIM builds, in every location, the vault
	// included. The web client passes these items to DIM as allItems.
	items []*Item
	// current is the current character, the one that the player played
	// last.
	current Character
}

// searchFilter is one filter that the preview evaluates. An is: word sets
// match or build, and a filter that takes a value sets value.
type searchFilter struct {
	// match is the predicate of an is: word that reads only the tested
	// item.
	match func(*Item) bool
	// build builds the predicate of an is: word that reads the search
	// context.
	build queryTest
	// value reads the value of a filter that takes one, such as tag:keep,
	// and returns the builder of its predicate. It returns an error that
	// wraps errInvalidValue for a value that the web client rejects. It
	// returns errUnsupportedTerm for a value that the preview does not
	// evaluate.
	value func(args string) (queryTest, error)
	// name is the filter name, such as "is" or "tag".
	name string
	// word is the is: word, such as "weapon". A filter that takes a value
	// sets word only when the preview evaluates one value alone, such as
	// item for dupe:item.
	word string
}

// keyword returns the text that names the filter, such as "is:weapon" or
// "tag:".
func (f searchFilter) keyword() string {
	return f.name + ":" + f.word
}

// compiledQuery is a search query in the form that the preview evaluates.
// It has one of three outcomes. test is set, unsupported holds a term, or
// invalid holds a term.
type compiledQuery struct {
	// test builds the predicate of the query for a search context. It is
	// nil when unsupported or invalid holds a term.
	test queryTest
	// unsupported holds the Text of each term that the preview does not
	// evaluate, one time each, in the order of the query. It is empty when
	// invalid holds a term.
	unsupported []string
	// invalid holds the Text of each term whose value the web client
	// rejects, one time each, in the order of the query.
	invalid []string
}

// queryCompiler collects the terms that the preview does not evaluate and
// the terms whose value the web client rejects while [compileQuery] walks
// a parse tree.
type queryCompiler struct {
	unsupported []string
	invalid     []string
}

// compileQuery builds the predicate of the parse tree that [parseQuery]
// returns. It evaluates only the filters of searchFilters. The web client
// voids a whole query when one term is invalid. A predicate of the other
// terms can therefore match items that the web client does not match. A
// query with a term whose value the web client rejects has no predicate,
// and the result names each such term. The web client skips that query
// whatever its other terms mean, so the result names no unsupported term.
// Any other query with a term outside searchFilters has no predicate
// either, and the result names each such term.
func compileQuery(root *QueryNode) compiledQuery {
	var c queryCompiler

	test := c.node(root)

	switch {
	case len(c.invalid) > 0:
		return compiledQuery{invalid: c.invalid}
	case len(c.unsupported) > 0:
		return compiledQuery{unsupported: c.unsupported}
	default:
		return compiledQuery{test: test}
	}
}

// node returns the predicate builder of n, or nil for a filter that the
// preview does not evaluate or whose value the web client rejects.
func (c *queryCompiler) node(n *QueryNode) queryTest {
	switch n.Op {
	case OpAnd, OpOr:
		builds := make([]queryTest, 0, len(n.Operands))
		for _, operand := range n.Operands {
			builds = append(builds, c.node(operand))
		}

		// An and node matches when no operand fails, and an or node
		// matches when one operand passes.
		want := n.Op == OpOr

		return func(ctx searchContext) func(*Item) bool {
			tests := make([]func(*Item) bool, 0, len(builds))
			for _, build := range builds {
				tests = append(tests, build(ctx))
			}

			return func(item *Item) bool {
				for _, test := range tests {
					if test(item) == want {
						return want
					}
				}

				return !want
			}
		}

	case OpNot:
		build := c.node(n.Operand)

		return func(ctx searchContext) func(*Item) bool {
			test := build(ctx)

			return func(item *Item) bool { return !test(item) }
		}

	default:
		build, err := filterTest(n)

		switch {
		case errors.Is(err, errInvalidValue):
			c.invalid = appendNew(c.invalid, n.Text)
		case err != nil:
			c.unsupported = appendNew(c.unsupported, n.Text)
		}

		return build
	}
}

// appendNew appends term to terms when terms does not hold it yet.
func appendNew(terms []string, term string) []string {
	if slices.Contains(terms, term) {
		return terms
	}

	return append(terms, term)
}

// filterTest returns the predicate builder of the filter node n. It
// returns errUnsupportedTerm when the preview does not evaluate the
// filter or its value. It returns an error that wraps errInvalidValue
// when the web client rejects the value. Only the table decides which is:
// words the preview evaluates, so is:constructor and is:__proto__ stay
// unsupported. DIM finds both words on the prototype of its filter table
// and matches nothing.
func filterTest(n *QueryNode) (queryTest, error) {
	if n.Type == filterIs {
		build, ok := isFilters[n.Args]
		if !ok {
			return nil, errUnsupportedTerm
		}

		return build, nil
	}

	value, ok := valueFilters[n.Type]
	if !ok {
		return nil, errUnsupportedTerm
	}

	return value(n.Args)
}

// matches returns the items of items that q matches in ctx, ordered by
// name and then by instance id, as the default sort of list_items orders
// them.
func (q compiledQuery) matches(ctx searchContext, items []*Item) []*Item {
	var out []*Item

	test := q.test(ctx)
	for _, item := range items {
		if test(item) {
			out = append(out, item)
		}
	}

	slices.SortStableFunc(out, func(a, b *Item) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), compareIDs(a.InstanceID, b.InstanceID))
	})

	return out
}

// invalidQuery returns the [ErrInvalidInput] error that names terms, the
// terms whose value the web client rejects.
func invalidQuery(terms []string) error {
	return fmt.Errorf(
		"%w: the web client rejects %s, so it skips an autotag rule with this query",
		ErrInvalidInput, strings.Join(terms, ", "),
	)
}

// unsupportedQuery returns the [ErrUnsupportedQuery] error that names
// terms and every filter that the preview evaluates.
func unsupportedQuery(terms []string) error {
	//nolint:staticcheck // ST1005: the tool result is this text, which ends a sentence.
	return fmt.Errorf(
		"%w: the server does not evaluate %s, so it previews nothing. "+
			"The web client might still accept these terms. The server evaluates %s. "+
			"Each is: word also works as not:<word>.",
		ErrUnsupportedQuery, strings.Join(terms, ", "), listWithAnd(supportedKeywords()),
	)
}

// supportedKeywords returns the keyword of each filter that the preview
// evaluates, in the order of searchFilters.
func supportedKeywords() []string {
	keywords := make([]string, 0, len(searchFilters))
	for _, f := range searchFilters {
		keywords = append(keywords, f.keyword())
	}

	return keywords
}

// listWithAnd joins words with commas and puts "and" before the last word.
func listWithAnd(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " and " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", and " + words[len(words)-1]
	}
}

// builtItems returns the items of s that DIM builds. DIM builds no item
// whose current bucket holds dummy items. The current bucket of a lost
// item is the Lost Items bucket of the Postmaster, and the current bucket
// of every other item is Item.Bucket. DIM also builds no item without a
// definition, but the snapshot keeps such an item under the name "Unknown
// item N", and so does builtItems.
func builtItems(s *Snapshot) []*Item {
	var built []*Item

	for _, item := range s.Items {
		current := item.Bucket
		if item.Lost {
			current = bucketPostmaster
		}

		if current != bucketForbidden && current != bucketDummy {
			built = append(built, item)
		}
	}

	return built
}

// ownedItems returns the items that the rule editor of the web client
// previews a rule against. The editor keeps each instanced item of
// [builtItems] that DIM marks as equipment, in every location. DIM marks
// an item whose definition has an equipping block, except an emote or a
// finisher.
func ownedItems(s *Snapshot) []*Item {
	var owned []*Item

	for _, item := range builtItems(s) {
		if item.InstanceID != "" && isEquipment(item) {
			owned = append(owned, item)
		}
	}

	return owned
}

// isEquipment reports whether DIM marks item as equipment. The web client
// marks no item as classified, so the equipping block of the definition
// decides alone.
func isEquipment(item *Item) bool {
	return item.Equippable && item.Bucket != bucketEmotes && item.Bucket != bucketFinishers
}

// isComparable reports whether DIM marks item as comparable, which the
// dupe filters read. DIM marks equipment that the game can lock, except an
// emblem.
func isComparable(item *Item) bool {
	return isEquipment(item) && item.Lockable && item.Bucket != bucketEmblems
}

// inBuckets returns the predicate of an item whose bucket is one of
// buckets. DIM reads the bucket of the definition. Item.Bucket holds it
// for an item in the vault or in the Lost Items bucket, and holds the
// profile bucket for every other item.
func inBuckets(buckets []uint32) func(*Item) bool {
	return func(item *Item) bool { return slices.Contains(buckets, item.Bucket) }
}

// inPostmaster reports whether DIM puts the current bucket of item in its
// Postmaster category. The current bucket of a lost item is the Lost Items
// bucket. Item.Postmaster also marks an item at the Postmaster in a bucket
// outside that category, which DIM does not count.
func inPostmaster(item *Item) bool {
	return item.Lost || slices.Contains(postmasterBuckets, item.Bucket)
}

// ofClass returns the predicate of an item for the Bungie DestinyClass
// value classType.
func ofClass(classType int) func(*Item) bool {
	return func(item *Item) bool { return item.ClassType == classType }
}

// inCategory returns the predicate of an item whose item categories hold
// one of categories.
func inCategory(categories ...int64) func(*Item) bool {
	return func(item *Item) bool {
		return slices.ContainsFunc(item.Categories, func(c int64) bool { return slices.Contains(categories, c) })
	}
}

// ofAmmo returns the predicate of an item for the Bungie
// DestinyAmmunitionType value ammoType.
func ofAmmo(ammoType int) func(*Item) bool {
	return func(item *Item) bool { return item.AmmoType == ammoType }
}

// tagMatch returns the predicate of the filter tag:value, as the web
// client defines it. The value favorite reads the favorite mark. Every
// other value matches an item whose tag is that value, and the web client
// knows only the tags of tagOrder. An untagged item matches no value.
func tagMatch(value string) func(*Item) bool {
	if value == tagFavorite {
		return func(item *Item) bool { return item.Favorite }
	}

	return func(item *Item) bool { return item.Tag == value && slices.Contains(tagOrder, item.Tag) }
}

// nameMatch returns the predicate of the filter name:value, as DIM defines
// it. The filter matches an item when a word of its name starts with the
// value, and it compares both texts after [plainString] folds them. DIM
// puts a word boundary before the value only when the folded value starts
// with an ASCII letter, an ASCII digit, or an underscore. A value such as
// -x can therefore match in the middle of a word.
func nameMatch(value string) func(*Item) bool {
	folded := plainString(value)

	pattern := "(?i)"
	if wordStart.MatchString(folded) {
		pattern += `\b`
	}

	re, err := regexp.Compile(pattern + regexp.QuoteMeta(folded))
	if err != nil {
		// Compile refuses only a value of millions of characters. Each
		// character of the pattern matches one character of a name, so
		// such a value matches no name.
		return func(*Item) bool { return false }
	}

	return func(item *Item) bool { return re.MatchString(plainName(item)) }
}

// exactNameMatch returns the predicate of the filter exactname:value, as
// DIM defines it. The filter matches an item whose whole name equals the
// value after [plainString] folds both texts.
func exactNameMatch(value string) func(*Item) bool {
	folded := plainString(value)

	return func(item *Item) bool { return plainName(item) == folded }
}

// plainName returns the name of item in the form that the name filters
// compare. The lexer turned the typographic quotes of the value into ASCII
// quotes. plainName therefore applies [normalizeQuotes] to the name, as
// DIM does, and then [plainString] folds it.
func plainName(item *Item) string {
	return plainString(normalizeQuotes(item.Name))
}

// plainString folds s for a text filter, as plainString of DIM folds the
// text of a Latin-based language. The web client always searches in
// English, which DIM marks as Latin-based. The fold decomposes s to
// Unicode Normalization Form D, removes each character with the Unicode
// property Diacritic, and lowercases the rest. The tables of Go and of
// the browser each follow their own Unicode version. A character that
// only one of the two versions lists can therefore fold differently.
func plainString(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Diacritic, r) {
			return -1
		}

		return r
	}, norm.NFD.String(s))

	return strings.ToLower(s)
}

// idMatch returns the predicate of the filter id:value, which matches the
// item whose instance id is the value.
func idMatch(value string) func(*Item) bool {
	return func(item *Item) bool { return item.InstanceID == value }
}

// hashMatch returns the predicate of the filter hash:value. DIM reads the
// value with parseInt and the radix 10. parseInt skips leading whitespace,
// reads an optional sign, and then reads the leading decimal digits, so
// hash:12abc is hash:12. A value without leading digits matches nothing.
// So do a negative value and a value that does not fit in a uint32,
// because no item hash equals them. JavaScript compares -0 as equal to 0,
// so hash:-0 is hash:0.
func hashMatch(value string) func(*Item) bool {
	value = strings.TrimLeftFunc(value, isJSSpace)

	negative := strings.HasPrefix(value, "-")
	if negative || strings.HasPrefix(value, "+") {
		value = value[1:]
	}

	end := strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(value)
	}

	hash, err := strconv.ParseUint(value[:end], 10, 32)
	if err != nil || (negative && hash != 0) {
		return func(*Item) bool { return false }
	}

	return func(item *Item) bool { return uint64(item.Hash) == hash }
}

// isDupe builds the predicate of is:dupe and dupe:item, as DIM builds it
// for the value item. It groups every item of the context by
// [itemDupeID]. It matches a comparable item whose group holds more than
// one item, except the default shader and an item of the Artifacts
// bucket.
func isDupe(ctx searchContext) func(*Item) bool {
	counts := dupeCounts(ctx.items, func(*Item) bool { return true })

	return func(item *Item) bool {
		return item.Hash != hashDefaultShader &&
			item.Bucket != bucketArtifacts &&
			isComparable(item) &&
			counts[itemDupeID(item)] > 1
	}
}

// dupeValue reads the value of the filter dupe:. The preview evaluates the
// value item. DIM accepts each key of its table dupeTypeLookupRaw and each
// value whose parts, split at "+", are all keys. The preview does not
// evaluate those other values. DIM rejects every other value.
//
// A prefix of a key, such as ite in dupe:ite, is invalid too. The search
// bar of the web client completes such a value to a key before it runs
// the query. The autotagger and the rule editor of the web client first
// check the query with the validator of DIM. They skip a query that fails
// the check, so the completion never reaches an autotag rule.
func dupeValue(value string) (queryTest, error) {
	if value == "item" {
		return isDupe, nil
	}

	for part := range strings.SplitSeq(value, "+") {
		if !slices.Contains(dupeTypes, part) {
			return nil, fmt.Errorf("%w: dupe:%s", errInvalidValue, value)
		}
	}

	return nil, errUnsupportedTerm
}

// countValue reads the range value of the filter count:. The predicate
// compares the number of comparable items of the context that share the
// [itemDupeID] of the tested item, and 0 when no such item exists. The
// tested item counts only when it is comparable itself. DIM groups only
// comparable items for count:, so its groups differ from those of
// is:dupe.
func countValue(value string) (queryTest, error) {
	compare, err := rangeFormat{}.comparator(value)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidValue, err)
	}

	return func(ctx searchContext) func(*Item) bool {
		counts := dupeCounts(ctx.items, isComparable)

		return func(item *Item) bool { return compare(float64(counts[itemDupeID(item)])) }
	}, nil
}

// dupeCounts returns the number of items of items that keep accepts for
// each [itemDupeID].
func dupeCounts(items []*Item, keep func(*Item) bool) map[string]int {
	counts := map[string]int{}

	for _, item := range items {
		if keep(item) {
			counts[itemDupeID(item)]++
		}
	}

	return counts
}

// itemDupeID returns the key that groups the copies of item, as itemDupeID
// of DIM builds it. The key joins the name, the decimal class type, the
// tier name, and the decimal bucket hash without separators. A weapon
// name goes through [stripAdept] first. The web client classifies no
// item, so the key never falls back to the item hash.
func itemDupeID(item *Item) string {
	name := item.Name
	if slices.Contains(weaponBuckets, item.Bucket) {
		name = stripAdept(name)
	}

	return name + strconv.Itoa(item.ClassType) + item.Tier + strconv.FormatUint(uint64(item.Bucket), 10)
}

// stripAdept removes every match of each of adeptPatterns from name, in
// order. It then trims the name, as stripAdept of DIM does in the web
// client. The patterns never match a suffix such as (Adept), so an adept
// weapon keeps its own key.
func stripAdept(name string) string {
	for _, pattern := range adeptPatterns {
		name = pattern.ReplaceAllLiteralString(name, "")
	}

	return jsTrim(name)
}

// inCurrentChar builds the predicate of is:incurrentchar. It matches an
// item of the current character. It also matches every item whose owner
// is "account". DIM files such an item under the current character when
// the vault accepts its bucket or the bucket is Special Orders. DIM files
// an item of any other bucket under the vault, which the predicate
// ignores.
func inCurrentChar(ctx searchContext) func(*Item) bool {
	return func(item *Item) bool { return item.Owner == ctx.current.ID || item.Owner == ownerAccount }
}

// currentClass builds the predicate of is:currentclass. It matches an item
// of the class of the current character.
func currentClass(ctx searchContext) func(*Item) bool {
	return ofClass(ctx.current.ClassType)
}

// rangeFormat is the range format of DIM, which reads a value such as >=2
// as a comparison with a number. overloads maps each word that stands for
// a number in the value, such as a season name. A nil map marks a filter
// without an overload table in DIM.
type rangeFormat struct {
	overloads map[string]float64
}

// comparator returns the predicate of a number for the range value, as
// rangeStringToComparator of DIM builds it. The value is an operator and
// then a word of overloads or a number. The operator is empty or one of
// =, <, <=, >, and >=. The empty operator and = both test equality. The
// comparator returns an error that wraps errNotRange for every other
// value, the empty value included.
func (f rangeFormat) comparator(value string) (func(float64) bool, error) {
	op, operand, ok := f.operand(value)
	if !ok {
		return nil, fmt.Errorf("%w: %q", errNotRange, value)
	}

	switch op {
	case "", "=":
		return func(n float64) bool { return n == operand }, nil
	case "<":
		return func(n float64) bool { return n < operand }, nil
	case "<=":
		return func(n float64) bool { return n <= operand }, nil
	case ">":
		return func(n float64) bool { return n > operand }, nil
	case ">=":
		return func(n float64) bool { return n >= operand }, nil
	default:
		return nil, fmt.Errorf("%w: unknown operator %q", errNotRange, op)
	}
}

// operand splits a range value into its operator and its number, as
// extractOpAndValue of DIM does. It tries a word of overloads first and a
// number second, and it reports false when neither matches.
//
// DIM tests the word with the JavaScript in operator, which also finds the
// keys of Object.prototype. The query is lowercase, so only the keys
// constructor and __proto__ can reach the test. Each one stands for a
// function or an object, which no number equals or compares with, so NaN
// takes its place. A nil overloads marks a filter without a table, and
// the words then stay invalid.
func (f rangeFormat) operand(value string) (string, float64, bool) {
	m := rangeOverload.FindStringSubmatch(value)
	if m != nil && f.overloads != nil {
		n, ok := f.overloads[m[2]]
		if ok {
			return m[1], n, true
		}

		if m[2] == "constructor" || m[2] == "__proto__" {
			return m[1], math.NaN(), true
		}
	}

	m = rangeNumber.FindStringSubmatch(value)
	if m == nil {
		return "", 0, false
	}

	// The pattern admits only a well-formed number, so ParseFloat fails
	// only on a number too large for a float64. It then returns +Inf, as
	// parseFloat does.
	n, err := strconv.ParseFloat(m[2], 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return "", 0, false
	}

	return m[1], n, true
}

// itemValue adapts the predicate function of a filter that reads only the
// tested item and accepts every value to the value field of a
// [searchFilter].
func itemValue(match func(value string) func(*Item) bool) func(string) (queryTest, error) {
	return func(value string) (queryTest, error) {
		test := match(value)

		return func(searchContext) func(*Item) bool { return test }, nil
	}
}

// isTable maps each is: word of filters to the builder of its predicate.
func isTable(filters []searchFilter) map[string]queryTest {
	table := map[string]queryTest{}

	for _, f := range filters {
		switch {
		case f.build != nil:
			table[f.word] = f.build
		case f.match != nil:
			match := f.match
			table[f.word] = func(searchContext) func(*Item) bool { return match }
		}
	}

	return table
}

// valueTable maps the name of each filter of filters that takes a value
// to the function that reads the value.
func valueTable(filters []searchFilter) map[string]func(string) (queryTest, error) {
	table := map[string]func(string) (queryTest, error){}

	for _, f := range filters {
		if f.value != nil {
			table[f.name] = f.value
		}
	}

	return table
}
