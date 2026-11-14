package search

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
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
	filterPower     = "power"
	filterLight     = "light"

	// The rating filters read the Aegis sheet. Each of them also has a
	// form whose name starts with pvpPrefix, such as pvprate:, which reads
	// the PvP sheet.
	filterRate       = "rate"
	filterRating     = "rating"
	filterRateBase   = "ratebase"
	filterRatingBase = "ratingbase"
	filterMisses     = "misses"
	filterSetBonus   = "setbonus"
	filterVerdict    = "verdict"

	// pvpPrefix starts the name of each rating filter that reads the PvP
	// sheet.
	pvpPrefix = "pvp"
)

// The reasons that the server cannot decide a rating filter for an item.
const (
	// ReasonNoPerks applies to a copy without perks on a sheet row with
	// perk columns.
	ReasonNoPerks = "the profile lists no perks for this copy"
	// ReasonVerdictNoPerks applies at a verdict filter to a copy without
	// perks on a sheet row with perk columns. It also applies to each
	// rival of such a copy.
	ReasonVerdictNoPerks = "the profile lists no perks for this copy or for another copy on its sheet row"
)

// The kinds of verdict that [verdictOf] gives a copy against its rivals,
// as the web client names them. The hits of a copy are the perk columns
// that it hits.
const (
	// verdictOnly is the verdict of a copy without rivals.
	verdictOnly = "only"
	// verdictBetter is the verdict of a copy whose hits are a strict
	// superset of the hits of each rival.
	verdictBetter = "better"
	// verdictEqual is the verdict of a copy with the hits of a rival.
	verdictEqual = "equal"
	// verdictWorse is the verdict of a copy whose hits are a strict
	// subset of the hits of a rival.
	verdictWorse = "worse"
	// verdictMixed is the verdict of every other copy with rivals.
	verdictMixed = "mixed"
)

// OutcomeState tells whether a test matches one item, does not match it,
// or cannot decide it.
type OutcomeState uint8

// The states of an [Outcome].
const (
	// NoMatch marks an item that the test does not match.
	NoMatch OutcomeState = iota
	// Match marks an item that the test matches.
	Match
	// Undecided marks an item whose outcome the server cannot decide.
	Undecided
)

// jsDot is a character class that matches what a dot matches in a
// JavaScript regular expression without the u flag. Such a dot matches one
// UTF-16 code unit other than the line terminators \n, \r, U+2028, and
// U+2029. A dot that an ASCII character follows therefore never matches a
// character outside the Basic Multilingual Plane, whose two code units it
// splits. The dot of Go matches every character except \n.
const jsDot = `[^\n\r\x{2028}\x{2029}\x{10000}-\x{10ffff}]`

