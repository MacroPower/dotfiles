package main

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// compiled parses and compiles query, which must parse.
func compiled(t *testing.T, query string) compiledQuery {
	t.Helper()

	root, err := parseQuery(query)
	require.NoError(t, err)

	return compileQuery(root)
}

// matchIDs returns the instance id of each item that query matches in a
// search context whose items are items, in the order of items. Every term
// of query must be supported.
func matchIDs(t *testing.T, query string, items []*Item) []string {
	t.Helper()

	return contextMatchIDs(t, query, searchContext{items: items})
}

// contextMatchIDs returns the instance id of each item of ctx that query
// matches in ctx, in the order of the items. Every term of query must be
// supported.
func contextMatchIDs(t *testing.T, query string, ctx searchContext) []string {
	t.Helper()

	q := compiled(t, query)
	require.Empty(t, q.unsupported)
	require.Empty(t, q.invalid)
	require.NotNil(t, q.test)

	test := q.test(ctx)
	out := []string{}

	for _, item := range ctx.items {
		if test(item) {
			out = append(out, item.InstanceID)
		}
	}

	return out
}

func TestOwnedItems(t *testing.T) {
	t.Parallel()

	// The profile holds one item for each case. Hash 403 is an equippable
	// weapon whose definition names the bucket that holds dummy items.
	// Hash 999 has no definition.
	const profile = `{
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [
			{"itemHash": 100, "itemInstanceId": "1001", "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 403, "itemInstanceId": "2005", "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 999, "itemInstanceId": "2008", "quantity": 1, "location": 2, "bucketHash": 138197802},
			{"itemHash": 300, "quantity": 42, "location": 2, "bucketHash": 1469714392}
		]}},
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": 102, "itemInstanceId": "1003", "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 100, "quantity": 1, "location": 1, "bucketHash": 1498876634},
			{"itemHash": 100, "itemInstanceId": "1005", "quantity": 1, "location": 4, "bucketHash": 215593132},
			{"itemHash": 403, "itemInstanceId": "2006", "quantity": 1, "location": 4, "bucketHash": 215593132},
			{"itemHash": 400, "itemInstanceId": "2001", "quantity": 1, "location": 1, "bucketHash": 375726501},
			{"itemHash": 401, "itemInstanceId": "2002", "quantity": 1, "location": 1, "bucketHash": 1107761855},
			{"itemHash": 402, "itemInstanceId": "2003", "quantity": 1, "location": 1, "bucketHash": 3683254069},
			{"itemHash": 403, "itemInstanceId": "2004", "quantity": 1, "location": 1, "bucketHash": 3621873013},
			{"itemHash": 403, "itemInstanceId": "2007", "quantity": 1, "location": 1, "bucketHash": 2422292810}
		]}}},
		"characterEquipment": {"data": {"c1": {"items": [
			{"itemHash": 200, "itemInstanceId": "1004", "quantity": 1, "location": 1, "bucketHash": 3448274439}
		]}}}
	}`

	defs := fixtureDefs()
	defs.Items[400] = ItemDef{Name: "Exotic Engram", Bucket: bucketEngrams, ClassType: classAny}
	defs.Items[401] = ItemDef{Name: "Wave", Bucket: bucketEmotes, ClassType: classAny, Equippable: true}
	defs.Items[402] = ItemDef{Name: "Takedown", Bucket: bucketFinishers, ClassType: classAny, Equippable: true}
	defs.Items[403] = ItemDef{
		Name:       "Dummy",
		Bucket:     bucketForbidden,
		ItemType:   itemTypeWeapon,
		ClassType:  classAny,
		Equippable: true,
	}

	var p Profile

	require.NoError(t, json.Unmarshal([]byte(profile), &p))

	s := NewSnapshot(&p, defs, fixtureNow)
	built := builtItems(s)
	owned := ownedItems(s)

	tests := map[string]struct {
		id    string
		hash  uint32
		built bool
		owned bool
	}{
		"weapon on a character":              {hash: 102, id: "1003", built: true, owned: true},
		"weapon in the vault":                {hash: 100, id: "1001", built: true, owned: true},
		"weapon at the postmaster":           {hash: 100, id: "1005", built: true, owned: true},
		"armor piece":                        {hash: 200, id: "1004", built: true, owned: true},
		"lost weapon of the dummy bucket":    {hash: 403, id: "2006", built: true, owned: true},
		"stack":                              {hash: 300, built: true},
		"stack of an equippable definition":  {hash: 100, built: true},
		"instanced item without equipping":   {hash: 400, id: "2001", built: true},
		"item without a definition":          {hash: 999, id: "2008", built: true},
		"emote":                              {hash: 401, id: "2002", built: true},
		"finisher":                           {hash: 402, id: "2003", built: true},
		"held item in the forbidden bucket":  {hash: 403, id: "2007"},
		"held item in the other dummy":       {hash: 403, id: "2004"},
		"vault item of the forbidden bucket": {hash: 403, id: "2005"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			i := slices.IndexFunc(s.Items, func(item *Item) bool {
				return item.Hash == tc.hash && item.InstanceID == tc.id
			})
			require.GreaterOrEqual(t, i, 0)

			assert.Equal(t, tc.built, slices.Contains(built, s.Items[i]), "built")
			assert.Equal(t, tc.owned, slices.Contains(owned, s.Items[i]), "owned")
		})
	}
}

