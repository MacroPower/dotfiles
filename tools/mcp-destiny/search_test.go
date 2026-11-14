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

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungietest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitionstest"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
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
		if test(item).state == match {
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

	defs := definitionstest.Defs()
	defs.Items[400] = definitions.ItemDef{
		Name:      "Exotic Engram",
		Bucket:    bungie.BucketEngrams,
		ClassType: bungie.ClassAny,
	}
	defs.Items[401] = definitions.ItemDef{
		Name:       "Wave",
		Bucket:     bungie.BucketEmotes,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	defs.Items[402] = definitions.ItemDef{
		Name:       "Takedown",
		Bucket:     bungie.BucketFinishers,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}
	defs.Items[403] = definitions.ItemDef{
		Name:       "Dummy",
		Bucket:     bungie.BucketForbidden,
		ItemType:   bungie.ItemTypeWeapon,
		ClassType:  bungie.ClassAny,
		Equippable: true,
	}

	var p bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(profile), &p))

	s := NewSnapshot(&p, defs, bungietest.Now)
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
				{InstanceID: "kinetic", Bucket: bungie.BucketKinetic},
				{InstanceID: "energy", Bucket: bungie.BucketEnergy},
				{InstanceID: "power", Bucket: bungie.BucketPower},
				{InstanceID: "helmet", Bucket: bungie.BucketHelmet},
				{InstanceID: "weapon kind", Bucket: bungie.BucketEngrams, Kind: kindWeapon},
			},
			want: []string{"kinetic", "energy", "power"},
		},
		"is:armor": {
			query: "is:armor",
			items: []*Item{
				{InstanceID: "helmet", Bucket: bungie.BucketHelmet},
				{InstanceID: "gauntlets", Bucket: bungie.BucketGauntlets},
				{InstanceID: "chest", Bucket: bungie.BucketChestArmor},
				{InstanceID: "legs", Bucket: bungie.BucketLegArmor},
				{InstanceID: "class", Bucket: bungie.BucketClassArmor},
				{InstanceID: "kinetic", Bucket: bungie.BucketKinetic},
				{InstanceID: "armor kind", Bucket: bungie.BucketEngrams, Kind: kindArmor},
			},
			want: []string{"helmet", "gauntlets", "chest", "legs", "class"},
		},
		"is:equipment": {
			query: "is:equipment",
			items: []*Item{
				{InstanceID: "equippable", Bucket: bungie.BucketKinetic, Equippable: true},
				{InstanceID: "not equippable", Bucket: bungie.BucketKinetic},
				{InstanceID: "emote", Bucket: bungie.BucketEmotes, Equippable: true},
				{InstanceID: "finisher", Bucket: bungie.BucketFinishers, Equippable: true},
			},
			want: []string{"equippable"},
		},
		"is:equippable": {
			query: "is:equippable",
			items: []*Item{
				{InstanceID: "equippable", Bucket: bungie.BucketKinetic, Equippable: true},
				{InstanceID: "not equippable", Bucket: bungie.BucketKinetic},
				{InstanceID: "emote", Bucket: bungie.BucketEmotes, Equippable: true},
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
				{InstanceID: "character", Owner: bungietest.HunterID},
			},
			want: []string{"vault"},
		},
		"is:postmaster": {
			query: "is:postmaster",
			items: []*Item{
				{InstanceID: "lost", Bucket: bungie.BucketKinetic, Lost: true, Postmaster: true},
				{InstanceID: "engram", Bucket: bungie.BucketEngrams},
				{InstanceID: "message", Bucket: bungie.BucketMessages, Postmaster: true},
				{InstanceID: "special order", Bucket: bungie.BucketSpecialOrders},
				{InstanceID: "postmaster flag", Bucket: bungie.BucketKinetic, Postmaster: true},
				{InstanceID: "held", Bucket: bungie.BucketKinetic},
			},
			want: []string{"lost", "engram", "message", "special order"},
		},
		"is:inpostmaster": {
			query: "is:inpostmaster",
			items: []*Item{
				{InstanceID: "lost", Bucket: bungie.BucketKinetic, Lost: true, Postmaster: true},
				{InstanceID: "engram", Bucket: bungie.BucketEngrams},
				{InstanceID: "held", Bucket: bungie.BucketKinetic},
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
				{InstanceID: "titan", ClassType: bungie.ClassTitan},
				{InstanceID: "hunter", ClassType: bungie.ClassHunter},
				{InstanceID: "warlock", ClassType: bungie.ClassWarlock},
				{InstanceID: "any", ClassType: bungie.ClassAny},
			},
			want: []string{"titan"},
		},
		"is:hunter": {
			query: "is:hunter",
			items: []*Item{
				{InstanceID: "titan", ClassType: bungie.ClassTitan},
				{InstanceID: "hunter", ClassType: bungie.ClassHunter},
				{InstanceID: "any", ClassType: bungie.ClassAny},
			},
			want: []string{"hunter"},
		},
		"is:warlock": {
			query: "is:warlock",
			items: []*Item{
				{InstanceID: "hunter", ClassType: bungie.ClassHunter},
				{InstanceID: "warlock", ClassType: bungie.ClassWarlock},
				{InstanceID: "any", ClassType: bungie.ClassAny},
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
				{InstanceID: "junk", Tag: dvm.TagJunk},
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

	custom := dvm.PrimaryTag([]string{"custom"})
	require.Equal(t, "custom", custom)

	items := []*Item{
		{InstanceID: "keep", Tag: "keep"},
		{InstanceID: "junk", Tag: dvm.TagJunk},
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
		{InstanceID: "locked weapon", Bucket: bungie.BucketKinetic, Lockable: true, Locked: true},
		{InstanceID: "weapon", Bucket: bungie.BucketKinetic, Lockable: true},
		{InstanceID: "locked helmet", Bucket: bungie.BucketHelmet, Lockable: true, Locked: true},
		{InstanceID: "engram", Bucket: bungie.BucketEngrams},
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
		"is:weapon", "is:armor", "is:equipment", "is:equippable", "is:equipped", "is:invault", "is:postmaster",
		"is:inpostmaster", "is:locked", "is:unlocked", "is:titan", "is:hunter", "is:warlock", "is:favorite",
		"is:autorifle", "is:handcannon", "is:pulserifle", "is:scoutrifle", "is:fusionrifle", "is:sniperrifle",
		"is:shotgun", "is:machinegun", "is:lmg", "is:rocketlauncher", "is:sidearm", "is:sword",
		"is:grenadelauncher", "is:heavygrenadelauncher", "is:specialgrenadelauncher", "is:tracerifle",
		"is:linearfusionrifle", "is:lfr", "is:submachine", "is:smg", "is:bow", "is:glaive", "is:transmat",
		"is:weaponmod", "is:armormod", "is:reptoken", "is:primary", "is:special", "is:heavy", "is:dupe",
		"is:incurrentchar", "is:currentclass", "is:common", "is:uncommon", "is:rare", "is:legendary", "is:exotic",
		"is:white", "is:green", "is:blue", "is:purple", "is:yellow", "is:kinetic", "is:arc", "is:solar", "is:void",
		"is:stasis", "is:strand", "is:light", "is:dark", "is:engrams", "is:lostitems", "is:messages",
		"is:specialorders", "is:kineticslot", "is:energy", "is:power", "is:helmet", "is:gauntlets", "is:chest",
		"is:leg", "is:classitem", "is:subclass", "is:seasonalartifacts", "is:ghost", "is:emblems", "is:ships",
		"is:vehicle", "is:emotes", "is:finishers", "is:accessories", "is:consumables", "is:modifications",
		"is:haspower", "is:haslight", "is:autotagged", "tag:", "dupe:item", "count:", "id:", "hash:", "name:",
		"exactname:", "power:", "light:", "rate:", "rating:", "ratebase:", "ratingbase:", "misses:", "setbonus:",
		"verdict:", "pvprate:", "pvprating:", "pvpratebase:", "pvpratingbase:", "pvpmisses:", "pvpsetbonus:",
		"pvpverdict:",
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
		"is:linearfusionrifle, is:lfr, is:submachine, is:smg, is:bow, is:glaive, is:transmat, is:weaponmod, "+
		"is:armormod, is:reptoken, is:primary, is:special, is:heavy, is:dupe, is:incurrentchar, "+
		"is:currentclass, is:common, is:uncommon, is:rare, is:legendary, is:exotic, is:white, is:green, "+
		"is:blue, is:purple, is:yellow, is:kinetic, is:arc, is:solar, is:void, is:stasis, is:strand, "+
		"is:light, is:dark, is:engrams, is:lostitems, is:messages, is:specialorders, is:kineticslot, "+
		"is:energy, is:power, is:helmet, is:gauntlets, is:chest, is:leg, is:classitem, is:subclass, "+
		"is:seasonalartifacts, is:ghost, is:emblems, is:ships, is:vehicle, is:emotes, is:finishers, "+
		"is:accessories, is:consumables, is:modifications, is:haspower, is:haslight, is:autotagged, tag:, "+
		"dupe:item, count:, id:, hash:, name:, exactname:, power:, light:, rate:, rating:, ratebase:, "+
		"ratingbase:, misses:, setbonus:, verdict:, pvprate:, pvprating:, pvpratebase:, pvpratingbase:, "+
		"pvpmisses:, pvpsetbonus:, and pvpverdict:. "+
		"Each is: word also works as not:<word>.", err.Error())
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
		{InstanceID: "20", Name: "Gjallarhorn", Bucket: bungie.BucketPower},
		{InstanceID: "100", Name: "Fatebringer", Bucket: bungie.BucketKinetic},
		{InstanceID: "9", Name: "Fatebringer", Bucket: bungie.BucketKinetic},
		{InstanceID: "5", Name: "Celestial Nighthawk", Bucket: bungie.BucketHelmet},
	}

	got := compiled(t, "is:weapon").evaluate(searchContext{items: items}, items).matches
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

func TestRangeComparatorUnsupported(t *testing.T) {
	t.Parallel()

	format := rangeFormat{overloads: map[string]float64{"worthy": 10}, unsupported: []string{"pinnaclecap"}}

	tests := map[string]struct {
		value string
		err   error
	}{
		"a word without an operator":       {value: "pinnaclecap", err: errOverloadValue},
		"a word after an operator":         {value: ">=pinnaclecap", err: errOverloadValue},
		"a word after an invalid operator": {value: "=<pinnaclecap", err: errNotRange},
		"a word after three operators":     {value: "<<=pinnaclecap", err: errNotRange},
		"a word that starts with the word": {value: "pinnaclecaps", err: errNotRange},
		"a word of the overloads":          {value: "<worthy"},
		"a number":                         {value: "<10"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			compare, err := format.comparator(tc.value)
			if tc.err == nil {
				require.NoError(t, err)
				assert.True(t, compare(5))

				return
			}

			require.ErrorIs(t, err, tc.err)
			assert.Nil(t, compare)
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
		TierType:   bungie.TierTypeLegendary,
		ClassType:  bungie.ClassAny,
		Bucket:     bungie.BucketKinetic,
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
			item: &Item{
				Name:      "Helm ",
				TierType:  bungie.TierTypeExotic,
				ClassType: bungie.ClassHunter,
				Bucket:    bungie.BucketHelmet,
			},
			want: "Helm 1Exotic3448274439",
		},
		"armor keeps a translation key": {
			item: &Item{
				Name:      "Filter.Adept Helm",
				TierType:  bungie.TierTypeExotic,
				ClassType: bungie.ClassTitan,
				Bucket:    bungie.BucketHelmet,
			},
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
			items: pair("Emblem", func(item *Item) { item.Bucket = bungie.BucketEmblems }),
			want:  []string{},
		},
		"pair in the Artifacts bucket": {
			items: pair("Artifact", func(item *Item) { item.Bucket = bungie.BucketArtifacts }),
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
			items: pair("Wave", func(item *Item) { item.Bucket = bungie.BucketEmotes }),
			want:  []string{},
		},
		"same name in two classes": {
			items: []*Item{comparableWeapon("titan", "Helm"), {
				InstanceID: "hunter", Name: "Helm", Tier: "Legendary", TierType: bungie.TierTypeLegendary,
				ClassType: bungie.ClassHunter, Bucket: bungie.BucketKinetic, Equippable: true, Lockable: true,
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
	emblem.Bucket = bungie.BucketEmblems

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
		current: Character{ID: bungietest.HunterID, ClassType: bungie.ClassHunter},
		items: []*Item{
			{InstanceID: "hunter armor", Owner: bungietest.HunterID, ClassType: bungie.ClassHunter},
			{InstanceID: "hunter weapon", Owner: bungietest.HunterID, ClassType: bungie.ClassAny},
			{InstanceID: "account", Owner: ownerAccount, ClassType: bungie.ClassAny},
			{InstanceID: "vault armor", Owner: ownerVault, ClassType: bungie.ClassHunter},
			{InstanceID: "vault weapon", Owner: ownerVault, ClassType: bungie.ClassAny},
			{InstanceID: "titan armor", Owner: bungietest.TitanID, ClassType: bungie.ClassTitan},
			{InstanceID: "titan holds hunter armor", Owner: bungietest.TitanID, ClassType: bungie.ClassHunter},
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

	defs := definitionstest.Defs()

	item := func(name, typeName string, bucket uint32, ammoType int, categories ...int64) definitions.ItemDef {
		def := definitions.ItemDef{
			Name:       name,
			TypeName:   typeName,
			Tier:       "Legendary",
			Bucket:     bucket,
			ItemType:   bungie.ItemTypeWeapon,
			ClassType:  bungie.ClassAny,
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
	defs.Items[500] = item("Fatebringer", "Hand Cannon", bungie.BucketKinetic, bungie.AmmoPrimary,
		bungie.CategoryWeapon, 2, bungie.CategoryHandCannon)
	defs.Items[501] = item("Ace of Spades", "Hand Cannon", bungie.BucketKinetic, bungie.AmmoPrimary,
		bungie.CategoryWeapon, 2, bungie.CategoryHandCannon)
	defs.Items[502] = item("Beloved", "Sniper Rifle", bungie.BucketEnergy, bungie.AmmoSpecial,
		bungie.CategoryWeapon, 3, bungie.CategorySniperRifle)
	defs.Items[503] = item("Matador 64", "Shotgun", bungie.BucketKinetic, bungie.AmmoSpecial,
		bungie.CategoryWeapon, 2, bungie.CategoryShotgun)
	defs.Items[504] = item("Plug One.1", "Fusion Rifle", bungie.BucketEnergy, bungie.AmmoSpecial,
		bungie.CategoryWeapon, 3, bungie.CategoryFusionRifle)
	defs.Items[505] = item(
		"Sleeper Simulant",
		"Linear Fusion Rifle",
		bungie.BucketPower,
		bungie.AmmoHeavy,
		bungie.CategoryWeapon,
		bungie.CategoryPowerWeapon,
		bungie.CategoryFusionRifle,
		bungie.CategoryLinearFusionRifles,
	)
	defs.Items[506] = item("Thunderlord", "Machine Gun", bungie.BucketPower, bungie.AmmoHeavy,
		bungie.CategoryWeapon, bungie.CategoryPowerWeapon, bungie.CategoryMachineGun)
	defs.Items[507] = item("Riskrunner", "Submachine Gun", bungie.BucketEnergy, bungie.AmmoPrimary,
		bungie.CategoryWeapon, 3, bungie.CategorySubmachineGuns)
	defs.Items[508] = item("Wendigo GL3", "Grenade Launcher", bungie.BucketPower, bungie.AmmoHeavy,
		bungie.CategoryWeapon, bungie.CategoryPowerWeapon, bungie.CategoryGrenadeLaunchers)
	defs.Items[509] = item("Forbearance", "Grenade Launcher", bungie.BucketEnergy, bungie.AmmoSpecial,
		bungie.CategoryWeapon, 3, bungie.CategoryGrenadeLaunchers)
	defs.Items[510] = definitions.ItemDef{
		Name:       "Celestial Nighthawk",
		TypeName:   "Helmet",
		Tier:       "Exotic",
		Bucket:     bungie.BucketHelmet,
		ItemType:   bungie.ItemTypeArmor,
		ClassType:  bungie.ClassHunter,
		Categories: []uint32{20, 45},
		Equippable: true,
	}

	var vault []string
	for hash := 500; hash <= 510; hash++ {
		vault = append(vault, fmt.Sprintf(
			`{"itemHash": %d, "itemInstanceId": "%d", "quantity": 1, "location": 2, "bucketHash": %d}`,
			hash, hash*10, bungie.BucketVault,
		))
	}

	profile := `{
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"profileInventory": {"data": {"items": [` + strings.Join(vault, ",") + `]}}
	}`

	var p bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(profile), &p))

	owned := ownedItems(NewSnapshot(&p, defs, bungietest.Now))
	require.Len(t, owned, 11)

	return owned
}

// matchNames returns the name of each item of items that query matches,
// in the order of [compiledQuery.evaluate].
func matchNames(t *testing.T, query string, items []*Item) []string {
	t.Helper()

	q := compiled(t, query)
	require.Empty(t, q.unsupported)

	names := []string{}
	for _, item := range q.evaluate(searchContext{items: items}, items).matches {
		names = append(names, item.Name)
	}

	return names
}

func TestWeaponTypeWords(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	assert.Equal(t, []string{"Ace of Spades", "Fatebringer"}, matchNames(t, "is:handcannon", items))

	for _, item := range compiled(t, "is:handcannon").evaluate(searchContext{items: items}, items).matches {
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

func TestKleene(t *testing.T) {
	t.Parallel()

	var (
		yes     = outcome{state: match}
		no      = outcome{}
		other   = outcome{reasons: []string{"another reason"}, state: undecided}
		noPerks = outcome{reasons: []string{reasonNoPerks}, state: undecided}
		both    = outcome{reasons: []string{"another reason", reasonNoPerks}, state: undecided}
	)

	tests := map[string]struct {
		got  outcome
		want outcome
	}{
		"match and match":                {got: yes.and(yes), want: yes},
		"match and no match":             {got: yes.and(no), want: no},
		"match and undecided":            {got: yes.and(other), want: other},
		"no match and match":             {got: no.and(yes), want: no},
		"no match and no match":          {got: no.and(no), want: no},
		"no match and undecided":         {got: no.and(other), want: no},
		"undecided and match":            {got: other.and(yes), want: other},
		"undecided and no match":         {got: other.and(no), want: no},
		"undecided and undecided":        {got: other.and(noPerks), want: both},
		"and keeps each reason one time": {got: other.and(both), want: both},
		"match or match":                 {got: yes.or(yes), want: yes},
		"match or no match":              {got: yes.or(no), want: yes},
		"match or undecided":             {got: yes.or(other), want: yes},
		"no match or match":              {got: no.or(yes), want: yes},
		"no match or no match":           {got: no.or(no), want: no},
		"no match or undecided":          {got: no.or(other), want: other},
		"undecided or match":             {got: other.or(yes), want: yes},
		"undecided or no match":          {got: other.or(no), want: other},
		"undecided or undecided":         {got: other.or(noPerks), want: both},
		"or keeps each reason one time":  {got: both.or(noPerks), want: both},
		"not match":                      {got: yes.not(), want: no},
		"not no match":                   {got: no.not(), want: yes},
		"not undecided":                  {got: both.not(), want: both},
		"decided true":                   {got: decided(true), want: yes},
		"decided false":                  {got: decided(false), want: no},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.got)
		})
	}
}

// ratingContext returns a search context whose items cover the rating
// filters, keyed by instance id, in this order:
//
//   - s, a, and b are copies of weapon 100, which the Aegis sheet rates S
//     with two perk columns. Copy s hits both columns, a misses one, and b
//     misses both, so their rolls rate S, A, and B.
//   - exotic is weapon 101, which the Aegis sheet rates A without columns.
//   - no tier is weapon 102, on an Aegis row without a tier or columns.
//   - unknown is weapon 103 without perks, on an Aegis row with the tier
//     A and one column.
//   - unknown no tier is weapon 104 without perks, on an Aegis row
//     without a tier but with one column.
//   - unlisted is a weapon that neither sheet lists.
//   - helmet is armor that neither sheet lists.
//   - rated helmet is armor with hash 101, and unknown helmet is armor
//     with hash 103, so the Aegis sheet rates both.
//
// The PvP sheet rates weapon 100 C with one column, which every copy of
// the weapon misses. The rolls of s, a, and b therefore rate D for PvP.
func ratingContext() searchContext {
	pve := &Sheet{
		Rows: map[uint32]SheetRow{100: {Tier: "S"}, 101: {Tier: "A"}, 102: {}, 103: {Tier: "A"}, 104: {}},
		Columns: map[uint32][][]uint32{
			100: {{plugExplosivePayload.Hash}, {plugKillClip.Hash}},
			103: {{plugExplosivePayload.Hash}},
			104: {{plugExplosivePayload.Hash}},
		},
	}
	pvp := &Sheet{
		Rows:    map[uint32]SheetRow{100: {Tier: "C"}},
		Columns: map[uint32][][]uint32{100: {{906}}},
	}

	weapon := func(id string, hash uint32, perks ...Perk) *Item {
		return &Item{InstanceID: id, Name: id, Hash: hash, Bucket: bungie.BucketKinetic, Perks: perks}
	}

	armor := func(id string, hash uint32) *Item {
		return &Item{InstanceID: id, Name: id, Hash: hash, Bucket: bungie.BucketHelmet}
	}

	return searchContext{
		items: []*Item{
			weapon("s", 100, Perk{Plug: plugExplosivePayload}, Perk{Plug: plugKillClip, Socket: 1}),
			weapon("a", 100, rolledPerks()...),
			weapon("b", 100, Perk{Plug: plugFirefly}),
			weapon("exotic", 101),
			weapon("no tier", 102),
			weapon("unknown", 103),
			weapon("unknown no tier", 104),
			weapon("unlisted", 999, rolledPerks()...),
			armor("helmet", 200),
			armor("rated helmet", 101),
			armor("unknown helmet", 103),
		},
		sources: map[string]RatingSource{
			modePvE: {Sheet: pve, Defs: &definitions.Defs{}},
			modePvP: {Sheet: pvp, Defs: &definitions.Defs{}},
		},
	}
}

// judge returns the instance id of each item of ctx that query matches,
// in the order of the items. It also returns the reasons of each item
// whose outcome is undecided, keyed by instance id. Every term of query
// must be supported.
func judge(t *testing.T, query string, ctx searchContext) ([]string, map[string][]string) {
	t.Helper()

	q := compiled(t, query)
	require.Empty(t, q.unsupported)
	require.Empty(t, q.invalid)
	require.NotNil(t, q.test)

	test := q.test(ctx)
	matched := []string{}
	open := map[string][]string{}

	for _, item := range ctx.items {
		got := test(item)

		switch got.state {
		case match:
			matched = append(matched, item.InstanceID)
		case undecided:
			open[item.InstanceID] = got.reasons
		case noMatch:
			assert.Empty(t, got.reasons, item.InstanceID)
		}
	}

	return matched, open
}

// judgeTests is a table of queries with the items that each one matches
// and the reasons of the items that it leaves undecided.
type judgeTests map[string]struct {
	undecided map[string][]string
	query     string
	matches   []string
}

// run runs each case of tests against ctx.
func (tests judgeTests) run(t *testing.T, ctx searchContext) {
	t.Helper()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			matches, open := judge(t, tc.query, ctx)
			assert.Equal(t, tc.matches, matches)

			want := tc.undecided
			if want == nil {
				want = map[string][]string{}
			}

			assert.Equal(t, want, open)
		})
	}
}

// noPerksOnly holds the reason of a copy without perks on a sheet row
// with perk columns.
var noPerksOnly = []string{reasonNoPerks}

func TestRateRange(t *testing.T) {
	t.Parallel()

	// open holds the copies without perks on a row with a tier and perk
	// columns, which each rate: value of the Aegis sheet leaves undecided.
	// The armor of the context has no set, so rate: decides it.
	open := map[string][]string{
		"unknown":        noPerksOnly,
		"unknown helmet": noPerksOnly,
	}

	judgeTests{
		"A or better": {
			query:     "rate:>=a",
			matches:   []string{"s", "a", "exotic", "rated helmet"},
			undecided: open,
		},
		"a number tests the tier value": {
			query:     "rate:>=5",
			matches:   []string{"s", "a", "exotic", "rated helmet"},
			undecided: open,
		},
		"rating: is rate:": {
			query:     "rating:>=a",
			matches:   []string{"s", "a", "exotic", "rated helmet"},
			undecided: open,
		},
		"equal to B":   {query: "rate:b", matches: []string{"b"}, undecided: open},
		"worse than A": {query: "rate:<a", matches: []string{"b"}, undecided: open},
		"constructor matches nothing": {
			query:     "rate:constructor",
			matches:   []string{},
			undecided: open,
		},
		"__proto__ matches nothing": {
			query:     "rate:__proto__",
			matches:   []string{},
			undecided: open,
		},
	}.run(t, ratingContext())

	ctx := ratingContext()

	// Two weapons have no value. The sheet does not list one, and the
	// other sits on a row without a tier. Every rate: value decides both.
	for _, query := range []string{"rate:>=f", "rate:<=s", "rate:f", "rate:s", "rating:>=0", "rate:<7"} {
		matches, open := judge(t, query, ctx)
		assert.NotContains(t, matches, "unlisted", query)
		assert.NotContains(t, open, "unlisted", query)
		assert.NotContains(t, matches, "no tier", query)
		assert.NotContains(t, open, "no tier", query)
	}

	for _, query := range []string{"rate:>=z", "rating:a+", "rate:>>a", "rate:=<a", "rate:1.", "rate:-1"} {
		assert.Equal(t, []string{query}, compiled(t, query).invalid, query)
	}
}

func TestRateBase(t *testing.T) {
	t.Parallel()

	// ratebase: reads no perks, and the armor of the context has no set,
	// so ratebase: decides every item.
	judgeTests{
		"the row of a roll that stepped down": {
			query:   "ratebase:s",
			matches: []string{"s", "a", "b"},
		},
		"ratingbase: is ratebase:": {
			query:   "ratingbase:s",
			matches: []string{"s", "a", "b"},
		},
		"a copy without perks has the tier of its row": {
			query:   "ratebase:>=a",
			matches: []string{"s", "a", "b", "exotic", "unknown", "rated helmet", "unknown helmet"},
		},
	}.run(t, ratingContext())

	assert.Equal(t, []string{"ratebase:>=z"}, compiled(t, "ratebase:>=z").invalid)
}

func TestMisses(t *testing.T) {
	t.Parallel()

	// open holds the copies without perks on a row with columns, which
	// every misses: value leaves undecided. misses: reads no set bonus,
	// so it decides the armor that the sheet does not list.
	open := map[string][]string{
		"unknown":         noPerksOnly,
		"unknown no tier": noPerksOnly,
		"unknown helmet":  noPerksOnly,
	}

	judgeTests{
		"two misses":   {query: "misses:2", matches: []string{"b"}, undecided: open},
		"at least one": {query: "misses:>=1", matches: []string{"a", "b"}, undecided: open},
		"no miss":      {query: "misses:0", matches: []string{"s"}, undecided: open},
		"a row without columns never matches": {
			query:     "misses:>=0",
			matches:   []string{"s", "a", "b"},
			undecided: open,
		},
	}.run(t, ratingContext())

	for _, query := range []string{"misses:a", "misses:constructor", "misses:__proto__", "pvpmisses:s"} {
		assert.Equal(t, []string{query}, compiled(t, query).invalid, query)
	}
}

func TestRateUnknownRoll(t *testing.T) {
	t.Parallel()

	// set helmet is armor without perks on the row of hash 103, as unknown
	// helmet is. Its item set has a 4-piece bonus that the Aegis sheet
	// rates S.
	ctx := ratingContext()
	ctx.sources[modePvE].Sheet.SetBonuses = map[uint32]SetBonus{perkFourPiece: {Tier: "S", Pieces: 4}}
	ctx.items = append(ctx.items, &Item{
		InstanceID: "set helmet",
		Name:       "set helmet",
		Hash:       103,
		Bucket:     bungie.BucketHelmet,
		SetHash:    7,
		SetKnown:   true,
		SetPerks:   []uint32{perkFourPiece},
	})

	tests := map[string]struct {
		query string
		id    string
		want  outcome
	}{
		"rate: cannot decide a copy without perks": {
			query: "rate:>=a",
			id:    "unknown",
			want:  outcome{reasons: noPerksOnly, state: undecided},
		},
		"ratebase: decides a copy without perks": {
			query: "ratebase:>=a",
			id:    "unknown",
			want:  outcome{state: match},
		},
		"a row without a tier gives no tier": {
			query: "rate:>=a",
			id:    "unknown no tier",
			want:  outcome{},
		},
		"armor without perks on a row with columns has the perk reason": {
			query: "rate:>=a",
			id:    "unknown helmet",
			want:  outcome{reasons: noPerksOnly, state: undecided},
		},
		"two terms give each reason one time": {
			query: "rate:>=a misses:>=0",
			id:    "unknown",
			want:  outcome{reasons: noPerksOnly, state: undecided},
		},
		"is:weapon keeps a weapon without perks undecided": {
			query: "is:weapon rate:>=a",
			id:    "unknown",
			want:  outcome{reasons: noPerksOnly, state: undecided},
		},
		"is:weapon decides armor without perks": {
			query: "is:weapon rate:>=a",
			id:    "unknown helmet",
			want:  outcome{},
		},
		"a set bonus that passes decides a copy without perks": {
			query: "rate:s",
			id:    "set helmet",
			want:  outcome{state: match},
		},
		"a set bonus that passes beside the tier of the row": {
			query: "rate:>=a",
			id:    "set helmet",
			want:  outcome{state: match},
		},
		"a copy without perks stays undecided when no set bonus passes": {
			query: "rate:f",
			id:    "set helmet",
			want:  outcome{reasons: noPerksOnly, state: undecided},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			i := slices.IndexFunc(ctx.items, func(item *Item) bool { return item.InstanceID == tc.id })
			require.GreaterOrEqual(t, i, 0)

			assert.Equal(t, tc.want, compiled(t, tc.query).test(ctx)(ctx.items[i]))
		})
	}
}

func TestPvPKeywords(t *testing.T) {
	t.Parallel()

	ctx := ratingContext()

	tests := map[string]struct {
		query string
		want  []string
		modes []string
	}{
		"pvprate:":         {query: "pvprate:d", want: []string{"s", "a", "b"}, modes: []string{modePvP}},
		"pvprating:":       {query: "pvprating:d", want: []string{"s", "a", "b"}, modes: []string{modePvP}},
		"pvpratebase:":     {query: "pvpratebase:c", want: []string{"s", "a", "b"}, modes: []string{modePvP}},
		"pvpratingbase:":   {query: "pvpratingbase:c", want: []string{"s", "a", "b"}, modes: []string{modePvP}},
		"pvpmisses:":       {query: "pvpmisses:1", want: []string{"s", "a", "b"}, modes: []string{modePvP}},
		"rate: reads PvE":  {query: "rate:d", want: []string{}, modes: []string{modePvE}},
		"ratebase: of PvE": {query: "ratebase:c", want: []string{}, modes: []string{modePvE}},
		"misses: of PvE":   {query: "misses:1", want: []string{"a"}, modes: []string{modePvE}},
		"both modes in the order of the game modes": {
			query: "pvprate:d or rate:d",
			want:  []string{"s", "a", "b"},
			modes: []string{modePvE, modePvP},
		},
		"a negated term reads its mode": {
			query: "is:weapon -pvpmisses:1",
			want:  []string{"exotic", "no tier", "unknown", "unknown no tier", "unlisted"},
			modes: []string{modePvP},
		},
		"no rating filter": {
			query: "is:weapon",
			want:  []string{"s", "a", "b", "exotic", "no tier", "unknown", "unknown no tier", "unlisted"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.modes, compiled(t, tc.query).modes)
			assert.Equal(t, tc.want, contextMatchIDs(t, tc.query, ctx))
		})
	}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()

	// Two copies of weapon 103 without perks share the name b, so rate:b
	// leaves both undecided.
	ctx := ratingContext()
	ctx.items = slices.Concat(ctx.items, []*Item{
		{InstanceID: "20", Name: "b", Hash: 103, Bucket: bungie.BucketKinetic},
		{InstanceID: "3", Name: "b", Hash: 103, Bucket: bungie.BucketKinetic},
	})

	result := compiled(t, "rate:b").evaluate(ctx, ctx.items)
	assert.Equal(t, []string{"b"}, ids(result.matches))

	got := make([]string, 0, len(result.undecided))
	for _, u := range result.undecided {
		got = append(got, u.item.InstanceID)
		assert.NotEmpty(t, u.reasons, u.item.InstanceID)
	}

	assert.Equal(t, []string{"3", "20", "unknown", "unknown helmet"}, got)
}