var (
	// errUnsupportedTerm reports a filter or a filter value that the
	// preview does not evaluate.
	errUnsupportedTerm = errors.New("the preview does not evaluate the term")

	// errInvalidValue reports a filter value that the web client rejects.
	errInvalidValue = errors.New("the web client rejects the value")

	// errNotRange reports a value that the range format of DIM rejects.
	errNotRange = errors.New("not a range")

	// errOverloadValue reports a word of a range value that DIM reads as
	// a number that the preview lacks, such as pinnaclecap.
	errOverloadValue = errors.New("the preview lacks the number of the word")

	// The buckets of the categories of DIM that the search filters read.
	weaponBuckets = []uint32{bungie.BucketKinetic, bungie.BucketEnergy, bungie.BucketPower}
	armorBuckets  = []uint32{
		bungie.BucketHelmet,
		bungie.BucketGauntlets,
		bungie.BucketChestArmor,
		bungie.BucketLegArmor,
		bungie.BucketClassArmor,
	}

	// PostmasterBuckets lists the buckets of the Postmaster category of DIM
	// other than the Lost Items bucket, which is [bungie.BucketPostmaster].
	PostmasterBuckets = []uint32{bungie.BucketEngrams, bungie.BucketMessages, bungie.BucketSpecialOrders}

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

	// rangeOperators lists the operators of a range value that DIM
	// accepts.
	rangeOperators = []string{"", "=", "<", "<=", ">", ">="}

	// setBonusPieces maps each size prefix of a value of setbonus:, such
	// as 4pc in setbonus:4pc:s, to the number of pieces that the set bonus
	// needs.
	setBonusPieces = map[string]int{"2pc": 2, "4pc": 4}

	// setBonusQueries lists the values of setbonus: and pvpsetbonus: that
	// the web client accepts. DIM accepts only the suggestions of a filter
	// of the format query.
	setBonusQueries = setBonusSuggestions()

	// powerRange is the range format of power: and light:. DIM reads
	// each word of its table powerLevelByKeyword as a power level of the
	// season, and the preview lacks the season table. The empty overloads
	// still accept constructor and __proto__, which the table of DIM
	// accepts because it is an object.
	powerRange = rangeFormat{
		overloads:   map[string]float64{},
		unsupported: []string{"powerfloor", "softcap", "powerfulcap", "pinnaclecap"},
	}

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
	// order that the refusal of a query with an unsupported term names
	// them.
	searchFilters = []searchFilter{
		{name: filterIs, word: "weapon", match: inBuckets(weaponBuckets...)},
		{name: filterIs, word: "armor", match: inBuckets(armorBuckets...)},
		{name: filterIs, word: "equipment", match: isEquipment},
		{name: filterIs, word: "equippable", match: isEquipment},
		{name: filterIs, word: "equipped", match: func(item *inventory.Item) bool { return item.Equipped }},
		{
			name:  filterIs,
			word:  "invault",
			match: func(item *inventory.Item) bool { return item.Owner == inventory.OwnerVault },
		},
		{name: filterIs, word: "postmaster", match: inPostmaster},
		{name: filterIs, word: "inpostmaster", match: inPostmaster},
		{
			name:  filterIs,
			word:  "locked",
			match: func(item *inventory.Item) bool { return item.Lockable && item.Locked },
		},
		{
			name:  filterIs,
			word:  "unlocked",
			match: func(item *inventory.Item) bool { return item.Lockable && !item.Locked },
		},
		{name: filterIs, word: "titan", match: ofClass(bungie.ClassTitan)},
		{name: filterIs, word: "hunter", match: ofClass(bungie.ClassHunter)},
		{name: filterIs, word: "warlock", match: ofClass(bungie.ClassWarlock)},
		{name: filterIs, word: "favorite", match: func(item *inventory.Item) bool { return item.Favorite }},
		{name: filterIs, word: "autorifle", match: inCategory(bungie.CategoryAutoRifle)},
		{name: filterIs, word: "handcannon", match: inCategory(bungie.CategoryHandCannon)},
		{name: filterIs, word: "pulserifle", match: inCategory(bungie.CategoryPulseRifle)},
		{name: filterIs, word: "scoutrifle", match: inCategory(bungie.CategoryScoutRifle)},
		{name: filterIs, word: "fusionrifle", match: inCategory(bungie.CategoryFusionRifle)},
		{name: filterIs, word: "sniperrifle", match: inCategory(bungie.CategorySniperRifle)},
		{name: filterIs, word: "shotgun", match: inCategory(bungie.CategoryShotgun)},
		{name: filterIs, word: "machinegun", match: inCategory(bungie.CategoryMachineGun)},
		{name: filterIs, word: "lmg", match: inCategory(bungie.CategoryMachineGun)},
		{name: filterIs, word: "rocketlauncher", match: inCategory(bungie.CategoryRocketLauncher)},
		{name: filterIs, word: "sidearm", match: inCategory(bungie.CategorySidearm)},
		{name: filterIs, word: "sword", match: inCategory(bungie.CategorySword)},
		{
			name:  filterIs,
			word:  "grenadelauncher",
			match: inCategory(bungie.CategoryGrenadeLaunchers, -bungie.CategoryGrenadeLaunchers),
		},
		{name: filterIs, word: "heavygrenadelauncher", match: inCategory(bungie.CategoryGrenadeLaunchers)},
		{name: filterIs, word: "specialgrenadelauncher", match: inCategory(-bungie.CategoryGrenadeLaunchers)},
		{name: filterIs, word: "tracerifle", match: inCategory(bungie.CategoryTraceRifles)},
		{name: filterIs, word: "linearfusionrifle", match: inCategory(bungie.CategoryLinearFusionRifles)},
		{name: filterIs, word: "lfr", match: inCategory(bungie.CategoryLinearFusionRifles)},
		{name: filterIs, word: "submachine", match: inCategory(bungie.CategorySubmachineGuns)},
		{name: filterIs, word: "smg", match: inCategory(bungie.CategorySubmachineGuns)},
		{name: filterIs, word: "bow", match: inCategory(bungie.CategoryBows)},
		{name: filterIs, word: "glaive", match: inCategory(bungie.CategoryGlaives)},
		{name: filterIs, word: "transmat", match: inCategory(bungie.CategoryShipModsTransmatEffects)},
		{name: filterIs, word: "weaponmod", match: inCategory(bungie.CategoryWeaponMods)},
		{name: filterIs, word: "armormod", match: inCategory(bungie.CategoryArmorMods)},
		{name: filterIs, word: "reptoken", match: inCategory(bungie.CategoryReputationTokens)},
		{name: filterIs, word: "primary", match: ofAmmo(bungie.AmmoPrimary)},
		{name: filterIs, word: "special", match: ofAmmo(bungie.AmmoSpecial)},
		{name: filterIs, word: "heavy", match: ofAmmo(bungie.AmmoHeavy)},
		{name: filterIs, word: "dupe", build: isDupe},
		{name: filterIs, word: "incurrentchar", build: inCurrentChar},
		{name: filterIs, word: "currentclass", build: currentClass},
		{name: filterIs, word: "common", match: ofRarity(inventory.RarityCommon)},
		{name: filterIs, word: "uncommon", match: ofRarity(inventory.RarityUncommon)},
		{name: filterIs, word: "rare", match: ofRarity(inventory.RarityRare)},
		{name: filterIs, word: "legendary", match: ofRarity(inventory.RarityLegendary)},
		{name: filterIs, word: "exotic", match: ofRarity(inventory.RarityExotic)},
		{name: filterIs, word: "white", match: ofRarity(inventory.RarityCommon)},
		{name: filterIs, word: "green", match: ofRarity(inventory.RarityUncommon)},
		{name: filterIs, word: "blue", match: ofRarity(inventory.RarityRare)},
		{name: filterIs, word: "purple", match: ofRarity(inventory.RarityLegendary)},
		{name: filterIs, word: "yellow", match: ofRarity(inventory.RarityExotic)},
		{name: filterIs, word: "kinetic", match: ofElement(bungie.DamageKinetic)},
		{name: filterIs, word: "arc", match: ofElement(bungie.DamageArc)},
		{name: filterIs, word: "solar", match: ofElement(bungie.DamageThermal)},
		{name: filterIs, word: "void", match: ofElement(bungie.DamageVoid)},
		{name: filterIs, word: "stasis", match: ofElement(bungie.DamageStasis)},
		{name: filterIs, word: "strand", match: ofElement(bungie.DamageStrand)},
		{name: filterIs, word: "light", match: ofElement(bungie.DamageArc, bungie.DamageThermal, bungie.DamageVoid)},
		{name: filterIs, word: "dark", match: ofElement(bungie.DamageStasis, bungie.DamageStrand)},
		{name: filterIs, word: "engrams", match: inBuckets(bungie.BucketEngrams)},
		{name: filterIs, word: "lostitems", match: inBuckets(bungie.BucketPostmaster)},
		{name: filterIs, word: "messages", match: inBuckets(bungie.BucketMessages)},
		{name: filterIs, word: "specialorders", match: inBuckets(bungie.BucketSpecialOrders)},
		{name: filterIs, word: "kineticslot", match: inBuckets(bungie.BucketKinetic)},
		{name: filterIs, word: "energy", match: inBuckets(bungie.BucketEnergy)},
		{name: filterIs, word: "power", match: inBuckets(bungie.BucketPower)},
		{name: filterIs, word: "helmet", match: inBuckets(bungie.BucketHelmet)},
		{name: filterIs, word: "gauntlets", match: inBuckets(bungie.BucketGauntlets)},
		{name: filterIs, word: "chest", match: inBuckets(bungie.BucketChestArmor)},
		{name: filterIs, word: "leg", match: inBuckets(bungie.BucketLegArmor)},
		{name: filterIs, word: "classitem", match: inBuckets(bungie.BucketClassArmor)},
		{name: filterIs, word: "subclass", match: inBuckets(bungie.BucketSubclass)},
		{name: filterIs, word: "seasonalartifacts", match: inBuckets(bungie.BucketArtifacts)},
		{name: filterIs, word: "ghost", match: inBuckets(bungie.BucketGhost)},
		{name: filterIs, word: "emblems", match: inBuckets(bungie.BucketEmblems)},
		{name: filterIs, word: "ships", match: inBuckets(bungie.BucketShips)},
		{name: filterIs, word: "vehicle", match: inBuckets(bungie.BucketVehicle)},
		{name: filterIs, word: "emotes", match: inBuckets(bungie.BucketEmotes)},
		{name: filterIs, word: "finishers", match: inBuckets(bungie.BucketFinishers)},
		{name: filterIs, word: "accessories", match: inBuckets(bungie.BucketAccessories)},
		{name: filterIs, word: "consumables", match: inBuckets(bungie.BucketConsumables)},
		{name: filterIs, word: "modifications", match: inBuckets(bungie.BucketModifications)},
		{name: filterIs, word: "haspower", match: hasPower},
		{name: filterIs, word: "haslight", match: hasPower},
		{name: filterIs, word: "autotagged", match: isAutotagged},
		{name: filterTag, value: itemValue(tagMatch)},
		{name: filterDupe, word: "item", value: dupeValue},
		{name: filterCount, value: countValue},
		{name: filterID, value: itemValue(idMatch)},
		{name: filterHash, value: itemValue(hashMatch)},
		{name: filterName, value: itemValue(nameMatch)},
		{name: filterExactName, value: itemValue(exactNameMatch)},
		{name: filterPower, value: powerValue},
		{name: filterLight, value: powerValue},
		ratingFilter(filterRate, rolls.ModePvE, tierRange, rollValues),
		ratingFilter(filterRating, rolls.ModePvE, tierRange, rollValues),
		ratingFilter(filterRateBase, rolls.ModePvE, tierRange, baseValues),
		ratingFilter(filterRatingBase, rolls.ModePvE, tierRange, baseValues),
		ratingFilter(filterMisses, rolls.ModePvE, rangeFormat{}, missValues),
		setBonusFilter(filterSetBonus, rolls.ModePvE),
		verdictFilter(filterVerdict, rolls.ModePvE),
		ratingFilter(pvpPrefix+filterRate, rolls.ModePvP, tierRange, rollValues),
		ratingFilter(pvpPrefix+filterRating, rolls.ModePvP, tierRange, rollValues),
		ratingFilter(pvpPrefix+filterRateBase, rolls.ModePvP, tierRange, baseValues),
		ratingFilter(pvpPrefix+filterRatingBase, rolls.ModePvP, tierRange, baseValues),
		ratingFilter(pvpPrefix+filterMisses, rolls.ModePvP, rangeFormat{}, missValues),
		setBonusFilter(pvpPrefix+filterSetBonus, rolls.ModePvP),
		verdictFilter(pvpPrefix+filterVerdict, rolls.ModePvP),
	}

	// isFilters maps each is: word of searchFilters to the builder of its
	// test.
	isFilters = isTable(searchFilters)

	// valueFilters maps the name of each filter of searchFilters that
	// takes a value to the filter.
	valueFilters = valueTable(searchFilters)

	// tierRange is the range format of rate:, ratebase:, and their other
	// names. Its overloads map each tier of the roll sheets, in lowercase,
	// to a number. S maps to 6 and F maps to 0, so rate:>=b means B or
	// better. DIM tries an overload before a number, so rate:>=5 means
	// rate:>=a.
	tierRange = rangeFormat{overloads: tierOverloads()}

	// verdictKinds maps each value of the verdict filters to the kind of
	// verdict that the value matches, as VERDICT_KINDS of the web client
	// maps it. The value same matches the kind equal.
	verdictKinds = map[string]string{
		"better": verdictBetter,
		"same":   verdictEqual,
		"worse":  verdictWorse,
		"only":   verdictOnly,
		"mixed":  verdictMixed,
	}

	// wordStart matches a text that starts with a character of \w. The \w
	// of Go and the \w of a JavaScript regular expression without the u
	// flag both match an ASCII letter, an ASCII digit, or an underscore.
	wordStart = regexp.MustCompile(`^\w`)
)