func TestCompileQueryTableA(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		items []*Item
		want  []string
	}{
		"is:weapon": {
			query: "is:weapon",
			items: []*Item{
				{InstanceID: "kinetic", Bucket: bucketKinetic},
				{InstanceID: "energy", Bucket: bucketEnergy},
				{InstanceID: "power", Bucket: bucketPower},
				{InstanceID: "helmet", Bucket: bucketHelmet},
				{InstanceID: "weapon kind", Bucket: bucketEngrams, Kind: kindWeapon},
			},
			want: []string{"kinetic", "energy", "power"},
		},
		"is:armor": {
			query: "is:armor",
			items: []*Item{
				{InstanceID: "helmet", Bucket: bucketHelmet},
				{InstanceID: "gauntlets", Bucket: bucketGauntlets},
				{InstanceID: "chest", Bucket: bucketChestArmor},
				{InstanceID: "legs", Bucket: bucketLegArmor},
				{InstanceID: "class", Bucket: bucketClassArmor},
				{InstanceID: "kinetic", Bucket: bucketKinetic},
				{InstanceID: "armor kind", Bucket: bucketEngrams, Kind: kindArmor},
			},
			want: []string{"helmet", "gauntlets", "chest", "legs", "class"},
		},
		"is:equipment": {
			query: "is:equipment",
			items: []*Item{
				{InstanceID: "equippable", Bucket: bucketKinetic, Equippable: true},
				{InstanceID: "not equippable", Bucket: bucketKinetic},
				{InstanceID: "emote", Bucket: bucketEmotes, Equippable: true},
				{InstanceID: "finisher", Bucket: bucketFinishers, Equippable: true},
			},
			want: []string{"equippable"},
		},
		"is:equippable": {
			query: "is:equippable",
			items: []*Item{
				{InstanceID: "equippable", Bucket: bucketKinetic, Equippable: true},
				{InstanceID: "not equippable", Bucket: bucketKinetic},
				{InstanceID: "emote", Bucket: bucketEmotes, Equippable: true},
			},
			want: []string{"equippable"},
		},
		"is:equipped": {
			query: "is:equipped",
			items: []*Item{
				{InstanceID: "equipped", Equipped: true},
				{InstanceID: "held"},
			},
			want: []string{"equipped"},
		},
		"is:invault": {
			query: "is:invault",
			items: []*Item{
				{InstanceID: "vault", Owner: ownerVault},
				{InstanceID: "account", Owner: ownerAccount},
				{InstanceID: "character", Owner: hunterID},
			},
			want: []string{"vault"},
		},
		"is:postmaster": {
			query: "is:postmaster",
			items: []*Item{
				{InstanceID: "lost", Bucket: bucketKinetic, Lost: true, Postmaster: true},
				{InstanceID: "engram", Bucket: bucketEngrams},
				{InstanceID: "message", Bucket: bucketMessages, Postmaster: true},
				{InstanceID: "special order", Bucket: bucketSpecialOrders},
				{InstanceID: "postmaster flag", Bucket: bucketKinetic, Postmaster: true},
				{InstanceID: "held", Bucket: bucketKinetic},
			},
			want: []string{"lost", "engram", "message", "special order"},
		},
		"is:inpostmaster": {
			query: "is:inpostmaster",
			items: []*Item{
				{InstanceID: "lost", Bucket: bucketKinetic, Lost: true, Postmaster: true},
				{InstanceID: "engram", Bucket: bucketEngrams},
				{InstanceID: "held", Bucket: bucketKinetic},
			},
			want: []string{"lost", "engram"},
		},
		"is:locked": {
			query: "is:locked",
			items: []*Item{
				{InstanceID: "locked", Lockable: true, Locked: true},
				{InstanceID: "unlocked", Lockable: true},
				{InstanceID: "locked without lock", Locked: true},
				{InstanceID: "no lock"},
			},
			want: []string{"locked"},
		},
		"is:unlocked": {
			query: "is:unlocked",
			items: []*Item{
				{InstanceID: "locked", Lockable: true, Locked: true},
				{InstanceID: "unlocked", Lockable: true},
				{InstanceID: "locked without lock", Locked: true},
				{InstanceID: "no lock"},
			},
			want: []string{"unlocked"},
		},
		"is:titan": {
			query: "is:titan",
			items: []*Item{
				{InstanceID: "titan", ClassType: classTitan},
				{InstanceID: "hunter", ClassType: classHunter},
				{InstanceID: "warlock", ClassType: classWarlock},
				{InstanceID: "any", ClassType: classAny},
			},
			want: []string{"titan"},
		},
		"is:hunter": {
			query: "is:hunter",
			items: []*Item{
				{InstanceID: "titan", ClassType: classTitan},
				{InstanceID: "hunter", ClassType: classHunter},
				{InstanceID: "any", ClassType: classAny},
			},
			want: []string{"hunter"},
		},
		"is:warlock": {
			query: "is:warlock",
			items: []*Item{
				{InstanceID: "hunter", ClassType: classHunter},
				{InstanceID: "warlock", ClassType: classWarlock},
				{InstanceID: "any", ClassType: classAny},
			},
			want: []string{"warlock"},
		},
		"is:favorite": {
			query: "is:favorite",
			items: []*Item{
				{InstanceID: "favorite", Favorite: true},
				{InstanceID: "plain"},
			},
			want: []string{"favorite"},
		},
		"tag:keep": {
			query: "tag:keep",
			items: []*Item{
				{InstanceID: "keep", Tag: "keep"},
				{InstanceID: "junk", Tag: tagJunk},
				{InstanceID: "untagged"},
			},
			want: []string{"keep"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, tc.items))
		})
	}
}