func TestUnratedReason(t *testing.T) {
	t.Parallel()

	noPvP := fmt.Errorf("%w: the service publishes no PvP sheet", ErrNoRating)
	noAegis := fmt.Errorf("%w: the service publishes no Aegis sheet", ErrNoRating)
	sheet := &Sheet{Rows: map[uint32]SheetRow{100: {Tier: "S"}}}

	tests := map[string]struct {
		sources map[string]RatingSource
		query   string
		want    string
	}{
		"the mode of the query has ratings": {
			query:   "rate:>=a",
			sources: map[string]RatingSource{modePvE: {Sheet: sheet}, modePvP: {Err: noPvP}},
		},
		"the mode of the query has no ratings": {
			query:   "pvprate:>=a",
			sources: map[string]RatingSource{modePvE: {Sheet: sheet}, modePvP: {Err: noPvP}},
			want:    "PvP: rating unavailable: the service publishes no PvP sheet",
		},
		"the first mode in the order of the game modes": {
			query:   "pvprate:>=a rate:>=a",
			sources: map[string]RatingSource{modePvE: {Err: noAegis}, modePvP: {Err: noPvP}},
			want:    "PvE: rating unavailable: the service publishes no Aegis sheet",
		},
		"a query without a rating filter": {
			query:   "is:weapon",
			sources: map[string]RatingSource{modePvE: {Err: noAegis}, modePvP: {Err: noPvP}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, compiled(t, tc.query).unratedReason(tc.sources))
		})
	}

	err := unratedQuery("PvP: rating unavailable: the service publishes no PvP sheet")
	require.ErrorIs(t, err, errUnratedMode)
	assert.Equal(t, "the query reads a game mode without ratings. "+
		"PvP: rating unavailable: the service publishes no PvP sheet", err.Error())
}

func TestVerdictOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		mine   []int
		rivals [][]int
		want   string
	}{
		"a copy without rivals stands alone": {
			mine: []int{0},
			want: verdictOnly,
		},
		"a rival with a superset of the hits": {
			mine:   []int{0},
			rivals: [][]int{{0, 1}},
			want:   verdictWorse,
		},
		"a rival with the same hits": {
			mine:   []int{0},
			rivals: [][]int{{0}},
			want:   verdictEqual,
		},
		"rivals with strict subsets of the hits": {
			mine:   []int{0, 1},
			rivals: [][]int{{0}, nil},
			want:   verdictBetter,
		},
		"a trade of hits": {
			mine:   []int{0},
			rivals: [][]int{{1}},
			want:   verdictMixed,
		},
		"the strongest of two rivals decides": {
			mine:   []int{0},
			rivals: [][]int{{0}, {0, 1}},
			want:   verdictWorse,
		},
		"two copies that miss every column": {
			rivals: [][]int{nil},
			want:   verdictEqual,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, verdictOf(tc.mine, tc.rivals))
		})
	}
}

// hitting returns the perks of a copy on a row whose columns
// verdictColumns returns. The copy hits the first column when first holds and the second column when
// second holds. It holds a perk in each socket, so the profile lists
// perks for it.
func hitting(first, second bool) []Perk {
	perks := []Perk{{Plug: plugFirefly}, {Plug: plugRampage, Socket: 1}}

	if first {
		perks[0].Plug = plugExplosivePayload
	}

	if second {
		perks[1].Plug = plugKillClip
	}

	return perks
}