// Outcome is what a query, or one term of a query, gives one item. The
// zero outcome is a no match. Only an undecided outcome holds reasons. It
// gives each reason that keeps the server from deciding the item, one
// time each.
type Outcome struct {
	Reasons []string
	State   OutcomeState
}

// decided returns the match outcome when matched holds, and the no match
// outcome otherwise.
func decided(matched bool) Outcome {
	if matched {
		return Outcome{State: Match}
	}

	return Outcome{}
}

// and returns the outcome of an and of o and other in Kleene logic. A no
// match of either operand gives a no match. Otherwise an undecided operand
// gives an undecided outcome, and two matches give a match.
func (o Outcome) and(other Outcome) Outcome {
	switch {
	case o.State == NoMatch || other.State == NoMatch:
		return Outcome{}
	case o.State == Undecided || other.State == Undecided:
		return o.undecidedWith(other)
	default:
		return Outcome{State: Match}
	}
}

// or returns the outcome of an or of o and other in Kleene logic. A match
// of either operand gives a match. Otherwise an undecided operand gives an
// undecided outcome, and two no matches give a no match.
func (o Outcome) or(other Outcome) Outcome {
	switch {
	case o.State == Match || other.State == Match:
		return Outcome{State: Match}
	case o.State == Undecided || other.State == Undecided:
		return o.undecidedWith(other)
	default:
		return Outcome{}
	}
}

// not returns the outcome of a not of o in Kleene logic. It swaps a match
// and a no match, and it keeps an undecided outcome with its reasons.
func (o Outcome) not() Outcome {
	switch o.State {
	case Match:
		return Outcome{}
	case NoMatch:
		return Outcome{State: Match}
	default:
		return o
	}
}

// undecidedWith returns the undecided outcome with the reasons of o and
// then each reason of other that o does not hold. Only an undecided
// outcome holds reasons, so the result names the reasons of the undecided
// operands.
func (o Outcome) undecidedWith(other Outcome) Outcome {
	reasons := slices.Clone(o.Reasons)
	for _, reason := range other.Reasons {
		reasons = appendNew(reasons, reason)
	}

	return Outcome{Reasons: reasons, State: Undecided}
}

// QueryTest builds the test of a query, or of one term of a query, for a
// search context. A filter that reads other items, such as is:dupe,
// groups the items of the context when it builds the test, as DIM does
// when it builds the filter.
type QueryTest func(ctx Context) func(*inventory.Item) Outcome

// decidedTest builds the predicate of a term that the server decides for
// every item, such as is:dupe.
type decidedTest func(ctx Context) func(*inventory.Item) bool

// fromDecided adapts the predicate builder of a term that the server
// decides for every item to a [QueryTest].
func fromDecided(build decidedTest) QueryTest {
	return func(ctx Context) func(*inventory.Item) Outcome {
		predicate := build(ctx)

		return func(item *inventory.Item) Outcome { return decided(predicate(item)) }
	}
}

// itemTest adapts a predicate that reads only the tested item to a
// [QueryTest] that decides every item and ignores the search context.
func itemTest(p func(*inventory.Item) bool) QueryTest {
	return fromDecided(func(Context) func(*inventory.Item) bool { return p })
}

// Context holds what the filters read besides the tested item, as
// the filter context of DIM does.
type Context struct {
	// sources holds the [rolls.RatingSource] of each game mode, keyed by the
	// mode. Only the rating filters read it, so a query without one can
	// run with no sources.
	sources map[string]rolls.RatingSource
	// items holds every item that DIM builds, in every location, the vault
	// included. The web client passes these items to DIM as allItems.
	items []*inventory.Item
	// current is the current character, the one that the player played
	// last.
	current inventory.Character
}

// NewContext returns the context of a query on the items of s. The
// context holds every item of s that [builtItems] keeps, in every
// location, the vault included. It holds current as the current character
// and sources as the [rolls.RatingSource] of each game mode.
func NewContext(
	s *inventory.Snapshot,
	current inventory.Character,
	sources map[string]rolls.RatingSource,
) Context {
	return Context{current: current, items: builtItems(s), sources: sources}
}

