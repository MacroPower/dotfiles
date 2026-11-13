package main

import (
	"encoding/json"
	"fmt"
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

// matchIDs returns the instance id of each item that query matches, in
// the order of items. Every term of query must be supported.
func matchIDs(t *testing.T, query string, items []*Item) []string {
	t.Helper()

	q := compiled(t, query)
	require.Empty(t, q.unsupported)
	require.NotNil(t, q.test)

	out := []string{}

	for _, item := range items {
		if q.test(item) {
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
	owned := ownedItems(s)

	tests := map[string]struct {
		id   string
		hash uint32
		want bool
	}{
		"weapon on a character":              {hash: 102, id: "1003", want: true},
		"weapon in the vault":                {hash: 100, id: "1001", want: true},
		"weapon at the postmaster":           {hash: 100, id: "1005", want: true},
		"armor piece":                        {hash: 200, id: "1004", want: true},
		"lost weapon of the dummy bucket":    {hash: 403, id: "2006", want: true},
		"stack":                              {hash: 300},
		"stack of an equippable definition":  {hash: 100},
		"instanced item without equipping":   {hash: 400, id: "2001"},
		"item without a definition":          {hash: 999, id: "2008"},
		"emote":                              {hash: 401, id: "2002"},
		"finisher":                           {hash: 402, id: "2003"},
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

			assert.Equal(t, tc.want, slices.Contains(owned, s.Items[i]))
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
		"is:primary", "is:special", "is:heavy", "tag:",
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
		"is:weaponmod, is:armormod, is:reptoken, is:primary, is:special, is:heavy, and tag:. "+
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
		{InstanceID: "20", Name: "Gjallarhorn", Bucket: bucketPower},
		{InstanceID: "100", Name: "Fatebringer", Bucket: bucketKinetic},
		{InstanceID: "9", Name: "Fatebringer", Bucket: bucketKinetic},
		{InstanceID: "5", Name: "Celestial Nighthawk", Bucket: bucketHelmet},
	}

	assert.Equal(t, []string{"9", "100", "20"}, ids(compiled(t, "is:weapon").matches(items)))
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
	for _, item := range q.matches(items) {
		names = append(names, item.Name)
	}

	return names
}

func TestWeaponTypeWords(t *testing.T) {
	t.Parallel()

	items := weaponTypeItems(t)

	assert.Equal(t, []string{"Ace of Spades", "Fatebringer"}, matchNames(t, "is:handcannon", items))

	for _, item := range compiled(t, "is:handcannon").matches(items) {
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