// verdictColumns returns the perk columns of each row with columns of
// verdictContext. The first column picks Explosive Payload, and the
// second picks Kill Clip.
func verdictColumns() [][]uint32 {
	return [][]uint32{{plugExplosivePayload.Hash}, {plugKillClip.Hash}}
}

// verdictContext returns a search context whose items cover the verdict
// filters, keyed by instance id. The Aegis sheet gives each weapon hash
// below except 999 a row. Every row except the row of 140 has the columns
// of verdictColumns:
//
//   - best (100) and worse (101) are copies of a weapon and its reissue,
//     which share a row. Copy best hits both columns, and worse hits the
//     first, so best is better and worse is worse. Copy best sits in the
//     vault.
//   - same a and same b (110) each hit the first column, so each is equal
//     to the other.
//   - mixed a (120) hits the first column, and mixed b (120) hits the
//     second, so each is mixed.
//   - lone (130) is the only copy of its weapon.
//   - exotic (140) sits on a row without columns, and unlisted (999) is a
//     weapon that neither sheet lists.
//   - no perks (150) has no perks, and beside no perks (150) hits the
//     first column. The verdict of both is undecided.
//
// The PvP sheet lists only weapon 120, with one column that picks Kill
// Clip. Copy mixed b hits it, and mixed a misses it.
func verdictContext() searchContext {
	pve := &Sheet{
		Rows: map[uint32]SheetRow{
			100: {Tier: "S"}, 101: {Tier: "S"}, 110: {Tier: "A"}, 120: {Tier: "B"}, 130: {Tier: "C"},
			140: {Tier: "A"}, 150: {Tier: "D"},
		},
		Columns: map[uint32][][]uint32{
			100: verdictColumns(), 101: verdictColumns(), 110: verdictColumns(), 120: verdictColumns(),
			130: verdictColumns(), 150: verdictColumns(),
		},
		Versions: map[uint32][]uint32{
			100: {100, 101}, 101: {100, 101}, 110: {110}, 120: {120}, 130: {130}, 150: {150},
		},
	}
	pvp := &Sheet{
		Rows:     map[uint32]SheetRow{120: {Tier: "A"}},
		Columns:  map[uint32][][]uint32{120: {{plugKillClip.Hash}}},
		Versions: map[uint32][]uint32{120: {120}},
	}

	weapon := func(id string, hash uint32, perks []Perk) *Item {
		return &Item{
			InstanceID: id,
			Name:       id,
			Hash:       hash,
			Bucket:     bungie.BucketKinetic,
			Owner:      bungietest.HunterID,
			Perks:      perks,
		}
	}

	best := weapon("best", 100, hitting(true, true))
	best.Owner = ownerVault

	return searchContext{
		items: []*Item{
			best,
			weapon("worse", 101, hitting(true, false)),
			weapon("same a", 110, hitting(true, false)),
			weapon("same b", 110, hitting(true, false)),
			weapon("mixed a", 120, hitting(true, false)),
			weapon("mixed b", 120, hitting(false, true)),
			weapon("lone", 130, hitting(true, true)),
			weapon("exotic", 140, nil),
			weapon("unlisted", 999, hitting(true, true)),
			weapon("no perks", 150, nil),
			weapon("beside no perks", 150, hitting(true, false)),
		},
		sources: map[string]RatingSource{
			modePvE: {Sheet: pve, Defs: &definitions.Defs{}},
			modePvP: {Sheet: pvp, Defs: &definitions.Defs{}},
		},
	}
}