// searchFilter is one filter that the preview evaluates. An is: word sets
// match or build, and a filter that takes a value sets value.
type searchFilter struct {
	// match is the predicate of an is: word that reads only the tested
	// item.
	match func(*inventory.Item) bool
	// build builds the predicate of an is: word that reads the search
	// context.
	build decidedTest
	// value reads the value of a filter that takes one, such as tag:keep,
	// and returns the builder of its test. It returns an error that wraps
	// errInvalidValue for a value that the web client rejects. It returns
	// an error that wraps errUnsupportedTerm for a value that the preview
	// does not evaluate.
	value func(args string) (QueryTest, error)
	// name is the filter name, such as "is" or "tag".
	name string
	// word is the is: word, such as "weapon". A filter that takes a value
	// sets word only when the preview evaluates one value alone, such as
	// item for dupe:item.
	word string
	// mode is the game mode whose ratings a rating filter reads, such as
	// [rolls.ModePvP] for pvprate:. It is empty for every other filter.
	mode string
}

// keyword returns the text that names the filter, such as "is:weapon" or
// "tag:".
func (f searchFilter) keyword() string {
	return f.name + ":" + f.word
}

// CompiledQuery is a search query in the form that the preview evaluates.
// Exactly one of three cases holds for it. Test is set, Unsupported holds
// a term, or Invalid holds a term.
type CompiledQuery struct {
	// Test builds the test of the query for a search context. It is nil
	// when Unsupported or Invalid holds a term.
	Test QueryTest
	// Unsupported holds the Text of each term that the preview does not
	// evaluate, one time each, in the order of the query. It is empty when
	// Invalid holds a term.
	Unsupported []string
	// Invalid holds the Text of each term whose value the web client
	// rejects, one time each, in the order of the query.
	Invalid []string
	// Modes holds the game modes whose ratings the rating filters of the
	// query read, in the order of [rolls.Modes]. It is empty when Test is
	// nil.
	Modes []string
}

// queryCompiler holds what [CompileQuery] collects while it walks a parse
// tree. It collects the unsupported terms, the terms whose value the web
// client rejects, and the game modes of the rating filters.
type queryCompiler struct {
	unsupported []string
	invalid     []string
	modes       []string
}

// CompileQuery builds the test of the parse tree that [ParseQuery]
// returns. It evaluates only the filters of searchFilters. The web client
// voids a whole query when one term is invalid. A test of the other terms
// can therefore match items that the web client does not match. A query
// with a term whose value the web client rejects has no test, and the
// result names each such term. The web client skips that query whatever
// its other terms mean, so the result names no unsupported term. Any other
// query with a term outside searchFilters has no test either, and the
// result names each such term.
func CompileQuery(root *QueryNode) CompiledQuery {
	var c queryCompiler

	test := c.node(root)

	switch {
	case len(c.invalid) > 0:
		return CompiledQuery{Invalid: c.invalid}
	case len(c.unsupported) > 0:
		return CompiledQuery{Unsupported: c.unsupported}
	default:
		var modes []string

		for _, mode := range rolls.Modes {
			if slices.Contains(c.modes, mode) {
				modes = append(modes, mode)
			}
		}

		return CompiledQuery{Test: test, Modes: modes}
	}
}

