package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
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

	// bucketForbidden and bucketDummy hold the dummy items that DIM
	// leaves out of every store. DIM names them THE_FORBIDDEN_BUCKET
	// and SOME_OTHER_DUMMY_BUCKET.
	bucketForbidden uint32 = 2422292810
	bucketDummy     uint32 = 3621873013
)

// Filter names of the search syntax that the preview evaluates.
const (
	filterIs  = "is"
	filterTag = "tag"
)

var (
	// ErrUnsupportedQuery reports a search query with a term that the
	// preview does not evaluate.
	ErrUnsupportedQuery = errors.New("unsupported query")

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
		{name: filterTag, value: tagMatch},
	}

	// isFilters maps each is: word of searchFilters to its predicate.
	isFilters = isTable(searchFilters)

	// valueFilters maps the name of each filter of searchFilters that
	// takes a value to the function that builds its predicate.
	valueFilters = valueTable(searchFilters)
)

// searchFilter is one filter that the preview evaluates. An is: word sets
// match, and a filter that takes a value sets value.
type searchFilter struct {
	// match is the predicate of an is: word.
	match func(*Item) bool
	// value returns the predicate of a filter that takes a value, such as
	// tag:keep, for the value.
	value func(args string) func(*Item) bool
	// name is the filter name, such as "is" or "tag".
	name string
	// word is the is: word, such as "weapon". A filter that takes a value
	// has no word.
	word string
}

// keyword returns the text that names the filter, such as "is:weapon" or
// "tag:".
func (f searchFilter) keyword() string {
	return f.name + ":" + f.word
}

// compiledQuery is a search query in the form that the preview evaluates.
type compiledQuery struct {
	// test reports whether the query matches an item. It is nil when
	// unsupported holds a term.
	test func(*Item) bool
	// unsupported holds the Text of each term that the preview does not
	// evaluate, one time each, in the order of the query.
	unsupported []string
}

// queryCompiler collects the terms that the preview does not evaluate
// while [compileQuery] walks a parse tree.
type queryCompiler struct {
	unsupported []string
}

// compileQuery builds the predicate of the parse tree that [parseQuery]
// returns. It evaluates only the filters of searchFilters. A query with
// any other term has no predicate, and the result names each such term.
// The web client voids a whole query when one term is invalid. A
// predicate of the other terms can therefore match items that the web
// client does not match.
func compileQuery(root *QueryNode) compiledQuery {
	var c queryCompiler

	test := c.node(root)
	if len(c.unsupported) > 0 {
		return compiledQuery{unsupported: c.unsupported}
	}

	return compiledQuery{test: test}
}

// node returns the predicate of n, or nil for a filter that the preview
// does not evaluate.
func (c *queryCompiler) node(n *QueryNode) func(*Item) bool {
	switch n.Op {
	case OpAnd, OpOr:
		tests := make([]func(*Item) bool, 0, len(n.Operands))
		for _, operand := range n.Operands {
			tests = append(tests, c.node(operand))
		}

		// An and node matches when no operand fails, and an or node
		// matches when one operand passes.
		want := n.Op == OpOr

		return func(item *Item) bool {
			for _, test := range tests {
				if test(item) == want {
					return want
				}
			}

			return !want
		}

	case OpNot:
		test := c.node(n.Operand)

		return func(item *Item) bool { return !test(item) }

	default:
		test := filterTest(n)
		if test == nil && !slices.Contains(c.unsupported, n.Text) {
			c.unsupported = append(c.unsupported, n.Text)
		}

		return test
	}
}

// filterTest returns the predicate of the filter node n, or nil when the
// preview does not evaluate the filter. Only the table decides, so the
// is: words is:constructor and is:__proto__ stay unsupported. DIM finds
// both words on the prototype of its filter table and matches nothing.
func filterTest(n *QueryNode) func(*Item) bool {
	if n.Type == filterIs {
		return isFilters[n.Args]
	}

	value, ok := valueFilters[n.Type]
	if !ok {
		return nil
	}

	return value(n.Args)
}

// matches returns the items that q matches, ordered by name and then by
// instance id, as the default sort of list_items orders them.
func (q compiledQuery) matches(items []*Item) []*Item {
	var out []*Item

	for _, item := range items {
		if q.test(item) {
			out = append(out, item)
		}
	}

	slices.SortStableFunc(out, func(a, b *Item) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), compareIDs(a.InstanceID, b.InstanceID))
	})

	return out
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

// ownedItems returns the items that the rule editor of the web client
// previews a rule against. The editor keeps each instanced item that DIM
// marks as equipment, in every location. DIM marks an item whose
// definition has an equipping block, except an emote or a finisher. DIM
// builds no item whose current bucket holds dummy items. The current
// bucket of a lost item is the Lost Items bucket of the Postmaster.
func ownedItems(s *Snapshot) []*Item {
	var owned []*Item

	for _, item := range s.Items {
		current := item.Bucket
		if item.Lost {
			current = bucketPostmaster
		}

		if item.InstanceID != "" && isEquipment(item) && current != bucketForbidden && current != bucketDummy {
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

// isTable maps each is: word of filters to its predicate.
func isTable(filters []searchFilter) map[string]func(*Item) bool {
	table := map[string]func(*Item) bool{}

	for _, f := range filters {
		if f.match != nil {
			table[f.word] = f.match
		}
	}

	return table
}

// valueTable maps the name of each filter of filters that takes a value
// to the function that builds its predicate.
func valueTable(filters []searchFilter) map[string]func(string) func(*Item) bool {
	table := map[string]func(string) func(*Item) bool{}

	for _, f := range filters {
		if f.value != nil {
			table[f.name] = f.value
		}
	}

	return table
}