// verdictOpen returns the items whose verdict on the Aegis sheet of
// verdictContext the server cannot decide, with their reasons.
func verdictOpen() map[string][]string {
	return map[string][]string{
		"no perks":        {reasonVerdictNoPerks},
		"beside no perks": {reasonVerdictNoPerks},
	}
}

func TestVerdictRivals(t *testing.T) {
	t.Parallel()

	// The versions of 300 and 301 list 300 twice. The sheet gives 311,
	// a version of 310, no row of its own, which a decoded sheet never
	// does.
	sheet := &Sheet{
		Rows: map[uint32]SheetRow{300: {Tier: "A"}, 301: {Tier: "A"}, 302: {Tier: "A"}, 310: {Tier: "A"}},
		Columns: map[uint32][][]uint32{
			300: verdictColumns(),
			301: verdictColumns(),
			302: verdictColumns(),
			310: verdictColumns(),
		},
		Versions: map[uint32][]uint32{
			300: {300, 301, 300}, 301: {300, 301, 300}, 302: {302}, 310: {310, 311}, 311: {310, 311},
		},
	}

	weapon := func(id string, hash uint32, owner string, perks []Perk) *Item {
		return &Item{InstanceID: id, Name: id, Hash: hash, Bucket: bungie.BucketKinetic, Owner: owner, Perks: perks}
	}

	stack := weapon("", 300, bungietest.HunterID, hitting(true, true))
	ctx := searchContext{
		items: []*Item{
			weapon("original", 300, bungietest.HunterID, hitting(true, false)),
			weapon("reissue", 301, ownerVault, hitting(true, true)),
			weapon("unrelated", 302, bungietest.HunterID, hitting(true, true)),
			weapon("listed", 310, bungietest.HunterID, hitting(true, false)),
			weapon("unrated", 311, bungietest.HunterID, hitting(true, true)),
			stack,
		},
		sources: map[string]RatingSource{modePvE: {Sheet: sheet, Defs: &definitions.Defs{}}},
	}

	copies := copiesByHash(ctx.items)

	rivals := map[string][]string{}
	for _, item := range ctx.items[:5] {
		rivals[item.InstanceID] = ids(rivalsOf(sheet, copies, item))
	}

	assert.Equal(t, map[string][]string{
		"original":  {"reissue"},
		"reissue":   {"original"},
		"unrelated": {},
		"listed":    {"unrated"},
		"unrated":   {"listed"},
	}, rivals)

	// The copy without an instance id is not a rival of another copy.
	// Every copy of 300 and 301 is a rival of it.
	assert.Equal(t, []string{"original", "reissue"}, ids(rivalsOf(sheet, copies, stack)))

	// The reissue in the vault beats the original. The rival of listed
	// has no sheet row, so it drops out.
	ctx.items = ctx.items[:5]
	judgeTests{
		"the reissue in the vault is a rival":    {query: "verdict:worse", matches: []string{"original"}},
		"the original is a rival of the reissue": {query: "verdict:better", matches: []string{"reissue"}},
		"a weapon without rivals":                {query: "verdict:only", matches: []string{"unrelated", "listed"}},
	}.run(t, ctx)
}

func TestVerdictValues(t *testing.T) {
	t.Parallel()

	judgeTests{
		"better": {query: "verdict:better", matches: []string{"best"}, undecided: verdictOpen()},
		"same is the kind equal": {
			query:     "verdict:same",
			matches:   []string{"same a", "same b"},
			undecided: verdictOpen(),
		},
		"worse": {query: "verdict:worse", matches: []string{"worse"}, undecided: verdictOpen()},
		"only":  {query: "verdict:only", matches: []string{"lone"}, undecided: verdictOpen()},
		"mixed": {query: "verdict:mixed", matches: []string{"mixed a", "mixed b"}, undecided: verdictOpen()},
		"either": {
			query:     "verdict:better or verdict:worse",
			matches:   []string{"best", "worse"},
			undecided: verdictOpen(),
		},
		"a negated value": {
			query:     "is:weapon -verdict:mixed",
			matches:   []string{"best", "worse", "same a", "same b", "lone", "exotic", "unlisted"},
			undecided: verdictOpen(),
		},
	}.run(t, verdictContext())

	for _, query := range []string{
		"verdict:equal", "verdict:bett", "verdict:constructor", "verdict:__proto__", "pvpverdict:equal",
		"verdict:better+worse",
	} {
		assert.Equal(t, []string{query}, compiled(t, query).invalid, query)
	}

	assert.Equal(t, []string{modePvE}, compiled(t, "verdict:Better").modes)
}

func TestVerdictNoColumns(t *testing.T) {
	t.Parallel()

	ctx := verdictContext()

	for _, value := range []string{"better", "same", "worse", "only", "mixed"} {
		for _, query := range []string{"verdict:" + value, "pvpverdict:" + value} {
			matches, open := judge(t, query, ctx)

			for _, id := range []string{"exotic", "unlisted"} {
				assert.NotContains(t, matches, id, query)
				assert.NotContains(t, open, id, query)
			}
		}
	}
}