// node returns the test builder of n, or nil for a filter that the preview
// does not evaluate or whose value the web client rejects. The and node,
// the or node, and the not node combine the outcomes of their operands in
// Kleene logic.
func (c *queryCompiler) node(n *QueryNode) QueryTest {
	switch n.Op {
	case OpAnd, OpOr:
		builds := make([]QueryTest, 0, len(n.Operands))
		for _, operand := range n.Operands {
			builds = append(builds, c.node(operand))
		}

		// The walk starts from the outcome that leaves each operand in
		// place. It stops at the outcome that no later operand changes:
		// a no match of an and node, or a match of an or node.
		combine, start, final := Outcome.and, Match, NoMatch
		if n.Op == OpOr {
			combine, start, final = Outcome.or, NoMatch, Match
		}

		return func(ctx Context) func(*inventory.Item) Outcome {
			tests := make([]func(*inventory.Item) Outcome, 0, len(builds))
			for _, build := range builds {
				tests = append(tests, build(ctx))
			}

			return func(item *inventory.Item) Outcome {
				result := Outcome{State: start}

				for _, test := range tests {
					result = combine(result, test(item))
					if result.State == final {
						break
					}
				}

				return result
			}
		}

	case OpNot:
		build := c.node(n.Operand)

		return func(ctx Context) func(*inventory.Item) Outcome {
			test := build(ctx)

			return func(item *inventory.Item) Outcome { return test(item).not() }
		}

	default:
		build, mode, err := filterTest(n)

		switch {
		case errors.Is(err, errInvalidValue):
			c.invalid = appendNew(c.invalid, n.Text)
		case err != nil:
			c.unsupported = appendNew(c.unsupported, n.Text)
		case mode != "":
			c.modes = appendNew(c.modes, mode)
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

// filterTest returns the test builder of the filter node n and the game
// mode whose ratings the filter reads. The mode is empty for a filter
// that reads no ratings. filterTest returns an error that wraps
// errUnsupportedTerm when the preview does not evaluate the filter or its
// value. It returns an error that wraps errInvalidValue when the web
// client rejects the value. Only the table decides which is: words the
// preview evaluates, so is:constructor and is:__proto__ stay unsupported.
// DIM finds both words on the prototype of its filter table and matches
// nothing.
func filterTest(n *QueryNode) (QueryTest, string, error) {
	if n.Type == filterIs {
		build, ok := isFilters[n.Args]
		if !ok {
			return nil, "", errUnsupportedTerm
		}

		return build, "", nil
	}

	f, ok := valueFilters[n.Type]
	if !ok {
		return nil, "", errUnsupportedTerm
	}

	build, err := f.value(n.Args)

	return build, f.mode, err
}

// QueryResult is what a query gives a list of items. Matches holds the
// items that the query matches. Undecided holds the items whose outcome
// the server cannot decide. Both lists follow the order of
// [inventory.ByNameAndID].
type QueryResult struct {
	Matches   []*inventory.Item
	Undecided []UndecidedItem
}

// UndecidedItem is an item whose outcome the server cannot decide, with
// the reasons of the outcome.
type UndecidedItem struct {
	Item    *inventory.Item
	Reasons []string
}

// Evaluate returns what q gives each item of items in ctx. It leaves out
// each item that q does not match.
func (q CompiledQuery) Evaluate(ctx Context, items []*inventory.Item) QueryResult {
	var result QueryResult

	test := q.Test(ctx)
	for _, item := range items {
		got := test(item)

		if got.State == Match {
			result.Matches = append(result.Matches, item)
		}

		if got.State == Undecided {
			result.Undecided = append(result.Undecided, UndecidedItem{Item: item, Reasons: got.Reasons})
		}
	}

	slices.SortStableFunc(result.Matches, inventory.ByNameAndID)
	slices.SortStableFunc(
		result.Undecided,
		func(a, b UndecidedItem) int { return inventory.ByNameAndID(a.Item, b.Item) },
	)

	return result
}

// UnratedReason returns the [rolls.RatingReason] of the first game mode of
// q.Modes whose source in sources holds an Err. It returns "" when every
// such mode has ratings.
func (q CompiledQuery) UnratedReason(sources map[string]rolls.RatingSource) string {
	for _, mode := range q.Modes {
		err := sources[mode].Err
		if err != nil {
			return rolls.RatingReason(mode, err)
		}
	}

	return ""
}

// SupportedKeywords returns the keyword of each filter that the preview
// evaluates, in the order of searchFilters.
func SupportedKeywords() []string {
	keywords := make([]string, 0, len(searchFilters))
	for _, f := range searchFilters {
		keywords = append(keywords, f.keyword())
	}

	return keywords
}

// builtItems returns the items of s that DIM builds. DIM builds no item
// whose current bucket holds dummy items, as [inDummyBucket] decides. The
// web client also builds no item whose item set its definitions lack, as
// [setBuilt] decides. DIM also builds no item without a definition, but
// the snapshot keeps such an item, and so does builtItems.
func builtItems(s *inventory.Snapshot) []*inventory.Item {
	var built []*inventory.Item

	for _, item := range s.Items {
		if !inDummyBucket(item) && setBuilt(s, item) {
			built = append(built, item)
		}
	}

	return built
}

// inDummyBucket reports whether the current bucket of item holds dummy
// items. The current bucket of a lost item is the Lost Items bucket of
// the Postmaster, and the current bucket of every other item is
// Item.Bucket.
func inDummyBucket(item *inventory.Item) bool {
	current := item.Bucket
	if item.Lost {
		current = bungie.BucketPostmaster
	}

	return current == bungie.BucketForbidden || current == bungie.BucketDummy
}

// setBuilt reports whether the web client can build the item set of item.
// DIM looks up the set of every item with a set hash other than 0. The
// lookup of the web client throws for a set that the EquipableItemSet
// definitions lack, and for every set when the service index lists no
// EquipableItemSet artifact. DIM then adds no item.
func setBuilt(s *inventory.Snapshot, item *inventory.Item) bool {
	return item.SetHash == 0 || (s.ItemSetsListed && item.SetKnown)
}

// OwnedItems returns the items that the rule editor of the web client
// previews a rule against. The editor keeps each item of [builtItems]
// that [previewable] accepts, in every location.
func OwnedItems(s *inventory.Snapshot) []*inventory.Item {
	var owned []*inventory.Item

	for _, item := range builtItems(s) {
		if previewable(item) {
			owned = append(owned, item)
		}
	}

	return owned
}

// previewable reports whether the rule editor of the web client previews
// a rule against item when DIM builds it. The editor keeps an instanced
// item that DIM marks as equipment. DIM marks an item whose definition has
// an equipping block, except an emote or a finisher.
func previewable(item *inventory.Item) bool {
	return item.InstanceID != "" && isEquipment(item)
}

// UnbuiltLine returns the line that counts the owned items that
// [OwnedItems] leaves out because the web client cannot build their item
// set, as [setBuilt] decides. It returns "" when OwnedItems leaves out no
// such item. The line gives one reason, because the service index either
// lists the EquipableItemSet artifact or does not.
func UnbuiltLine(s *inventory.Snapshot) string {
	left := 0

	for _, item := range s.Items {
		if !inDummyBucket(item) && !setBuilt(s, item) && previewable(item) {
			left++
		}
	}

	if left == 0 {
		return ""
	}

	if s.ItemSetsListed {
		return fmt.Sprintf(
			"The server leaves out %d owned items whose item set the EquipableItemSet definitions lack, "+
				"because the web client builds no such item.",
			left,
		)
	}

	return fmt.Sprintf(
		"The server leaves out %d owned items that belong to an item set, "+
			"because the service publishes no EquipableItemSet artifact "+
			"and the web client builds no such item without it.",
		left,
	)
}

// isEquipment reports whether DIM marks item as equipment. The web client
// marks no item as classified, so the equipping block of the definition
// decides alone.
func isEquipment(item *inventory.Item) bool {
	return item.Equippable && item.Bucket != bungie.BucketEmotes && item.Bucket != bungie.BucketFinishers
}

// isComparable reports whether DIM marks item as comparable, which the
// dupe filters read. DIM marks equipment that the game can lock, except an
// emblem.
func isComparable(item *inventory.Item) bool {
	return isEquipment(item) && item.Lockable && item.Bucket != bungie.BucketEmblems
}

// inBuckets returns the predicate of an item whose bucket is one of
// buckets. DIM reads the bucket of the definition. Item.Bucket holds it
// for an item in the vault or in the Lost Items bucket, and holds the
// profile bucket for every other item. An item at the Postmaster
// therefore matches the bucket of its definition, and not the Lost Items
// bucket.
func inBuckets(buckets ...uint32) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool { return slices.Contains(buckets, item.Bucket) }
}

// inPostmaster reports whether DIM puts the current bucket of item in its
// Postmaster category. The current bucket of a lost item is the Lost Items
// bucket. Item.Postmaster also marks an item at the Postmaster in a bucket
// outside that category, which DIM does not count.
func inPostmaster(item *inventory.Item) bool {
	return item.Lost || slices.Contains(PostmasterBuckets, item.Bucket)
}

// ofClass returns the predicate of an item for the Bungie DestinyClass
// value classType.
func ofClass(classType int) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool { return item.ClassType == classType }
}

// inCategory returns the predicate of an item whose item categories hold
// one of categories.
func inCategory(categories ...int64) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool {
		return slices.ContainsFunc(item.Categories, func(c int64) bool { return slices.Contains(categories, c) })
	}
}

// ofAmmo returns the predicate of an item for the Bungie
// DestinyAmmunitionType value ammoType.
func ofAmmo(ammoType int) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool { return item.AmmoType == ammoType }
}

// ofRarity returns the predicate of an item whose rarity is the DIM rarity
// name rarity. DIM reads the rarity from the tier type of the definition,
// as [inventory.RarityName] maps it, and not from the tier name that the
// definition displays.
func ofRarity(rarity string) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool { return inventory.RarityName(item.TierType) == rarity }
}

// ofElement returns the predicate of an item whose instance has one of
// the Bungie DamageType values elements. An item without an element
// matches none.
func ofElement(elements ...int) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool { return slices.Contains(elements, item.Element) }
}

// hasPower reports whether DIM counts a power for item. It is the
// predicate of is:haspower and is:haslight.
func hasPower(item *inventory.Item) bool {
	return item.Power > 0
}

// isAutotagged reports whether an autotag rule set the tag of item, as
// is:autotagged of the web client reads it. A tag that a person set
// clears the mark.
func isAutotagged(item *inventory.Item) bool {
	return item.TagRule != ""
}

// powerValue reads the range value of the filters power: and light:, as
// DIM reads it with [powerRange]. The predicate compares the power that
// DIM counts, so an item without power matches no value, also power:<10.
// A word of DIM such as pinnaclecap after a valid operator is a term that
// the preview does not evaluate.
func powerValue(value string) (QueryTest, error) {
	compare, err := powerRange.valueComparator(value)
	if err != nil {
		return nil, err
	}

	return itemTest(func(item *inventory.Item) bool {
		return item.Power != 0 && compare(float64(item.Power))
	}), nil
}