func TestCompileQueryTag(t *testing.T) {
	t.Parallel()

	custom := primaryTag([]string{"custom"})
	require.Equal(t, "custom", custom)

	items := []*Item{
		{InstanceID: "keep", Tag: "keep"},
		{InstanceID: "junk", Tag: tagJunk},
		{InstanceID: "untagged"},
		{InstanceID: "favorite", Favorite: true},
		{InstanceID: "custom", Tag: custom},
	}

	tests := map[string]struct {
		query string
		want  []string
	}{
		"a tag":                   {query: "tag:keep", want: []string{"keep"}},
		"a tag in another case":   {query: "tag:JUNK", want: []string{"junk"}},
		"none is not special":     {query: "tag:none", want: []string{}},
		"any is not special":      {query: "tag:any", want: []string{}},
		"favorite reads the mark": {query: "tag:favorite", want: []string{"favorite"}},
		"a tag outside the order": {query: "tag:custom", want: []string{}},
		"an empty quoted value":   {query: `tag:""`, want: []string{}},
		"a negated tag":           {query: "-tag:keep", want: []string{"junk", "untagged", "favorite", "custom"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestCompileQueryNot(t *testing.T) {
	t.Parallel()

	items := []*Item{
		{InstanceID: "equipped", Equipped: true},
		{InstanceID: "held"},
	}

	tests := map[string]struct {
		query string
		want  []string
	}{
		"filter named not": {query: "not:equipped", want: []string{"held"}},
		"minus":            {query: "-is:equipped", want: []string{"held"}},
		"not operator":     {query: "not is:equipped", want: []string{"held"}},
		"double negation":  {query: "-not:equipped", want: []string{"equipped"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestCompileQueryOperators(t *testing.T) {
	t.Parallel()

	items := []*Item{
		{InstanceID: "locked weapon", Bucket: bucketKinetic, Lockable: true, Locked: true},
		{InstanceID: "weapon", Bucket: bucketKinetic, Lockable: true},
		{InstanceID: "locked helmet", Bucket: bucketHelmet, Lockable: true, Locked: true},
		{InstanceID: "engram", Bucket: bucketEngrams},
	}

	tests := map[string]struct {
		query string
		want  []string
	}{
		"implicit and": {query: "is:weapon is:locked", want: []string{"locked weapon"}},
		"and":          {query: "is:weapon and is:locked", want: []string{"locked weapon"}},
		"or":           {query: "is:weapon or is:armor", want: []string{"locked weapon", "weapon", "locked helmet"}},
		"group": {
			query: "(is:weapon or is:armor) -is:unlocked",
			want:  []string{"locked weapon", "locked helmet"},
		},
		"or binds tighter than implicit and": {
			query: "is:locked is:weapon or is:armor",
			want:  []string{"locked weapon", "locked helmet"},
		},
		"comment": {query: "/* gear */ is:armor", want: []string{"locked helmet"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestCompileQueryUnsupported(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []string
	}{
		"is: word outside the table":    {query: "is:masterwork", want: []string{"is:masterwork"}},
		"filter outside the table":      {query: "note:junk", want: []string{"note:junk"}},
		"bare word":                     {query: "fatebringer", want: []string{"fatebringer"}},
		"quoted string":                 {query: `"fatebringer"`, want: []string{`"fatebringer"`}},
		"perk filter":                   {query: "perk:outlaw", want: []string{"perk:outlaw"}},
		"one unsupported operand":       {query: "is:weapon or is:masterwork", want: []string{"is:masterwork"}},
		"negated is: word":              {query: "not:masterwork", want: []string{"not:masterwork"}},
		"prototype key constructor":     {query: "is:constructor", want: []string{"is:constructor"}},
		"prototype key __proto__":       {query: "is:__proto__", want: []string{"is:__proto__"}},
		"is: word that names a filter":  {query: "is:tag", want: []string{"is:tag"}},
		"filter that names an is: word": {query: "weapon:x", want: []string{"weapon:x"}},
		"a repeated term": {
			query: "is:masterwork (is:weapon or -is:masterwork)",
			want:  []string{"is:masterwork"},
		},
		"two terms in query order": {
			query: "perk:outlaw is:weapon or is:masterwork",
			want:  []string{"perk:outlaw", "is:masterwork"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			q := compiled(t, tc.query)
			assert.Equal(t, tc.want, q.unsupported)
			assert.Empty(t, q.invalid)
			assert.Nil(t, q.test)
		})
	}
}

func TestSupportedKeywords(t *testing.T) {
	t.Parallel()

	want := []string{
		"is:weapon", "is:armor", "is:equipment", "is:equippable", "is:equipped", "is:invault",
		"is:postmaster", "is:inpostmaster", "is:locked", "is:unlocked", "is:titan", "is:hunter",
		"is:warlock", "is:favorite", "is:autorifle", "is:handcannon", "is:pulserifle", "is:scoutrifle",
		"is:fusionrifle", "is:sniperrifle", "is:shotgun", "is:machinegun", "is:lmg", "is:rocketlauncher",
		"is:sidearm", "is:sword", "is:grenadelauncher", "is:heavygrenadelauncher",
		"is:specialgrenadelauncher", "is:tracerifle", "is:linearfusionrifle", "is:lfr", "is:submachine",
		"is:smg", "is:bow", "is:glaive", "is:transmat", "is:weaponmod", "is:armormod", "is:reptoken",
		"is:primary", "is:special", "is:heavy", "is:dupe", "is:incurrentchar", "is:currentclass", "tag:",
		"dupe:item", "count:", "id:", "hash:", "name:", "exactname:",
	}
	assert.Equal(t, want, supportedKeywords())

	err := unsupportedQuery([]string{"is:masterwork", "perk:outlaw"})
	require.ErrorIs(t, err, ErrUnsupportedQuery)
	assert.Equal(t, "unsupported query: the server does not evaluate is:masterwork, perk:outlaw, "+
		"so it previews nothing. The web client might still accept these terms. "+
		"The server evaluates is:weapon, is:armor, is:equipment, is:equippable, is:equipped, is:invault, "+
		"is:postmaster, is:inpostmaster, is:locked, is:unlocked, is:titan, is:hunter, is:warlock, "+
		"is:favorite, is:autorifle, is:handcannon, is:pulserifle, is:scoutrifle, is:fusionrifle, "+
		"is:sniperrifle, is:shotgun, is:machinegun, is:lmg, is:rocketlauncher, is:sidearm, is:sword, "+
		"is:grenadelauncher, is:heavygrenadelauncher, is:specialgrenadelauncher, is:tracerifle, "+
		"is:linearfusionrifle, is:lfr, is:submachine, is:smg, is:bow, is:glaive, is:transmat, "+
		"is:weaponmod, is:armormod, is:reptoken, is:primary, is:special, is:heavy, is:dupe, "+
		"is:incurrentchar, is:currentclass, tag:, dupe:item, count:, id:, hash:, name:, "+
		"and exactname:. Each is: word also works as not:<word>.", err.Error())
}

func TestListWithAnd(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		words []string
		want  string
	}{
		"none":  {want: ""},
		"one":   {words: []string{"a"}, want: "a"},
		"two":   {words: []string{"a", "b"}, want: "a and b"},
		"three": {words: []string{"a", "b", "c"}, want: "a, b, and c"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, listWithAnd(tc.words))
		})
	}
}

func TestCompiledQueryMatches(t *testing.T) {
	t.Parallel()

	items := []*Item{
		{InstanceID: "20", Name: "Gjallarhorn", Bucket: bucketPower},
		{InstanceID: "100", Name: "Fatebringer", Bucket: bucketKinetic},
		{InstanceID: "9", Name: "Fatebringer", Bucket: bucketKinetic},
		{InstanceID: "5", Name: "Celestial Nighthawk", Bucket: bucketHelmet},
	}

	got := compiled(t, "is:weapon").matches(searchContext{items: items}, items)
	assert.Equal(t, []string{"9", "100", "20"}, ids(got))
}

func TestRangeComparator(t *testing.T) {
	t.Parallel()

	overloads := rangeFormat{overloads: map[string]float64{"worthy": 10, "arrivals": 11}}

	tests := map[string]struct {
		format rangeFormat
		value  string
		number float64
		want   bool
	}{
		"<10 of 8":                {value: "<10", number: 8, want: true},
		"<10 of 10":               {value: "<10", number: 10},
		">10 of 10":               {value: ">10", number: 10},
		">10 of 15":               {value: ">10", number: 15, want: true},
		"<=10 of 10":              {value: "<=10", number: 10, want: true},
		">=10 of 10":              {value: ">=10", number: 10, want: true},
		"<=10 of 8":               {value: "<=10", number: 8, want: true},
		">=10 of 15":              {value: ">=10", number: 15, want: true},
		"<=10 of 15":              {value: "<=10", number: 15},
		">=10 of 8":               {value: ">=10", number: 8},
		"=10 of 10":               {value: "=10", number: 10, want: true},
		"=10 of 15":               {value: "=10", number: 15},
		"10 of 10":                {value: "10", number: 10, want: true},
		"overloads <worthy of 8":  {format: overloads, value: "<worthy", number: 8, want: true},
		"overloads <worthy of 10": {format: overloads, value: "<worthy", number: 10},
		"overloads >10 of 10":     {format: overloads, value: ">10", number: 10},
		"overloads >10 of 15":     {format: overloads, value: ">10", number: 15, want: true},
		"overloads <=worthy of 10": {
			format: overloads, value: "<=worthy", number: 10, want: true,
		},
		"overloads >=worthy of 10": {
			format: overloads, value: ">=worthy", number: 10, want: true,
		},
		"overloads =worthy of 10":  {format: overloads, value: "=worthy", number: 10, want: true},
		"overloads =worthy of 15":  {format: overloads, value: "=worthy", number: 15},
		"overloads arrivals of 11": {format: overloads, value: "arrivals", number: 11, want: true},
		"overloads arrivals of 10": {format: overloads, value: "arrivals", number: 10},
		"a decimal number":         {value: "2.5", number: 2.5, want: true},
		"a decimal number of 2":    {value: "2.5", number: 2},
		"a number past a float64": {
			value: "<9" + strings.Repeat("0", 400), number: math.MaxFloat64, want: true,
		},
		"a word outside overloads":  {format: overloads, value: "<12", number: 11, want: true},
		"a number among overloads":  {format: overloads, value: "11", number: 11, want: true},
		"an operator before a word": {format: overloads, value: ">arrivals", number: 12, want: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			compare, err := tc.format.comparator(tc.value)
			require.NoError(t, err)
			assert.Equal(t, tc.want, compare(tc.number))
		})
	}
}

func TestRangeComparatorInvalid(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value string
		valid bool
	}{
		"operators in the wrong order": {value: "=<2"},
		"less or greater":              {value: "<>2"},
		"double equals":                {value: "==2"},
		"three operators":              {value: "<<=2"},
		"a word":                       {value: "abc"},
		"an empty value":               {value: ""},
		"an operator alone":            {value: ">="},
		"a trailing dot":               {value: "2."},
		"a leading dot":                {value: ".5"},
		"constructor without a table":  {value: "constructor"},
		"__proto__ without a table":    {value: "__proto__"},
		"a decimal number":             {value: "2.5", valid: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			compare, err := rangeFormat{}.comparator(tc.value)
			if tc.valid {
				require.NoError(t, err)
				assert.NotNil(t, compare)

				return
			}

			require.ErrorIs(t, err, errNotRange)
			assert.Nil(t, compare)
		})
	}
}

func TestRangeComparatorPrototypeKeys(t *testing.T) {
	t.Parallel()

	format := rangeFormat{overloads: map[string]float64{"worthy": 10, "arrivals": 11}}

	tests := map[string]struct {
		value string
		valid bool
	}{
		"constructor":                      {value: "constructor", valid: true},
		"constructor after an operator":    {value: ">=constructor", valid: true},
		"__proto__":                        {value: "__proto__", valid: true},
		"__proto__ after an operator":      {value: "<__proto__", valid: true},
		"constructor after a bad operator": {value: "=<constructor"},
		"toString in lowercase":            {value: "tostring"},
		"a word that starts with a key":    {value: "constructors"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			compare, err := format.comparator(tc.value)
			if !tc.valid {
				require.ErrorIs(t, err, errNotRange)

				return
			}

			require.NoError(t, err)

			for n := range 21 {
				assert.False(t, compare(float64(n)), n)
			}
		})
	}
}

// comparableWeapon returns a legendary kinetic weapon of every class that
// DIM marks as comparable.
func comparableWeapon(id, name string) *Item {
	return &Item{
		InstanceID: id,
		Name:       name,
		Tier:       "Legendary",
		ClassType:  classAny,
		Bucket:     bucketKinetic,
		Equippable: true,
		Lockable:   true,
	}
}

func TestItemDupeID(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		item *Item
		want string
	}{
		"weapon": {
			item: comparableWeapon("1", "Fatebringer"),
			want: "Fatebringer3Legendary1498876634",
		},
		"adept weapon keeps its suffix": {
			item: comparableWeapon("2", "Fatebringer (Adept)"),
			want: "Fatebringer (Adept)3Legendary1498876634",
		},
		"translation key of the adept suffix": {
			item: comparableWeapon("3", "Filter.Adept Fatebringer"),
			want: "Fatebringer3Legendary1498876634",
		},
		"translation keys in another case": {
			item: comparableWeapon("4", "FILTER-TIMELOST Fatebringer filter.harrowed"),
			want: "Fatebringer3Legendary1498876634",
		},
		"every match of a key": {
			item: comparableWeapon("5", "Filter.AdeptFate Filter.Adeptbringer"),
			want: "Fate bringer3Legendary1498876634",
		},
		"line terminator in a key": {
			item: comparableWeapon("6", "Filter\u2028Adept Fatebringer"),
			want: "Filter\u2028Adept Fatebringer3Legendary1498876634",
		},
		"character outside the Basic Multilingual Plane in a key": {
			item: comparableWeapon("7", "Filter\U0001F600Adept Fatebringer"),
			want: "Filter\U0001F600Adept Fatebringer3Legendary1498876634",
		},
		"long s in a key": {
			item: comparableWeapon("8", "Filter.Timelo\u017ft Fatebringer"),
			want: "Filter.Timelo\u017ft Fatebringer3Legendary1498876634",
		},
		"weapon name trimmed of JavaScript whitespace": {
			item: comparableWeapon("9", "\ufeffFatebringer\u3000"),
			want: "Fatebringer3Legendary1498876634",
		},
		"armor keeps its spaces": {
			item: &Item{Name: "Helm ", Tier: "Exotic", ClassType: classHunter, Bucket: bucketHelmet},
			want: "Helm 1Exotic3448274439",
		},
		"armor keeps a translation key": {
			item: &Item{Name: "Filter.Adept Helm", Tier: "Exotic", ClassType: classTitan, Bucket: bucketHelmet},
			want: "Filter.Adept Helm0Exotic3448274439",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, itemDupeID(tc.item))
		})
	}

	assert.Equal(t, itemDupeID(comparableWeapon("1", "Fatebringer")), itemDupeID(comparableWeapon("2", "Fatebringer")))
	assert.NotEqual(
		t,
		itemDupeID(comparableWeapon("1", "Fatebringer")),
		itemDupeID(comparableWeapon("2", "Fatebringer (Adept)")),
	)
}

func TestIsDupe(t *testing.T) {
	t.Parallel()

	// pair returns two comparable copies of a weapon, after edit changes
	// each copy.
	pair := func(name string, edit func(*Item)) []*Item {
		items := []*Item{comparableWeapon(name+" 1", name), comparableWeapon(name+" 2", name)}
		for _, item := range items {
			edit(item)
		}

		return items
	}

	tests := map[string]struct {
		items []*Item
		want  []string
	}{
		"pair": {
			items: pair("Fatebringer", func(*Item) {}),
			want:  []string{"Fatebringer 1", "Fatebringer 2"},
		},
		"adept copy and base weapon": {
			items: []*Item{comparableWeapon("adept", "Fatebringer (Adept)"), comparableWeapon("base", "Fatebringer")},
			want:  []string{},
		},
		"single copy": {
			items: []*Item{comparableWeapon("single", "Fatebringer")},
			want:  []string{},
		},
		"pair of emblems": {
			items: pair("Emblem", func(item *Item) { item.Bucket = bucketEmblems }),
			want:  []string{},
		},
		"pair in the Artifacts bucket": {
			items: pair("Artifact", func(item *Item) { item.Bucket = bucketArtifacts }),
			want:  []string{},
		},
		"pair of default shaders": {
			items: pair("Default Shader", func(item *Item) { item.Hash = hashDefaultShader }),
			want:  []string{},
		},
		"pair without a lock": {
			items: pair("Fatebringer", func(item *Item) { item.Lockable = false }),
			want:  []string{},
		},
		"pair of emotes": {
			items: pair("Wave", func(item *Item) { item.Bucket = bucketEmotes }),
			want:  []string{},
		},
		"same name in two classes": {
			items: []*Item{comparableWeapon("titan", "Helm"), {
				InstanceID: "hunter", Name: "Helm", Tier: "Legendary", ClassType: classHunter,
				Bucket: bucketKinetic, Equippable: true, Lockable: true,
			}},
			want: []string{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, "is:dupe", tc.items))
		})
	}
}