func TestVerdictUndecided(t *testing.T) {
	t.Parallel()

	ctx := verdictContext()

	tests := map[string]struct {
		query string
		id    string
		want  outcome
	}{
		"a copy without perks": {
			query: "verdict:only",
			id:    "no perks",
			want:  outcome{reasons: []string{reasonVerdictNoPerks}, state: undecided},
		},
		"a copy whose only rival has no perks": {
			query: "verdict:only",
			id:    "beside no perks",
			want:  outcome{reasons: []string{reasonVerdictNoPerks}, state: undecided},
		},
		"verdict:better is undecided too": {
			query: "verdict:better",
			id:    "beside no perks",
			want:  outcome{reasons: []string{reasonVerdictNoPerks}, state: undecided},
		},
		"misses: adds its own reason": {
			query: "verdict:worse or misses:>=0",
			id:    "no perks",
			want:  outcome{reasons: []string{reasonVerdictNoPerks, reasonNoPerks}, state: undecided},
		},
		"another term decides the copy": {
			query: "verdict:worse is:invault",
			id:    "no perks",
			want:  outcome{},
		},
		"a sheet that does not list the weapon decides it": {
			query: "pvpverdict:only",
			id:    "no perks",
			want:  outcome{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			i := slices.IndexFunc(ctx.items, func(item *Item) bool { return item.InstanceID == tc.id })
			require.GreaterOrEqual(t, i, 0)

			assert.Equal(t, tc.want, compiled(t, tc.query).test(ctx)(ctx.items[i]))
		})
	}
}

func TestPvPVerdict(t *testing.T) {
	t.Parallel()

	ctx := verdictContext()

	tests := map[string]struct {
		query string
		want  []string
		modes []string
	}{
		"better":  {query: "pvpverdict:better", want: []string{"mixed b"}, modes: []string{modePvP}},
		"worse":   {query: "pvpverdict:worse", want: []string{"mixed a"}, modes: []string{modePvP}},
		"only":    {query: "pvpverdict:only", want: []string{}, modes: []string{modePvP}},
		"same":    {query: "pvpverdict:same", want: []string{}, modes: []string{modePvP}},
		"mixed":   {query: "pvpverdict:mixed", want: []string{}, modes: []string{modePvP}},
		"verdict": {query: "verdict:mixed", want: []string{"mixed a", "mixed b"}, modes: []string{modePvE}},
		"each keyword reads its own sheet": {
			query: "verdict:mixed pvpverdict:better",
			want:  []string{"mixed b"},
			modes: []string{modePvE, modePvP},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.modes, compiled(t, tc.query).modes)
			assert.Equal(t, tc.want, contextMatchIDs(t, tc.query, ctx))
		})
	}
}

func TestRarityWords(t *testing.T) {
	t.Parallel()

	// The uncommon item displays the tier name Common, which DIM does not
	// read.
	items := []*Item{
		{InstanceID: "exotic helmet", Tier: "Exotic", TierType: bungie.TierTypeExotic, Bucket: bungie.BucketHelmet},
		{
			InstanceID: "legendary helmet",
			Tier:       "Legendary",
			TierType:   bungie.TierTypeLegendary,
			Bucket:     bungie.BucketHelmet,
		},
		{InstanceID: "exotic weapon", Tier: "Exotic", TierType: bungie.TierTypeExotic, Bucket: bungie.BucketKinetic},
		{InstanceID: "rare", Tier: "Rare", TierType: bungie.TierTypeRare},
		{InstanceID: "uncommon", Tier: "Common", TierType: bungie.TierTypeUncommon},
		{InstanceID: "common", Tier: "Basic", TierType: bungie.TierTypeBasic},
		{InstanceID: "unknown", TierType: bungie.TierTypeUnknown},
		{InstanceID: "currency", TierType: bungie.TierTypeCurrency},
		{InstanceID: "past the table", TierType: 9},
		{InstanceID: "negative", TierType: -1},
	}

	tests := map[string]struct {
		words []string
		want  []string
	}{
		"common":    {words: []string{"common", "white"}, want: []string{"common", "past the table", "negative"}},
		"uncommon":  {words: []string{"uncommon", "green"}, want: []string{"uncommon"}},
		"rare":      {words: []string{"rare", "blue"}, want: []string{"rare"}},
		"legendary": {words: []string{"legendary", "purple"}, want: []string{"legendary helmet"}},
		"exotic":    {words: []string{"exotic", "yellow"}, want: []string{"exotic helmet", "exotic weapon"}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			for _, word := range tc.words {
				assert.Equal(t, tc.want, matchIDs(t, "is:"+word, items), word)
			}
		})
	}

	assert.Equal(t, []string{"exotic helmet"}, matchIDs(t, "is:exotic is:armor", items))

	none := "-is:common -is:uncommon -is:rare -is:legendary -is:exotic"
	assert.Equal(t, []string{"unknown", "currency"}, matchIDs(t, none, items))
}

func TestElementWords(t *testing.T) {
	t.Parallel()

	var items []*Item
	for element := range 8 {
		items = append(items, &Item{InstanceID: strconv.Itoa(element), Element: element})
	}

	tests := map[string]struct {
		want []string
	}{
		"kinetic": {want: []string{"1"}},
		"arc":     {want: []string{"2"}},
		"solar":   {want: []string{"3"}},
		"void":    {want: []string{"4"}},
		"stasis":  {want: []string{"6"}},
		"strand":  {want: []string{"7"}},
		"light":   {want: []string{"2", "3", "4"}},
		"dark":    {want: []string{"6", "7"}},
	}

	for word, tc := range tests {
		t.Run(word, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, "is:"+word, items))
		})
	}

	none := "-is:kinetic -is:arc -is:solar -is:void -is:stasis -is:strand -is:light -is:dark"
	assert.Equal(t, []string{"0", "5"}, matchIDs(t, none, items))
}

func TestSlotWords(t *testing.T) {
	t.Parallel()

	// Each bucket is the BucketHashes value of DIM that the word reads.
	tests := map[string]struct {
		bucket uint32
	}{
		"engrams":           {bucket: 375726501},
		"lostitems":         {bucket: 215593132},
		"messages":          {bucket: 3161908920},
		"specialorders":     {bucket: 1367666825},
		"kineticslot":       {bucket: 1498876634},
		"energy":            {bucket: 2465295065},
		"power":             {bucket: 953998645},
		"helmet":            {bucket: 3448274439},
		"gauntlets":         {bucket: 3551918588},
		"chest":             {bucket: 14239492},
		"leg":               {bucket: 20886954},
		"classitem":         {bucket: 1585787867},
		"subclass":          {bucket: 3284755031},
		"seasonalartifacts": {bucket: 1506418338},
		"ghost":             {bucket: 4023194814},
		"emblems":           {bucket: 4274335291},
		"ships":             {bucket: 284967655},
		"vehicle":           {bucket: 2025709351},
		"emotes":            {bucket: 1107761855},
		"finishers":         {bucket: 3683254069},
		"accessories":       {bucket: 687325600},
		"consumables":       {bucket: 1469714392},
		"modifications":     {bucket: 3313201758},
	}

	// The lost weapon sits in the Lost Items bucket, and its definition
	// names the kinetic bucket. Each other item sits in the bucket of one
	// word and takes the word as its instance id.
	items := []*Item{{InstanceID: "lost weapon", Bucket: bungie.BucketKinetic, Lost: true, Postmaster: true}}
	for word, tc := range tests {
		items = append(items, &Item{InstanceID: word, Bucket: tc.bucket})
	}

	for word := range tests {
		t.Run(word, func(t *testing.T) {
			t.Parallel()

			want := []string{word}
			if word == "kineticslot" {
				want = []string{"lost weapon", word}
			}

			assert.Equal(t, want, matchIDs(t, "is:"+word, items))
		})
	}
}

func TestPowerRange(t *testing.T) {
	t.Parallel()

	// The item without power has a primary stat that DIM does not count.
	items := []*Item{
		{InstanceID: "1900", Power: 1900, LightPower: 1900},
		{InstanceID: "5", Power: 5, LightPower: 5},
		{InstanceID: "no power", Power: 2000},
	}

	tests := map[string]struct {
		value string
		want  []string
	}{
		"at least 10":                 {value: ">=10", want: []string{"1900"}},
		"fewer than 10":               {value: "<10", want: []string{"5"}},
		"fewer than 1900":             {value: "<1900", want: []string{"5"}},
		"equal to 1900":               {value: "1900", want: []string{"1900"}},
		"equal to 0":                  {value: "=0", want: []string{}},
		"more than 0":                 {value: ">0", want: []string{"1900", "5"}},
		"constructor":                 {value: "constructor", want: []string{}},
		"__proto__ after an operator": {value: ">=__proto__", want: []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, matchIDs(t, "power:"+tc.value, items), "power")
			assert.Equal(t, tc.want, matchIDs(t, "light:"+tc.value, items), "light")
		})
	}
}