// tagMatch returns the predicate of the filter tag:value, as the web
// client defines it. The value favorite reads the favorite mark. Every
// other value matches an item whose tag is that value, and the web client
// knows only the tags of [dvm.TagOrder]. An untagged item matches no value.
func tagMatch(value string) func(*inventory.Item) bool {
	if value == dvm.TagFavorite {
		return func(item *inventory.Item) bool { return item.Favorite }
	}

	return func(item *inventory.Item) bool { return item.Tag == value && slices.Contains(dvm.TagOrder, item.Tag) }
}

// nameMatch returns the predicate of the filter name:value, as DIM defines
// it. The filter matches an item when a word of its name starts with the
// value, and it compares both texts after [fold.Name] folds them. DIM
// puts a word boundary before the value only when the folded value starts
// with an ASCII letter, an ASCII digit, or an underscore. A value such as
// -x can therefore match in the middle of a word.
func nameMatch(value string) func(*inventory.Item) bool {
	folded := fold.Name(value)

	pattern := "(?i)"
	if wordStart.MatchString(folded) {
		pattern += `\b`
	}

	re, err := regexp.Compile(pattern + regexp.QuoteMeta(folded))
	if err != nil {
		// Compile refuses only a value of millions of characters. Each
		// character of the pattern matches one character of a name, so
		// such a value matches no name.
		return func(*inventory.Item) bool { return false }
	}

	return func(item *inventory.Item) bool { return re.MatchString(plainName(item)) }
}

// exactNameMatch returns the predicate of the filter exactname:value, as
// DIM defines it. The filter matches an item whose whole name equals the
// value after [fold.Name] folds both texts.
func exactNameMatch(value string) func(*inventory.Item) bool {
	folded := fold.Name(value)

	return func(item *inventory.Item) bool { return plainName(item) == folded }
}

// plainName returns the name of item in the form that the name filters
// compare. The lexer turned the typographic quotes of the value into ASCII
// quotes. plainName therefore applies [normalizeQuotes] to the name, as
// DIM does, and then [fold.Name] folds it.
func plainName(item *inventory.Item) string {
	return fold.Name(normalizeQuotes(item.Name))
}

// idMatch returns the predicate of the filter id:value, which matches the
// item whose instance id is the value.
func idMatch(value string) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool { return item.InstanceID == value }
}

// hashMatch returns the predicate of the filter hash:value. DIM reads the
// value with parseInt and the radix 10. parseInt skips leading whitespace,
// reads an optional sign, and then reads the leading decimal digits, so
// hash:12abc is hash:12. A value without leading digits matches nothing.
// So do a negative value and a value that does not fit in a uint32,
// because no item hash equals them. JavaScript compares -0 as equal to 0,
// so hash:-0 is hash:0.
func hashMatch(value string) func(*inventory.Item) bool {
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
		return func(*inventory.Item) bool { return false }
	}

	return func(item *inventory.Item) bool { return uint64(item.Hash) == hash }
}