func TestIsDupeGroupsEveryItem(t *testing.T) {
	t.Parallel()

	unlockable := comparableWeapon("unlockable", "Fatebringer")
	unlockable.Lockable = false

	items := []*Item{comparableWeapon("comparable", "Fatebringer"), unlockable}

	assert.Equal(t, []string{"comparable"}, matchIDs(t, "is:dupe", items))
	assert.Equal(t, []string{"unlockable"}, matchIDs(t, "not:dupe", items))
}

func TestCount(t *testing.T) {
	t.Parallel()

	unlockable := comparableWeapon("unlockable", "Midnight Coup")
	unlockable.Lockable = false

	emblem := comparableWeapon("emblem", "Emblem")
	emblem.Bucket = bucketEmblems

	items := []*Item{
		comparableWeapon("copy 1", "Midnight Coup"),
		comparableWeapon("copy 2", "Midnight Coup"),
		unlockable,
		comparableWeapon("adept", "Fatebringer (Adept)"),
		comparableWeapon("base", "Fatebringer"),
		emblem,
	}

	tests := map[string]struct {
		query string
		want  []string
	}{
		"at least two":    {query: "count:>=2", want: []string{"copy 1", "copy 2", "unlockable"}},
		"exactly two":     {query: "count:=2", want: []string{"copy 1", "copy 2", "unlockable"}},
		"one":             {query: "count:1", want: []string{"adept", "base"}},
		"zero":            {query: "count:0", want: []string{"emblem"}},
		"fewer than two":  {query: "count:<2", want: []string{"adept", "base", "emblem"}},
		"decimal number":  {query: "count:2.5", want: []string{}},
		"negated":         {query: "-count:>1", want: []string{"adept", "base", "emblem"}},
		"with is:dupe":    {query: "count:2 is:dupe", want: []string{"copy 1", "copy 2"}},
		"or another term": {query: "count:0 or is:dupe", want: []string{"copy 1", "copy 2", "emblem"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestDupeValues(t *testing.T) {
	t.Parallel()

	items := []*Item{
		comparableWeapon("copy 1", "Midnight Coup"),
		comparableWeapon("copy 2", "Midnight Coup"),
		comparableWeapon("single", "Fatebringer"),
	}

	assert.Equal(t, matchIDs(t, "is:dupe", items), matchIDs(t, "dupe:item", items))
	assert.Equal(t, []string{"copy 1", "copy 2"}, matchIDs(t, "dupe:item", items))

	tests := map[string]struct {
		query       string
		unsupported []string
		invalid     []string
	}{
		"another key":             {query: "dupe:stats", unsupported: []string{"dupe:stats"}},
		"the last key":            {query: "dupe:customstatlower", unsupported: []string{"dupe:customstatlower"}},
		"two keys":                {query: "dupe:item+stats", unsupported: []string{"dupe:item+stats"}},
		"a repeated key":          {query: "dupe:item+item", unsupported: []string{"dupe:item+item"}},
		"an unknown value":        {query: "dupe:foo", invalid: []string{"dupe:foo"}},
		"a key and an unknown":    {query: "dupe:item+foo", invalid: []string{"dupe:item+foo"}},
		"the is: word as a value": {query: "dupe:dupe", invalid: []string{"dupe:dupe"}},
		"an empty value":          {query: `dupe:""`, invalid: []string{`dupe:""`}},
		"a trailing plus":         {query: "dupe:item+", invalid: []string{"dupe:item+"}},
		"a prefix of a key":       {query: "is:weapon dupe:ite", invalid: []string{"dupe:ite"}},
		"a key and a prefix":      {query: "dupe:stats+ite", invalid: []string{"dupe:stats+ite"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			q := compiled(t, tc.query)
			assert.Equal(t, tc.unsupported, q.unsupported)
			assert.Equal(t, tc.invalid, q.invalid)
			assert.Nil(t, q.test)
		})
	}
}

func TestHashAndID(t *testing.T) {
	t.Parallel()

	items := []*Item{
		{InstanceID: "1001", Hash: 1234},
		{InstanceID: "1002", Hash: 1234},
		{InstanceID: "1003", Hash: 99},
		{InstanceID: "1004"},
	}

	tests := map[string]struct {
		query string
		want  []string
	}{
		"hash":                          {query: "hash:1234", want: []string{"1001", "1002"}},
		"hash before letters":           {query: "hash:1234abc", want: []string{"1001", "1002"}},
		"hash with a leading space":     {query: `hash:" 1234"`, want: []string{"1001", "1002"}},
		"hash with a leading tab":       {query: "hash:\"\t1234\"", want: []string{"1001", "1002"}},
		"hash with leading zeros":       {query: "hash:001234", want: []string{"1001", "1002"}},
		"hash with a plus sign":         {query: "hash:+1234", want: []string{"1001", "1002"}},
		"hash with a decimal part":      {query: "hash:1234.9", want: []string{"1001", "1002"}},
		"hash without digits":           {query: "hash:abc", want: []string{}},
		"negative hash":                 {query: "hash:-1234", want: []string{}},
		"negative zero":                 {query: "hash:-0", want: []string{"1004"}},
		"hash past a uint32":            {query: "hash:4294968530", want: []string{}},
		"hash with a space after sign":  {query: `hash:"+ 1234"`, want: []string{}},
		"hash in hexadecimal":           {query: "hash:0x4d2", want: []string{"1004"}},
		"hash with a space after digit": {query: `hash:"99 1234"`, want: []string{"1003"}},
		"id":                            {query: "id:1003", want: []string{"1003"}},
		"id with letters":               {query: "id:1003abc", want: []string{}},
		"id that names a hash":          {query: "id:1234", want: []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestCurrentCharacter(t *testing.T) {
	t.Parallel()

	ctx := searchContext{
		current: Character{ID: hunterID, ClassType: classHunter},
		items: []*Item{
			{InstanceID: "hunter armor", Owner: hunterID, ClassType: classHunter},
			{InstanceID: "hunter weapon", Owner: hunterID, ClassType: classAny},
			{InstanceID: "account", Owner: ownerAccount, ClassType: classAny},
			{InstanceID: "vault armor", Owner: ownerVault, ClassType: classHunter},
			{InstanceID: "vault weapon", Owner: ownerVault, ClassType: classAny},
			{InstanceID: "titan armor", Owner: titanID, ClassType: classTitan},
			{InstanceID: "titan holds hunter armor", Owner: titanID, ClassType: classHunter},
		},
	}

	tests := map[string]struct {
		query string
		want  []string
	}{
		"is:incurrentchar": {
			query: "is:incurrentchar",
			want:  []string{"hunter armor", "hunter weapon", "account"},
		},
		"is:currentclass": {
			query: "is:currentclass",
			want:  []string{"hunter armor", "vault armor", "titan holds hunter armor"},
		},
		"not:incurrentchar": {
			query: "not:incurrentchar",
			want:  []string{"vault armor", "vault weapon", "titan armor", "titan holds hunter armor"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, contextMatchIDs(t, tc.query, ctx))
		})
	}
}

func TestCompileQueryInvalidFirst(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []string
	}{
		"invalid and unsupported": {query: "count:=<2 is:masterwork", want: []string{"count:=<2"}},
		"unsupported first":       {query: "is:masterwork or count:<>2", want: []string{"count:<>2"}},
		"negated":                 {query: "-dupe:foo is:weapon", want: []string{"dupe:foo"}},
		"two terms in query order": {
			query: "dupe:foo perk:outlaw (count:==2 or dupe:foo)",
			want:  []string{"dupe:foo", "count:==2"},
		},
		"an unsupported dupe: value": {query: "dupe:stats count:abc", want: []string{"count:abc"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			q := compiled(t, tc.query)
			assert.Equal(t, tc.want, q.invalid)
			assert.Empty(t, q.unsupported)
			assert.Nil(t, q.test)
		})
	}

	err := invalidQuery([]string{"count:=<2", "dupe:foo"})
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Equal(t, "invalid input: the web client rejects count:=<2, dupe:foo, "+
		"so it skips an autotag rule with this query", err.Error())
}

// namedItems returns one item for each of names, with the name as its
// instance id too.
func namedItems(names ...string) []*Item {
	items := make([]*Item, 0, len(names))
	for _, name := range names {
		items = append(items, &Item{InstanceID: name, Name: name})
	}

	return items
}

// nameItems returns the items of the name filter tests, which port the
// name tests of the web client.
func nameItems() []*Item {
	return namedItems("Gnawing Hunger", "Hunger of Crota", "Hard Light", "D\u00e9j\u00e0 Vu", "Fatebringer")
}

func TestNameStartOfWord(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []string
	}{
		"start of a later word": {query: "name:hunger", want: []string{"Gnawing Hunger", "Hunger of Crota"}},
		"inside a word":         {query: "name:unger", want: []string{}},
		"last word":             {query: "name:crota", want: []string{"Hunger of Crota"}},
		"start of a word only":  {query: "name:fate", want: []string{"Fatebringer"}},
		"uppercase value":       {query: "name:HUNGER", want: []string{"Gnawing Hunger", "Hunger of Crota"}},
		"negated": {
			query: "-name:hunger",
			want:  []string{"Hard Light", "D\u00e9j\u00e0 Vu", "Fatebringer"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, nameItems()))
		})
	}
}

func TestNameQuoted(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query string
		want  []string
	}{
		"two words":          {query: `name:"hard light"`, want: []string{"Hard Light"}},
		"second word":        {query: `name:"light"`, want: []string{"Hard Light"}},
		"single quotes":      {query: `name:'hard light'`, want: []string{"Hard Light"}},
		"words out of order": {query: `name:"light hard"`, want: []string{}},
		"empty value": {
			query: `name:""`,
			want:  []string{"Gnawing Hunger", "Hunger of Crota", "Hard Light", "D\u00e9j\u00e0 Vu", "Fatebringer"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, nameItems()))
		})
	}
}