func TestPowerRangeValues(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		query       string
		unsupported []string
		invalid     []string
	}{
		"pinnaclecap after an operator": {
			query:       "power:>=pinnaclecap",
			unsupported: []string{"power:>=pinnaclecap"},
		},
		"powerfloor":   {query: "power:powerfloor", unsupported: []string{"power:powerfloor"}},
		"softcap":      {query: "light:<softcap", unsupported: []string{"light:<softcap"}},
		"powerfulcap":  {query: "light:=powerfulcap", unsupported: []string{"light:=powerfulcap"}},
		"a word":       {query: "power:abc", invalid: []string{"power:abc"}},
		"a bad order":  {query: "power:=<10", invalid: []string{"power:=<10"}},
		"an empty one": {query: `light:""`, invalid: []string{`light:""`}},
		"pinnaclecap after a bad operator": {
			query:   "power:=<pinnaclecap",
			invalid: []string{"power:=<pinnaclecap"},
		},
		"a bad operator before an unsupported word": {
			query:   "power:>=pinnaclecap light:<>softcap",
			invalid: []string{"light:<>softcap"},
		},
		"a word that starts with pinnaclecap": {
			query:   "power:pinnaclecaps",
			invalid: []string{"power:pinnaclecaps"},
		},
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

func TestPowerValueErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		value string
		errs  []error
	}{
		"a season word": {
			value: "pinnaclecap",
			errs:  []error{errUnsupportedTerm, errOverloadValue},
		},
		"a season word after >=": {
			value: ">=pinnaclecap",
			errs:  []error{errUnsupportedTerm, errOverloadValue},
		},
		"a season word after a bad operator": {value: "=<pinnaclecap", errs: []error{errInvalidValue, errNotRange}},
		"a word outside the table":           {value: "abc", errs: []error{errInvalidValue, errNotRange}},
		"a number":                           {value: "<10"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			build, err := powerValue(tc.value)
			if tc.errs == nil {
				require.NoError(t, err)
				assert.NotNil(t, build)

				return
			}

			for _, want := range tc.errs {
				require.ErrorIs(t, err, want)
			}

			assert.Nil(t, build)
		})
	}
}

func TestHasPower(t *testing.T) {
	t.Parallel()

	items := []*Item{
		{InstanceID: "counted", Power: 1900, LightPower: 1900},
		{InstanceID: "not counted", Power: 2010},
		{InstanceID: "no primary stat"},
	}

	for _, query := range []string{"is:haspower", "is:haslight"} {
		assert.Equal(t, []string{"counted"}, matchIDs(t, query, items), query)
	}
}

func TestAutotagged(t *testing.T) {
	t.Parallel()

	s := fixtureSnapshot(t)
	owned := ownedItems(s)
	target := owned[0]

	matches := func(query string) []string {
		return ids(compiled(t, query).evaluate(searchContext{items: builtItems(s)}, owned).matches)
	}

	s.Annotate([]dvm.ItemTag{{InstanceID: target.InstanceID, TagID: dvm.TagJunk, RuleID: "rule-1"}}, nil)
	assert.Equal(t, []string{target.InstanceID}, matches("is:autotagged"))

	// A tag that a person sets carries no rule id.
	s.Annotate([]dvm.ItemTag{{InstanceID: target.InstanceID, TagID: dvm.TagJunk}}, nil)
	assert.Empty(t, matches("is:autotagged"))
	assert.Equal(t, []string{target.InstanceID}, matches("tag:junk"))
}

func TestDupeKeyRarity(t *testing.T) {
	t.Parallel()

	rare := comparableWeapon("rare", "Fatebringer")
	rare.TierType = bungie.TierTypeRare

	assert.Equal(t, []string{}, matchIDs(t, "is:dupe", []*Item{rare, comparableWeapon("legendary", "Fatebringer")}))

	renamed := comparableWeapon("renamed", "Fatebringer")
	renamed.Tier = "Superior"

	got := matchIDs(t, "is:dupe", []*Item{renamed, comparableWeapon("legendary", "Fatebringer")})
	assert.Equal(t, []string{"renamed", "legendary"}, got)
}

// Sandbox perk hashes of the set bonuses of the set bonus tests.
const (
	perkTwoPiece  uint32 = 1360604600
	perkFourPiece uint32 = 1360604601
	perkNoTier    uint32 = 1360604602
	perkUnrated   uint32 = 1360604603
)

// setBonusContext returns a search context whose armor covers the set
// bonus filters, keyed by instance id, in this order:
//
//   - set has a 2-piece bonus that the Aegis sheet rates C and a 4-piece
//     bonus that it rates S. The PvP sheet rates the 2-piece bonus A.
//   - untiered has a 4-piece bonus on an Aegis row without a tier.
//   - unrated has a bonus that neither sheet rates.
//   - no set has no item set.
//
// No sheet lists a weapon row for the armor, so only the set bonuses
// rate it.
func setBonusContext() searchContext {
	pve := &Sheet{
		Rows: map[uint32]SheetRow{100: {Tier: "S"}},
		SetBonuses: map[uint32]SetBonus{
			perkTwoPiece:  {Tier: "C", Pieces: 2},
			perkFourPiece: {Tier: "S", Pieces: 4},
			perkNoTier:    {Pieces: 4},
		},
	}
	pvp := &Sheet{
		Rows:       map[uint32]SheetRow{100: {Tier: "C"}},
		SetBonuses: map[uint32]SetBonus{perkTwoPiece: {Tier: "A", Pieces: 2}},
	}

	armor := func(id string, perks ...uint32) *Item {
		return &Item{
			InstanceID: id,
			Name:       id,
			Hash:       200,
			Bucket:     bungie.BucketHelmet,
			SetHash:    3727270518,
			SetKnown:   true,
			SetPerks:   perks,
		}
	}

	return searchContext{
		items: []*Item{
			armor("set", perkTwoPiece, perkFourPiece),
			armor("untiered", perkNoTier),
			armor("unrated", perkUnrated),
			{InstanceID: "no set", Name: "no set", Hash: 200, Bucket: bungie.BucketHelmet},
		},
		sources: map[string]RatingSource{
			modePvE: {Sheet: pve, Defs: &definitions.Defs{}},
			modePvP: {Sheet: pvp, Defs: &definitions.Defs{}},
		},
	}
}

func TestSetBonusValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		"s", "a", "b", "c", "d", "e", "f",
		"2pc:s", "2pc:a", "2pc:b", "2pc:c", "2pc:d", "2pc:e", "2pc:f",
		"4pc:s", "4pc:a", "4pc:b", "4pc:c", "4pc:d", "4pc:e", "4pc:f",
	}, setBonusQueries)

	tests := map[string]struct {
		query string
		valid bool
	}{
		"a tier":                        {query: "setbonus:s", valid: true},
		"a size and a tier":             {query: "setbonus:4pc:s", valid: true},
		"an uppercase value":            {query: "setbonus:2PC:F", valid: true},
		"the pvp form":                  {query: "pvpsetbonus:2pc:a", valid: true},
		"a size that DIM does not know": {query: "setbonus:3pc:s"},
		"a size without a tier":         {query: "setbonus:4pc"},
		"a tier outside S to F":         {query: "setbonus:x"},
		"a size after the tier":         {query: "setbonus:s:4pc"},
		"a range":                       {query: "setbonus:>=a"},
		"the pvp form of a bad size":    {query: "pvpsetbonus:3pc:s"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			q := compiled(t, tc.query)
			assert.Empty(t, q.unsupported)

			if tc.valid {
				assert.Empty(t, q.invalid)
				assert.NotNil(t, q.test)

				return
			}

			assert.Equal(t, []string{tc.query}, q.invalid)
		})
	}

	for _, value := range setBonusQueries {
		assert.Empty(t, compiled(t, "setbonus:"+value).invalid, value)
	}
}

func TestSetBonusMatch(t *testing.T) {
	t.Parallel()

	judgeTests{
		"a tier of either bonus":             {query: "setbonus:s", matches: []string{"set"}},
		"the tier of the 4-piece bonus":      {query: "setbonus:4pc:s", matches: []string{"set"}},
		"the 2-piece bonus has another tier": {query: "setbonus:2pc:s", matches: []string{}},
		"the tier of the 2-piece bonus":      {query: "setbonus:2pc:c", matches: []string{"set"}},
		"a tier without a bonus":             {query: "setbonus:a", matches: []string{}},
		"rate: tests each bonus":             {query: "rate:>=a", matches: []string{"set"}},
		"rate: matches the weaker bonus":     {query: "rate:c", matches: []string{"set"}},
		"ratebase: tests each bonus":         {query: "ratebase:>=a", matches: []string{"set"}},
		"rating: and ratingbase: too": {
			query:   "rating:s ratingbase:s",
			matches: []string{"set"},
		},
		"a bonus without a tier has no value": {query: "rate:>=f", matches: []string{"set"}},
		"no bonus passes":                     {query: "rate:<c", matches: []string{}},
		"misses: reads no set bonus":          {query: "misses:>=0", matches: []string{}},
		"a negated term decides the armor": {
			query:   "-setbonus:s",
			matches: []string{"untiered", "unrated", "no set"},
		},
	}.run(t, setBonusContext())
}

func TestSetBonusNone(t *testing.T) {
	t.Parallel()

	// The armor named helmet has no item set, and no sheet lists its hash.
	ctx := ratingContext()

	i := slices.IndexFunc(ctx.items, func(item *Item) bool { return item.InstanceID == "helmet" })
	require.GreaterOrEqual(t, i, 0)

	armor := ctx.items[i]
	require.Zero(t, armor.SetHash)

	tests := map[string]struct {
		query string
		want  outcome
	}{
		"rate: decides armor without a set":     {query: "rate:>=a", want: outcome{}},
		"ratebase: decides armor without a set": {query: "ratebase:>=a", want: outcome{}},
		"setbonus: decides armor without a set": {query: "setbonus:s", want: outcome{}},
		"a negated rate: matches":               {query: "-rate:>=a", want: outcome{state: match}},
		"is:armor or rate: matches":             {query: "rate:>=a or is:armor", want: outcome{state: match}},
		"is:weapon decides armor":               {query: "is:weapon rate:>=a", want: outcome{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, compiled(t, tc.query).test(ctx)(armor))
		})
	}

	result := compiled(t, "rate:>=a").evaluate(ctx, []*Item{armor})
	assert.Empty(t, result.matches)
	assert.Empty(t, result.undecided)
}