// isDupe builds the predicate of is:dupe and dupe:item, as DIM builds it
// for the value item. It groups every item of the context by
// [itemDupeID]. It matches a comparable item whose group holds more than
// one item, except the default shader and an item of the Artifacts
// bucket.
func isDupe(ctx Context) func(*inventory.Item) bool {
	counts := dupeCounts(ctx.items, func(*inventory.Item) bool { return true })

	return func(item *inventory.Item) bool {
		return item.Hash != hashDefaultShader &&
			item.Bucket != bungie.BucketArtifacts &&
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
func dupeValue(value string) (QueryTest, error) {
	if value == "item" {
		return fromDecided(isDupe), nil
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
func countValue(value string) (QueryTest, error) {
	compare, err := rangeFormat{}.valueComparator(value)
	if err != nil {
		return nil, err
	}

	return fromDecided(func(ctx Context) func(*inventory.Item) bool {
		counts := dupeCounts(ctx.items, isComparable)

		return func(item *inventory.Item) bool { return compare(float64(counts[itemDupeID(item)])) }
	}), nil
}

// dupeCounts returns the number of items of items that keep accepts for
// each [itemDupeID].
func dupeCounts(items []*inventory.Item, keep func(*inventory.Item) bool) map[string]int {
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
// rarity name of [inventory.RarityName], and the decimal bucket hash without
// separators. A weapon name goes through [stripAdept] first. The web
// client classifies no item, so the key never falls back to the item
// hash.
func itemDupeID(item *inventory.Item) string {
	name := item.Name
	if slices.Contains(weaponBuckets, item.Bucket) {
		name = stripAdept(name)
	}

	return name + strconv.Itoa(item.ClassType) + inventory.RarityName(item.TierType) +
		strconv.FormatUint(uint64(item.Bucket), 10)
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
func inCurrentChar(ctx Context) func(*inventory.Item) bool {
	return func(item *inventory.Item) bool {
		return item.Owner == ctx.current.ID || item.Owner == inventory.OwnerAccount
	}
}

// currentClass builds the predicate of is:currentclass. It matches an item
// of the class of the current character.
func currentClass(ctx Context) func(*inventory.Item) bool {
	return ofClass(ctx.current.ClassType)
}

// rangeFormat is the range format of DIM, which reads a value such as >=2
// as a comparison with a number. overloads maps each word that stands for
// a number in the value, such as a season name. A nil map marks a filter
// without an overload table in DIM. unsupported lists the words of the
// overload table of DIM whose number the preview lacks.
type rangeFormat struct {
	overloads   map[string]float64
	unsupported []string
}

// valueComparator returns the predicate of a number for the value of a
// range filter. It returns an error that wraps errInvalidValue for a value
// that DIM rejects. For a word of unsupported, it returns an error that
// wraps errUnsupportedTerm and errOverloadValue.
func (f rangeFormat) valueComparator(value string) (func(float64) bool, error) {
	compare, err := f.comparator(value)

	switch {
	case errors.Is(err, errNotRange):
		return nil, fmt.Errorf("%w: %w", errInvalidValue, err)
	case errors.Is(err, errOverloadValue):
		return nil, fmt.Errorf("%w: %w", errUnsupportedTerm, err)
	}

	return compare, err
}

// comparator returns the predicate of a number for the range value, as
// rangeStringToComparator of DIM builds it. The value is an operator and
// then a word of overloads or a number. The operator is empty or one of
// =, <, <=, >, and >=. The empty operator and = both test equality. The
// comparator returns an error that wraps errNotRange for every other
// value, the empty value included.
//
// The comparator returns an error that wraps errOverloadValue for a word
// of unsupported after a valid operator. DIM reads the word before it
// checks the operator, but the comparator checks the operator first. A
// word of unsupported after an invalid operator therefore gives
// errNotRange, because DIM rejects that operator whatever the word is.
func (f rangeFormat) comparator(value string) (func(float64) bool, error) {
	m := rangeOverload.FindStringSubmatch(value)
	if len(m) == 3 && slices.Contains(f.unsupported, m[2]) {
		if !slices.Contains(rangeOperators, m[1]) {
			return nil, fmt.Errorf("%w: unknown operator %q", errNotRange, m[1])
		}

		return nil, fmt.Errorf("%w: %s", errOverloadValue, m[2])
	}

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
func itemValue(predicate func(value string) func(*inventory.Item) bool) func(string) (QueryTest, error) {
	return func(value string) (QueryTest, error) {
		return itemTest(predicate(value)), nil
	}
}

// isTable maps each is: word of filters to the builder of its test.
func isTable(filters []searchFilter) map[string]QueryTest {
	table := map[string]QueryTest{}

	for _, f := range filters {
		switch {
		case f.build != nil:
			table[f.word] = fromDecided(f.build)
		case f.match != nil:
			table[f.word] = itemTest(f.match)
		}
	}

	return table
}

// valueTable maps the name of each filter of filters that takes a value
// to the filter.
func valueTable(filters []searchFilter) map[string]searchFilter {
	table := map[string]searchFilter{}

	for _, f := range filters {
		if f.value != nil {
			table[f.name] = f
		}
	}

	return table
}

// ratingValues holds the numbers that a rating filter tests for one item.
// The filter matches the item when one of numbers passes the range of the
// filter. Reasons names each number that the server cannot read.
type ratingValues struct {
	numbers []float64
	reasons []string
}

// ratingFilter returns the rating filter of the name name, which reads
// the ratings of mode. Format reads the range value of the filter. Values
// returns the numbers that the range tests for an item, as the rating
// filters of the web client define them. The filter rejects a value that
// format rejects, as the web client does.
//
// The filter matches an item when one of its numbers passes the range,
// also when the server cannot read another number. Otherwise an item with
// a number that the server cannot read is undecided, and every other item
// is a no match. An item without numbers is therefore a no match.
func ratingFilter(
	name, mode string,
	format rangeFormat,
	values func(source rolls.RatingSource, item *inventory.Item) ratingValues,
) searchFilter {
	read := func(value string) (QueryTest, error) {
		compare, err := format.valueComparator(value)
		if err != nil {
			return nil, err
		}

		return func(ctx Context) func(*inventory.Item) Outcome {
			source := ctx.sources[mode]

			return func(item *inventory.Item) Outcome {
				v := values(source, item)

				switch {
				case slices.ContainsFunc(v.numbers, compare):
					return Outcome{State: Match}
				case len(v.reasons) > 0:
					return Outcome{Reasons: v.reasons, State: Undecided}
				default:
					return Outcome{}
				}
			}
		}, nil
	}

	return searchFilter{name: name, mode: mode, value: read}
}

// rollValues returns the numbers of rate: and rating: for item. The
// numbers are the tier of each set bonus of the item, which [setValues]
// gives, and the tier of the roll that [rolls.Rate] gives the copy.
//
// A copy without perks on a row with perk columns has a tier that the
// server cannot read. The web client rates such a copy from the socket
// list of DIM, which can come from the definitions. A row without a tier
// gives no copy a tier, so the server decides a copy without perks on such
// a row.
func rollValues(source rolls.RatingSource, item *inventory.Item) ratingValues {
	v := setValues(source, item)

	roll := rolls.Rate(source, item).Roll
	if roll == nil {
		return v
	}

	if roll.Unknown && roll.Row.Tier != "" {
		v.reasons = append(v.reasons, ReasonNoPerks)

		return v
	}

	v.numbers = appendTier(v.numbers, roll.Overall)

	return v
}

// baseValues returns the numbers of ratebase: and ratingbase: for item.
// The numbers are the tier of each set bonus of the item, which
// [setValues] gives, and the tier of the sheet row of the item hash,
// whatever the copy rolled.
func baseValues(source rolls.RatingSource, item *inventory.Item) ratingValues {
	v := setValues(source, item)

	roll := rolls.Rate(source, item).Roll
	if roll != nil {
		v.numbers = appendTier(v.numbers, roll.Row.Tier)
	}

	return v
}

// missValues returns the number of misses: for item, which is the number
// of perk columns that the roll of the copy missed. Only a roll with perk
// columns has the number. The server cannot read the number for a copy
// without perks on a row with perk columns.
func missValues(source rolls.RatingSource, item *inventory.Item) ratingValues {
	roll := rolls.Rate(source, item).Roll

	switch {
	case roll == nil:
		return ratingValues{}
	case roll.Unknown:
		return ratingValues{reasons: []string{ReasonNoPerks}}
	case len(roll.Columns) == 0:
		return ratingValues{}
	}

	missed := 0

	for _, column := range roll.Columns {
		if column.Missed() {
			missed++
		}
	}

	return ratingValues{numbers: []float64{float64(missed)}}
}

// setValues returns the number of the tier of each set bonus of item that
// the sheet of source rates, as rate: and ratebase: test them in the web
// client. A bonus without a tier adds no number. An item without a set
// has no number.
func setValues(source rolls.RatingSource, item *inventory.Item) ratingValues {
	var v ratingValues

	for _, bonus := range setRatings(item, source.Sheet) {
		v.numbers = appendTier(v.numbers, bonus.Tier)
	}

	return v
}

// setRatings returns the [rolls.SetBonus] of each sandbox perk of the item set
// of item that sheet rates, in the order of the set. setRatings of the web
// client also sorts the bonuses by their pieces, but no filter reads that
// order. A nil sheet rates no bonus.
func setRatings(item *inventory.Item, sheet *rolls.Sheet) []rolls.SetBonus {
	if sheet == nil {
		return nil
	}

	var bonuses []rolls.SetBonus

	for _, perk := range item.SetPerks {
		bonus, ok := sheet.SetBonuses[perk]
		if ok {
			bonuses = append(bonuses, bonus)
		}
	}

	return bonuses
}

// setBonusFilter returns the filter setbonus: of the name name, which
// reads the set bonuses that the sheet of mode rates. The value is a tier,
// such as s, or a size prefix and a tier, such as 4pc:s. The filter
// matches an item with a set bonus of that tier, and a size prefix also
// requires that the bonus need that number of pieces. The filter rejects
// every value outside setBonusQueries, as DIM does.
func setBonusFilter(name, mode string) searchFilter {
	read := func(value string) (QueryTest, error) {
		if !slices.Contains(setBonusQueries, value) {
			return nil, fmt.Errorf("%w: %s:%s", errInvalidValue, name, value)
		}

		size, tier, sized := strings.Cut(value, ":")
		if !sized {
			tier = size
		}

		pieces := setBonusPieces[size]
		want := rolls.LowerTiers[tier]

		return fromDecided(func(ctx Context) func(*inventory.Item) bool {
			sheet := ctx.sources[mode].Sheet

			return func(item *inventory.Item) bool {
				return slices.ContainsFunc(setRatings(item, sheet), func(bonus rolls.SetBonus) bool {
					return bonus.Tier == want && (!sized || bonus.Pieces == pieces)
				})
			}
		}), nil
	}

	return searchFilter{name: name, mode: mode, value: read}
}

// setBonusSuggestions returns the values of setBonusQueries, as the web
// client builds its suggestions of setbonus:. They are each tier in
// lowercase, and then each size prefix of setBonusPieces with each tier.
func setBonusSuggestions() []string {
	values := make([]string, 0, len(rolls.Tiers)*(len(setBonusPieces)+1))
	for _, tier := range rolls.Tiers {
		values = append(values, strings.ToLower(tier))
	}

	for _, size := range slices.Sorted(maps.Keys(setBonusPieces)) {
		for _, tier := range rolls.Tiers {
			values = append(values, size+":"+strings.ToLower(tier))
		}
	}

	return values
}

// appendTier appends the number that tierRange gives tier to numbers. A
// row or roll without a tier adds no number.
func appendTier(numbers []float64, tier string) []float64 {
	n, ok := tierRange.overloads[strings.ToLower(tier)]
	if !ok {
		return numbers
	}

	return append(numbers, n)
}

// tierOverloads returns the overloads of tierRange. Each tier of [rolls.Tiers],
// in lowercase, maps to the number of tiers after it, as TIER_VALUES of
// the web client maps it.
func tierOverloads() map[string]float64 {
	overloads := make(map[string]float64, len(rolls.Tiers))
	for i, tier := range rolls.Tiers {
		overloads[strings.ToLower(tier)] = float64(len(rolls.Tiers) - 1 - i)
	}

	return overloads
}

// verdictFilter returns the verdict filter of the name name, which reads
// the sheet of mode. The value is a key of verdictKinds, and the filter
// rejects any other value, as the web client does. The filter matches a
// copy whose verdict in [verdictIn] is the kind of the value. A copy
// without a verdict is a no match. A copy whose verdict the server cannot
// decide is undecided, whatever the value.
func verdictFilter(name, mode string) searchFilter {
	read := func(value string) (QueryTest, error) {
		kind, ok := verdictKinds[value]
		if !ok {
			return nil, fmt.Errorf("%w: %s:%s", errInvalidValue, name, value)
		}

		return func(ctx Context) func(*inventory.Item) Outcome {
			source := ctx.sources[mode]
			copies := copiesByHash(ctx.items)

			return func(item *inventory.Item) Outcome {
				got, known := verdictIn(source, copies, item)
				if !known {
					return Outcome{Reasons: []string{ReasonVerdictNoPerks}, State: Undecided}
				}

				return decided(got == kind)
			}
		}, nil
	}

	return searchFilter{name: name, mode: mode, value: read}
}

// verdictIn returns the kind of verdict that the source of one game mode
// gives item against its rivals, as verdictIn of the web client does.
// Copies holds the items with an instance id, keyed by item hash, as
// [copiesByHash] builds it. [rivalsOf] picks the rivals. Each rival that
// [rolls.Rate] gives no roll drops out.
//
// The kind is empty for a copy without a verdict. A weapon that the sheet
// does not list has none, and so has a copy on a row without perk
// columns.
//
// The second result is false when the server cannot decide the verdict.
// The profile then lists no perks for the copy or for one of its rivals.
// The web client gives no verdict to a copy whose sockets are missing or
// come from the definitions, and it drops such a rival. The server cannot
// tell such a copy from one whose perks the web client reads. A rival
// without perks leaves the verdict undecided also when another rival
// already makes the copy worse.
func verdictIn(source rolls.RatingSource, copies map[uint32][]*inventory.Item, item *inventory.Item) (string, bool) {
	roll := rolls.Rate(source, item).Roll

	switch {
	case roll == nil:
		return "", true
	case roll.Unknown:
		return "", false
	case len(roll.Columns) == 0:
		return "", true
	}

	var rivals [][]int

	for _, other := range rivalsOf(source.Sheet, copies, item) {
		rival := rolls.Rate(source, other).Roll

		switch {
		case rival == nil:
			continue
		case rival.Unknown:
			return "", false
		}

		rivals = append(rivals, hitSet(rival))
	}

	return verdictOf(hitSet(roll), rivals), true
}

// copiesByHash returns the items of items that have an instance id, keyed
// by item hash. The web client finds the rivals of a copy among every
// item of the account, the vault included. It leaves out each item
// without an instance id.
func copiesByHash(items []*inventory.Item) map[uint32][]*inventory.Item {
	copies := map[uint32][]*inventory.Item{}

	for _, item := range items {
		if item.InstanceID != "" {
			copies[item.Hash] = append(copies[item.Hash], item)
		}
	}

	return copies
}

// rivalsOf returns the rivals of item, which are the other copies whose
// hash is a version of the hash of item on sheet. Copies holds the
// candidates, keyed by item hash. The versions of a hash are its entry in
// Sheet.Versions, and a hash without an entry has only itself as a
// version. A hash that the entry lists twice adds its copies one time.
func rivalsOf(sheet *rolls.Sheet, copies map[uint32][]*inventory.Item, item *inventory.Item) []*inventory.Item {
	versions, ok := sheet.Versions[item.Hash]
	if !ok {
		versions = []uint32{item.Hash}
	}

	var (
		seen   []uint32
		rivals []*inventory.Item
	)

	for _, hash := range versions {
		if slices.Contains(seen, hash) {
			continue
		}

		seen = append(seen, hash)

		for _, other := range copies[hash] {
			if other.InstanceID != item.InstanceID {
				rivals = append(rivals, other)
			}
		}
	}

	return rivals
}

// hitSet returns the index of each perk column of roll that the copy
// hits, in order.
func hitSet(roll *rolls.Roll) []int {
	var hits []int

	for i, column := range roll.Columns {
		if !column.Missed() {
			hits = append(hits, i)
		}
	}

	return hits
}

// verdictOf returns the kind of verdict of a copy against its rivals, as
// verdictOf of the web client gives it. Mine holds the indexes of the
// perk columns that the copy hits, and rivals holds those of each rival.
// The sets compare index by index, also when the columns of two copies
// come from different rows of the sheet.
//
// The first kind whose rule holds is the verdict:
//
//   - verdictOnly, when the copy has no rival.
//   - verdictWorse, when the hits of a rival are a strict superset of the
//     hits of the copy.
//   - verdictEqual, when a rival has the hits of the copy.
//   - verdictBetter, when the hits of each rival are a strict subset of
//     the hits of the copy.
//   - verdictMixed, in every other case.
func verdictOf(mine []int, rivals [][]int) string {
	if len(rivals) == 0 {
		return verdictOnly
	}

	relations := make([]string, 0, len(rivals))
	for _, theirs := range rivals {
		relations = append(relations, relateHits(mine, theirs))
	}

	switch {
	case slices.Contains(relations, verdictWorse):
		return verdictWorse
	case slices.Contains(relations, verdictEqual):
		return verdictEqual
	case !slices.ContainsFunc(relations, func(kind string) bool { return kind != verdictBetter }):
		return verdictBetter
	default:
		return verdictMixed
	}
}

// relateHits returns the kind of verdict of a copy that hits the perk
// columns of mine against one rival that hits those of theirs. A copy
// that gains no column and loses none is equal. A copy that gains a
// column and loses none is better, and one that loses a column and gains
// none is worse. A copy that gains a column and loses another is mixed.
func relateHits(mine, theirs []int) string {
	gained := slices.ContainsFunc(mine, func(i int) bool { return !slices.Contains(theirs, i) })
	lost := slices.ContainsFunc(theirs, func(i int) bool { return !slices.Contains(mine, i) })

	switch {
	case !gained && !lost:
		return verdictEqual
	case !lost:
		return verdictBetter
	case !gained:
		return verdictWorse
	default:
		return verdictMixed
	}
}