func TestNameDiacritics(t *testing.T) {
	t.Parallel()

	items := append(nameItems(), &Item{InstanceID: "decomposed", Name: "De\u0301ja\u0300 Vu"})

	tests := map[string]struct {
		query string
		want  []string
	}{
		"plain value": {query: "name:deja", want: []string{"D\u00e9j\u00e0 Vu", "decomposed"}},
		"value with accents": {
			query: "name:d\u00e9j\u00e0",
			want:  []string{"D\u00e9j\u00e0 Vu", "decomposed"},
		},
		"uppercase accents": {
			query: "name:D\u00c9J\u00c0",
			want:  []string{"D\u00e9j\u00e0 Vu", "decomposed"},
		},
		"decomposed value": {
			query: "name:de\u0301ja\u0300",
			want:  []string{"D\u00e9j\u00e0 Vu", "decomposed"},
		},
		"boundary after folding":       {query: "name:\u00fcnger", want: []string{}},
		"accented start inside a word": {query: "name:\u00e9ja", want: []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestNameEscapes(t *testing.T) {
	t.Parallel()

	items := namedItems("a.b test", "axb test", "Gnawing Hunger (Adept)", "Gnawing Hunger")

	tests := map[string]struct {
		query string
		want  []string
	}{
		"dot":         {query: "name:a.b", want: []string{"a.b test"}},
		"parentheses": {query: `name:"(adept)"`, want: []string{"Gnawing Hunger (Adept)"}},
		"star":        {query: "name:a*", want: []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestNameNoBoundary(t *testing.T) {
	t.Parallel()

	items := namedItems("a-x", "-x", "ax", "a x", "\u00b5Tool")

	tests := map[string]struct {
		query string
		want  []string
	}{
		"value without a word start": {query: "name:-x", want: []string{"a-x", "-x"}},
		"value with a word start":    {query: "name:x", want: []string{"a-x", "-x", "a x"}},
		"case fold":                  {query: "name:\u03bctool", want: []string{"\u00b5Tool"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestNameQuotes(t *testing.T) {
	t.Parallel()

	items := namedItems("Gjallarhorn\u2019s Wrath", "Gjallarhorn's Echo", "Gjallarhorn\u201bs Ghost")

	tests := map[string]struct {
		query string
		want  []string
	}{
		"straight apostrophe": {
			query: `name:"gjallarhorn's"`,
			want:  []string{"Gjallarhorn\u2019s Wrath", "Gjallarhorn's Echo"},
		},
		"typographic apostrophe": {
			query: "name:gjallarhorn\u2018s",
			want:  []string{"Gjallarhorn\u2019s Wrath", "Gjallarhorn's Echo"},
		},
		"exact name": {
			query: `exactname:"gjallarhorn's wrath"`,
			want:  []string{"Gjallarhorn\u2019s Wrath"},
		},
		"reversed quote stays": {
			query: "name:gjallarhorn\u201bs",
			want:  []string{"Gjallarhorn\u201bs Ghost"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestExactName(t *testing.T) {
	t.Parallel()

	items := append(nameItems(), &Item{InstanceID: "adept", Name: "Gnawing Hunger (Adept)"})

	tests := map[string]struct {
		query string
		want  []string
	}{
		"whole name":         {query: `exactname:"gnawing hunger"`, want: []string{"Gnawing Hunger"}},
		"uppercase value":    {query: `exactname:"GNAWING HUNGER"`, want: []string{"Gnawing Hunger"}},
		"name with a suffix": {query: `exactname:"gnawing hunger (adept)"`, want: []string{"adept"}},
		"diacritics":         {query: `exactname:"deja vu"`, want: []string{"D\u00e9j\u00e0 Vu"}},
		"value with accents": {query: "exactname:\"d\u00e9j\u00e0 vu\"", want: []string{"D\u00e9j\u00e0 Vu"}},
		"one word":           {query: "exactname:hunger", want: []string{}},
		"bare word":          {query: "exactname:fatebringer", want: []string{"Fatebringer"}},
		"empty value":        {query: `exactname:""`, want: []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, tc.query, items))
		})
	}
}

func TestPlainString(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		text string
		want string
	}{
		"plain":                   {text: "Fatebringer", want: "fatebringer"},
		"precomposed diacritics":  {text: "D\u00e9j\u00e0 Vu", want: "deja vu"},
		"combining diacritics":    {text: "De\u0301ja\u0300 Vu", want: "deja vu"},
		"capital I with dot":      {text: "\u0130stanbul", want: "istanbul"},
		"letter without a decomp": {text: "\u00d8rsted", want: "\u00f8rsted"},
		"typographic quote stays": {text: "It\u2019s", want: "it\u2019s"},
		"spacing diacritics":      {text: "x^y`z", want: "xyz"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, plainString(tc.text))
		})
	}
}

// weaponTypeItems returns the owned items of a vault that holds ten weapons
// and a helmet. The items pass through [NewSnapshot], so their item
// categories carry the changes of [itemCategories].
func weaponTypeItems(t *testing.T) []*Item {
	t.Helper()

	defs := fixtureDefs()

	item := func(name, typeName string, bucket uint32, ammoType int, categories ...int64) ItemDef {
		def := ItemDef{
			Name:       name,
			TypeName:   typeName,
			Tier:       "Legendary",
			Bucket:     bucket,
			ItemType:   itemTypeWeapon,
			ClassType:  classAny,
			AmmoType:   ammoType,
			Equippable: true,
		}

		for _, category := range categories {
			def.Categories = append(def.Categories, uint32(category))
		}

		return def
	}

	// Each literal category is a hash of DIM that no search filter reads.
	// KineticWeapon is 2, EnergyWeapon is 3, Armor is 20, and Helmets is
	// 45.
	defs.Items[500] = item("Fatebringer", "Hand Cannon", bucketKinetic, ammoPrimary,
		categoryWeapon, 2, categoryHandCannon)
	defs.Items[501] = item("Ace of Spades", "Hand Cannon", bucketKinetic, ammoPrimary,
		categoryWeapon, 2, categoryHandCannon)
	defs.Items[502] = item("Beloved", "Sniper Rifle", bucketEnergy, ammoSpecial,
		categoryWeapon, 3, categorySniperRifle)
	defs.Items[503] = item("Matador 64", "Shotgun", bucketKinetic, ammoSpecial,
		categoryWeapon, 2, categoryShotgun)
	defs.Items[504] = item("Plug One.1", "Fusion Rifle", bucketEnergy, ammoSpecial,
		categoryWeapon, 3, categoryFusionRifle)
	defs.Items[505] = item("Sleeper Simulant", "Linear Fusion Rifle", bucketPower, ammoHeavy,
		categoryWeapon, categoryPowerWeapon, categoryFusionRifle, categoryLinearFusionRifles)
	defs.Items[506] = item("Thunderlord", "Machine Gun", bucketPower, ammoHeavy,
		categoryWeapon, categoryPowerWeapon, categoryMachineGun)
	defs.Items[507] = item("Riskrunner", "Submachine Gun", bucketEnergy, ammoPrimary,
		categoryWeapon, 3, categorySubmachineGuns)
	defs.Items[508] = item("Wendigo GL3", "Grenade Launcher", bucketPower, ammoHeavy,
		categoryWeapon, categoryPowerWeapon, categoryGrenadeLaunchers)
	defs.Items[509] = item("Forbearance", "Grenade Launcher", bucketEnergy, ammoSpecial,
		categoryWeapon, 3, categoryGrenadeLaunchers)
	defs.Items[510] = ItemDef{
		Name:       "Celestial Nighthawk",
		TypeName:   "Helmet",
		Tier:       "Exotic",
		Bucket:     bucketHelmet,
		ItemType:   itemTypeArmor,
		ClassType:  classHunter,
		Categories: []uint32{20, 45},
		Equippable: true,
	}

	var vault []string
	for hash := 500; hash <= 510; hash++ {
		vault = append(vault, fmt.Sprintf(
			`{"itemHash": %d, "itemInstanceId": "%d", "quantity": 1, "location": 2, "bucketHash": %d}`,
			hash, hash*10, bucketVault,
		))
	}

	profile := `{
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [` + strings.Join(vault, ",") + `]}}
	}`

	var p Profile

	require.NoError(t, json.Unmarshal([]byte(profile), &p))

	owned := ownedItems(NewSnapshot(&p, defs, fixtureNow))
	require.Len(t, owned, 11)

	return owned
}

// matchNames returns the name of each item of items that query matches,
// in the order of [compiledQuery.matches].
func matchNames(t *testing.T, query string, items []*Item) []string {
	t.Helper()

	q := compiled(t, query)
	require.Empty(t, q.unsupported)

	names := []string{}
	for _, item := range q.matches(searchContext{items: items}, items) {
		names = append(names, item.Name)
	}

	return names
}

func TestWeaponTypeWords(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	assert.Equal(t, []string{"Ace of Spades", "Fatebringer"}, matchNames(t, "is:handcannon", items))

	for _, item := range compiled(t, "is:handcannon").matches(searchContext{items: items}, items) {
		assert.Equal(t, "Hand Cannon", item.TypeName)
	}
}

func TestWeaponTypeOr(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	got := matchNames(t, "(is:sniperrifle or is:shotgun)", items)
	assert.Equal(t, []string{"Beloved", "Matador 64"}, got)
}

func TestWeaponTypeAliases(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	tests := map[string]struct {
		alias string
		word  string
		want  []string
	}{
		"lfr": {alias: "is:lfr", word: "is:linearfusionrifle", want: []string{"Sleeper Simulant"}},
		"lmg": {alias: "is:lmg", word: "is:machinegun", want: []string{"Thunderlord"}},
		"smg": {alias: "is:smg", word: "is:submachine", want: []string{"Riskrunner"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchNames(t, tc.word, items))
			assert.Equal(t, tc.want, matchNames(t, tc.alias, items))
		})
	}

	// The snapshot drops the fusion rifle category of a linear fusion
	// rifle.
	assert.Equal(t, []string{"Plug One.1"}, matchNames(t, "is:fusionrifle", items))
}

func TestGrenadeLaunchers(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	tests := map[string]struct {
		query string
		want  []string
	}{
		"both":    {query: "is:grenadelauncher", want: []string{"Forbearance", "Wendigo GL3"}},
		"heavy":   {query: "is:heavygrenadelauncher", want: []string{"Wendigo GL3"}},
		"special": {query: "is:specialgrenadelauncher", want: []string{"Forbearance"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchNames(t, tc.query, items))
		})
	}
}

func TestAmmoWords(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	tests := map[string]struct {
		query string
		want  []string
	}{
		"primary": {query: "is:primary", want: []string{"Ace of Spades", "Fatebringer", "Riskrunner"}},
		"special": {
			query: "is:special",
			want:  []string{"Beloved", "Forbearance", "Matador 64", "Plug One.1"},
		},
		"heavy":        {query: "is:heavy", want: []string{"Sleeper Simulant", "Thunderlord", "Wendigo GL3"}},
		"no ammo type": {query: "-is:primary -is:special -is:heavy", want: []string{"Celestial Nighthawk"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchNames(t, tc.query, items))
		})
	}
}

func TestCategoryWords(t *testing.T) {
	t.Parallel()

	// Each hash is the ItemCategoryHashes value of DIM that the word reads.
	tests := map[string]struct {
		category int64
	}{
		"autorifle":              {category: 5},
		"handcannon":             {category: 6},
		"pulserifle":             {category: 7},
		"scoutrifle":             {category: 8},
		"fusionrifle":            {category: 9},
		"sniperrifle":            {category: 10},
		"shotgun":                {category: 11},
		"machinegun":             {category: 12},
		"lmg":                    {category: 12},
		"rocketlauncher":         {category: 13},
		"sidearm":                {category: 14},
		"sword":                  {category: 54},
		"heavygrenadelauncher":   {category: 153950757},
		"specialgrenadelauncher": {category: -153950757},
		"tracerifle":             {category: 2489664120},
		"linearfusionrifle":      {category: 1504945536},
		"lfr":                    {category: 1504945536},
		"submachine":             {category: 3954685534},
		"smg":                    {category: 3954685534},
		"bow":                    {category: 3317538576},
		"glaive":                 {category: 3871742104},
		"transmat":               {category: 208981632},
		"weaponmod":              {category: 610365472},
		"armormod":               {category: 4104513227},
		"reptoken":               {category: 2088636411},
	}

	// The items hold one category each, so a word matches only the item of
	// its own category.
	var items []*Item
	for _, tc := range tests {
		id := strconv.FormatInt(tc.category, 10)
		if !slices.ContainsFunc(items, func(item *Item) bool { return item.InstanceID == id }) {
			items = append(items, &Item{InstanceID: id, Categories: []int64{tc.category}})
		}
	}

	for word, tc := range tests {
		t.Run(word, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{strconv.FormatInt(tc.category, 10)}, matchIDs(t, "is:"+word, items))
		})
	}
}