func TestPvPSetBonus(t *testing.T) {
	t.Parallel()

	ctx := setBonusContext()

	tests := map[string]struct {
		query string
		want  []string
		modes []string
	}{
		"pvpsetbonus: reads the PvP sheet": {query: "pvpsetbonus:a", want: []string{"set"}, modes: []string{modePvP}},
		"pvpsetbonus: takes a size": {
			query: "pvpsetbonus:2pc:a",
			want:  []string{"set"},
			modes: []string{modePvP},
		},
		"the PvP sheet rates no 4-piece bonus": {
			query: "pvpsetbonus:4pc:s",
			want:  []string{},
			modes: []string{modePvP},
		},
		"setbonus: reads the Aegis sheet":  {query: "setbonus:a", want: []string{}, modes: []string{modePvE}},
		"pvprate: tests the PvP set bonus": {query: "pvprate:a", want: []string{"set"}, modes: []string{modePvP}},
		"pvpratebase: tests the PvP bonus": {query: "pvpratebase:a", want: []string{"set"}, modes: []string{modePvP}},
		"pvprate: below the PvP set bonus": {query: "pvprate:<a", want: []string{}, modes: []string{modePvP}},
		"both modes in the order of modes": {
			query: "pvpsetbonus:a setbonus:s",
			want:  []string{"set"},
			modes: []string{modePvE, modePvP},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.modes, compiled(t, tc.query).modes)
			assert.Equal(t, tc.want, contextMatchIDs(t, tc.query, ctx))
		})
	}
}

// setSnapshot returns a snapshot whose Hunter holds three helmets. Helmet
// 2001 belongs to the item set 7, and helmet 2002 belongs to the item set
// 8. Helmet 1004 belongs to no set. The Hunter also holds a stack of a
// definition with the item set 8, which is not an owned item. Sets holds
// the item sets of the definitions, and listed records whether the
// service index lists the EquipableItemSet artifact.
func setSnapshot(t *testing.T, listed bool, sets map[uint32][]uint32) *Snapshot {
	t.Helper()

	const profile = `{
		"characters": {"data": {
			"c1": {"characterId": "c1", "classType": 1, "light": 2000, "dateLastPlayed": "2026-11-10T12:00:00Z"}
		}},
		"characterInventories": {"data": {"c1": {"items": [
			{"itemHash": 210, "itemInstanceId": "2001", "quantity": 1, "location": 1, "bucketHash": 3448274439},
			{"itemHash": 211, "itemInstanceId": "2002", "quantity": 1, "location": 1, "bucketHash": 3448274439},
			{"itemHash": 212, "quantity": 3, "location": 1, "bucketHash": 3448274439}
		]}}},
		"characterEquipment": {"data": {"c1": {"items": [
			{"itemHash": 200, "itemInstanceId": "1004", "quantity": 1, "location": 1, "bucketHash": 3448274439}
		]}}}
	}`

	helmet := func(name string, set uint32) definitions.ItemDef {
		return definitions.ItemDef{
			Name:       name,
			TypeName:   "Helmet",
			Bucket:     bungie.BucketHelmet,
			ItemType:   bungie.ItemTypeArmor,
			ClassType:  bungie.ClassHunter,
			Equippable: true,
			SetHash:    set,
		}
	}

	defs := definitionstest.Defs()
	defs.Items[210] = helmet("Iron Truage Helm", 7)
	defs.Items[211] = helmet("Wild Hunt Mask", 8)
	defs.Items[212] = helmet("Wild Hunt Stack", 8)
	defs.ItemSets = sets
	defs.ItemSetsListed = listed

	var p bungie.Profile

	require.NoError(t, json.Unmarshal([]byte(profile), &p))

	return NewSnapshot(&p, defs, bungietest.Now)
}

func TestBuiltItemsWithoutArtifact(t *testing.T) {
	t.Parallel()

	s := setSnapshot(t, false, map[uint32][]uint32{})
	assert.False(t, s.ItemSetsListed)

	assert.Equal(t, []string{"1004"}, ids(builtItems(s)))
	assert.Equal(t, []string{"1004"}, ids(ownedItems(s)))

	ctx := searchContext{items: builtItems(s)}
	assert.Equal(t, []string{"1004"}, contextMatchIDs(t, "is:armor", ctx))

	assert.Equal(t, "The server leaves out 2 owned items that belong to an item set, "+
		"because the service publishes no EquipableItemSet artifact "+
		"and the web client builds no such item without it.", unbuiltLine(s))

	// The index decides alone, so a set that the definitions hold does not
	// keep its item when the index lists no artifact.
	held := setSnapshot(t, false, map[uint32][]uint32{7: {perkTwoPiece}})
	assert.Equal(t, []string{"1004"}, ids(ownedItems(held)))
}

func TestBuiltItemsMissingSet(t *testing.T) {
	t.Parallel()

	s := setSnapshot(t, true, map[uint32][]uint32{7: {perkTwoPiece, perkFourPiece}})
	assert.True(t, s.ItemSetsListed)

	known, ok := s.Find("2001")
	require.True(t, ok)
	assert.Equal(t, uint32(7), known.SetHash)
	assert.True(t, known.SetKnown)
	assert.Equal(t, []uint32{perkTwoPiece, perkFourPiece}, known.SetPerks)

	missing, ok := s.Find("2002")
	require.True(t, ok)
	assert.Equal(t, uint32(8), missing.SetHash)
	assert.False(t, missing.SetKnown)
	assert.Nil(t, missing.SetPerks)

	assert.Equal(t, []string{"2001", "1004"}, ids(builtItems(s)))
	assert.Equal(t, []string{"2001", "1004"}, ids(ownedItems(s)))
	assert.Equal(t, "The server leaves out 1 owned items whose item set the EquipableItemSet definitions lack, "+
		"because the web client builds no such item.", unbuiltLine(s))

	every := setSnapshot(t, true, map[uint32][]uint32{7: {perkTwoPiece}, 8: {}})
	assert.Equal(t, []string{"2001", "2002", "1004"}, ids(ownedItems(every)))
	assert.Empty(t, unbuiltLine(every))
	assert.Empty(t, unbuiltLine(fixtureSnapshot(t)))
}

func TestUnbuiltLineSkipsDummyBuckets(t *testing.T) {
	t.Parallel()

	helmet := func(id string, bucket uint32, lost bool) *Item {
		return &Item{
			InstanceID: id,
			Name:       id,
			Bucket:     bucket,
			Equippable: true,
			Lost:       lost,
			SetHash:    8,
		}
	}

	// Every helmet belongs to a set that the definitions lack. The dummy
	// bucket rule already leaves out the helmets in the two dummy buckets,
	// so the line does not count them. The current bucket of the lost
	// helmet is the Lost Items bucket, so the line counts it.
	s := &Snapshot{
		ItemSetsListed: true,
		Items: []*Item{
			helmet("helmet", bungie.BucketHelmet, false),
			helmet("dummy", bungie.BucketDummy, false),
			helmet("forbidden", bungie.BucketForbidden, false),
			helmet("lost", bungie.BucketDummy, true),
		},
	}

	assert.Empty(t, ownedItems(s))
	assert.Equal(t, "The server leaves out 2 owned items whose item set the EquipableItemSet definitions lack, "+
		"because the web client builds no such item.", unbuiltLine(s))
}

func TestDupeSkipsUnbuilt(t *testing.T) {
	t.Parallel()

	helmet := func(id, name string, set uint32, known bool) *Item {
		return &Item{
			InstanceID: id,
			Name:       name,
			TierType:   bungie.TierTypeLegendary,
			ClassType:  bungie.ClassHunter,
			Bucket:     bungie.BucketHelmet,
			Equippable: true,
			Lockable:   true,
			SetHash:    set,
			SetKnown:   known,
		}
	}

	// Helmets 1 and 2 belong to a set that the definitions lack. Helmet 5
	// shares their name but belongs to no set. Helmets 3 and 4 belong to a
	// set that the definitions hold.
	s := &Snapshot{
		ItemSetsListed: true,
		Items: []*Item{
			helmet("1", "Wild Hunt Mask", 8, false),
			helmet("2", "Wild Hunt Mask", 8, false),
			helmet("5", "Wild Hunt Mask", 0, false),
			helmet("3", "Iron Truage Helm", 7, true),
			helmet("4", "Iron Truage Helm", 7, true),
		},
	}

	matches := func(query string) []string {
		return ids(compiled(t, query).evaluate(searchContext{items: builtItems(s)}, ownedItems(s)).matches)
	}

	assert.Equal(t, []string{"3", "4"}, matches("is:dupe"))
	assert.Equal(t, []string{"3", "4"}, matches("dupe:item"))
	assert.Equal(t, []string{"5"}, matches("count:1"))
	assert.Empty(t, matches("count:3"))
}
