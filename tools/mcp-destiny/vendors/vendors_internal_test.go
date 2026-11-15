package vendors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
)

func TestRead(t *testing.T) {
	t.Parallel()

	// Twelve vendors share a name. A walk over a map of twelve hashes
	// almost never yields them in hash order.
	shared := make(map[uint32]definitions.VendorDef, 12)
	order := make([]uint32, 0, 12)

	for hash := uint32(1); hash <= 12; hash++ {
		shared[hash] = definitions.VendorDef{Name: "X\u00fbr"}
		order = append(order, hash)
	}

	tests := map[string]struct {
		vendors map[uint32]definitions.VendorDef
		want    []uint32
	}{
		"vendors that share a name follow hash order": {
			vendors: shared,
			want:    order,
		},
		"name order before hash order": {
			vendors: map[uint32]definitions.VendorDef{
				1: {Name: "Zavala"},
				2: {Name: "Ada-1"},
			},
			want: []uint32{2, 1},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := &bungie.VendorsResponse{}
			resp.Vendors.Data = make(map[uint32]bungie.VendorComponent, len(tc.vendors))

			for hash := range tc.vendors {
				resp.Vendors.Data[hash] = bungie.VendorComponent{Hash: hash, Enabled: true}
			}

			got := Read(resp, &definitions.Defs{Vendors: tc.vendors})

			hashes := make([]uint32, 0, len(got))
			for _, v := range got {
				hashes = append(hashes, v.Hash)
			}

			assert.Equal(t, tc.want, hashes)
		})
	}
}

func TestReadGroups(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		groups []bungie.VendorGroup
		want   map[uint32]bool
	}{
		"no groups": {
			want: map[uint32]bool{1: false, 2: false, 3: false},
		},
		"vendors of several groups": {
			groups: []bungie.VendorGroup{
				{VendorHashes: []uint32{3}},
				{VendorHashes: []uint32{1, 4}},
			},
			want: map[uint32]bool{1: true, 2: false, 3: true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			resp := &bungie.VendorsResponse{}
			resp.VendorGroups.Data.Groups = tc.groups
			resp.Vendors.Data = map[uint32]bungie.VendorComponent{
				1: {Hash: 1, Enabled: true},
				2: {Hash: 2, Enabled: true},
			}
			resp.Sales.Data = map[uint32]bungie.VendorSales{3: {}}

			got := map[uint32]bool{}
			for _, v := range Read(resp, &definitions.Defs{}) {
				got[v.Hash] = v.InGroup
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGrouped(t *testing.T) {
	t.Parallel()

	sale := []SaleGroup{{Name: "Weapons", Sales: []Sale{{Name: "Fatebringer"}}}}
	empty := []SaleGroup{{Name: "Weapons"}}

	tests := map[string]struct {
		vendors []Vendor
		want    []uint32
	}{
		"no vendors": {},
		"grouped vendors with a sale in order": {
			vendors: []Vendor{
				{Hash: 2, InGroup: true, Groups: sale},
				{Hash: 1, InGroup: true, Groups: sale},
			},
			want: []uint32{2, 1},
		},
		"vendor outside the groups": {
			vendors: []Vendor{
				{Hash: 1, InGroup: true, Groups: sale},
				{Hash: 2, Groups: sale},
			},
			want: []uint32{1},
		},
		"grouped vendor without a sale": {
			vendors: []Vendor{
				{Hash: 1, InGroup: true},
				{Hash: 2, InGroup: true, Groups: empty},
				{Hash: 3, InGroup: true, Groups: sale, Disabled: true},
			},
			want: []uint32{3},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got []uint32
			for _, v := range Grouped(tc.vendors) {
				got = append(got, v.Hash)
			}

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSelect(t *testing.T) {
	t.Parallel()

	vendors := []Vendor{
		{Name: "Ada-1", Hash: 1},
		{Name: "ada-1", Hash: 2},
		{Name: "ADA-1", Hash: 3},
		{Name: "Ada-1 Prime", Hash: 4},
		{Name: "Saint-14", Hash: 5},
	}

	// Twelve vendors share a part of a name, which is more than the error
	// names.
	many := make([]Vendor, 0, 12)
	for i := range 12 {
		many = append(many, Vendor{Name: fmt.Sprintf("Ada-%d", i), Hash: uint32(i + 1)})
	}

	// The accented names write the u with a circumflex as the one precomposed
	// character U+00FB.
	accented := []Vendor{
		{Name: "X\u00fbr", Hash: 1},
		{Name: "X\u00fbr", Hash: 2},
		{Name: "Xurla", Hash: 3},
		{Name: "X\u00fbr Prime", Hash: 4},
	}

	// The game names two vendors M3-D1 Accessory Pack and S733k Pack. Each
	// writes digits in place of letters.
	digits := []Vendor{
		{Name: "M3-D1 Accessory Pack", Hash: 1},
		{Name: "S733k Pack", Hash: 2},
		{Name: "Saint-14", Hash: 3},
	}

	// The second and the third vendor write letters where the first one
	// writes digits.
	rivals := []Vendor{
		{Name: "M3-D1 Accessory Pack", Hash: 1},
		{Name: "Me-Di Accessory Pack", Hash: 2},
		{Name: "Me-Di Accessory Pack Prime", Hash: 3},
	}

	// The second and the third vendor write a letter where the first one
	// writes 7. The game writes the 7 of S733k Pack for l.
	sevens := []Vendor{
		{Name: "S733k Pack", Hash: 2},
		{Name: "Sleek Pack", Hash: 3},
		{Name: "Sveek Pack", Hash: 4},
	}

	tests := map[string]struct {
		arg     string
		err     string
		vendors []Vendor
		want    uint32
	}{
		"letters for the digits of a name": {
			arg:     "me-di accessory pack",
			vendors: digits,
			want:    1,
		},
		"letters for the digits of a part of a name": {
			arg:     "me-di",
			vendors: digits,
			want:    1,
		},
		"digits as the name writes them": {
			arg:     "m3-d1",
			vendors: digits,
			want:    1,
		},
		"letters for some digits of a name": {
			arg:     "s7eek pack",
			vendors: digits,
			want:    2,
		},
		"digits for the letters of a name": {
			arg:     "5a1nt-14",
			vendors: digits,
			want:    3,
		},
		"name in letters before a name with digits": {
			arg:     "me-di accessory pack",
			vendors: rivals,
			want:    2,
		},
		"name with digits before a name in letters": {
			arg:     "m3-d1 accessory pack",
			vendors: rivals,
			want:    1,
		},
		"part of a name in letters before a name with digits": {
			arg:     "me-di acc",
			vendors: rivals,
			err: `unknown vendor: "me-di acc" matches 2 vendors, so pass a vendor hash, ` +
				`such as Me-Di Accessory Pack (2), Me-Di Accessory Pack Prime (3)`,
		},
		"part of a name with digits before a name in letters": {
			arg:     "m3-d1 acc",
			vendors: rivals,
			want:    1,
		},
		"names that differ only in digits and letters": {
			arg:     "m3-di accessory pack",
			vendors: rivals,
			err: `unknown vendor: "m3-di accessory pack" matches 2 vendors, so pass a vendor hash, ` +
				`such as M3-D1 Accessory Pack (1), Me-Di Accessory Pack (2)`,
		},
		"letters for a seven that stands for l": {
			arg:     "sleek pack",
			vendors: digits,
			want:    2,
		},
		"letters for a part with a seven that stands for l": {
			arg:     "sleek",
			vendors: digits,
			want:    2,
		},
		"name in letters before a name with a seven for l": {
			arg:     "sleek pack",
			vendors: sevens,
			want:    3,
		},
		"part of a name in letters before a whole name with digits": {
			arg: "sleek pack",
			vendors: []Vendor{
				{Name: "S733k Pack", Hash: 2},
				{Name: "Sleek Pack Prime", Hash: 5},
			},
			want: 5,
		},
		"seven that reads as l and as v": {
			arg:     "s7eek pack",
			vendors: sevens[1:],
			err: `unknown vendor: "s7eek pack" matches 2 vendors, so pass a vendor hash, ` +
				`such as Sleek Pack (3), Sveek Pack (4)`,
		},
		"letters that no name writes": {
			arg:     "slick pack",
			vendors: digits,
			err:     `unknown vendor: no vendor of Hunter has a name that contains "slick pack"`,
		},
		"exact name before a name in another letter case": {
			arg:  "Ada-1",
			want: 1,
		},
		"name in another letter case before a part of a name": {
			arg: "aDa-1",
			err: `unknown vendor: "aDa-1" matches 3 vendors, so pass a vendor hash, ` +
				`such as Ada-1 (1), ada-1 (2), ADA-1 (3)`,
		},
		"part of a name": {
			arg:  "prime",
			want: 4,
		},
		"name without its accents before a part of a name": {
			arg:     "xur",
			vendors: accented,
			err: "unknown vendor: \"xur\" matches 2 vendors, so pass a vendor hash, " +
				"such as X\u00fbr (1), X\u00fbr (2)",
		},
		"name with combining accents": {
			arg:     "Xu\u0302r Prime",
			vendors: accented,
			want:    4,
		},
		"part of a name without its accents": {
			arg:     "xur p",
			vendors: accented,
			want:    4,
		},
		"hash": {
			arg:  "5",
			want: 5,
		},
		"unknown hash": {
			arg: "6",
			err: "unknown vendor: no vendor of Hunter has the hash 6",
		},
		"unknown name": {
			arg: "Zavala",
			err: `unknown vendor: no vendor of Hunter has a name that contains "Zavala"`,
		},
		"more matches than the error names": {
			arg:     "ada",
			vendors: many,
			err: `unknown vendor: "ada" matches 12 vendors, so pass a vendor hash, such as ` +
				`Ada-0 (1), Ada-1 (2), Ada-2 (3), Ada-3 (4), Ada-4 (5), ` +
				`Ada-5 (6), Ada-6 (7), Ada-7 (8), Ada-8 (9), Ada-9 (10)`,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := vendors
			if tc.vendors != nil {
				in = tc.vendors
			}

			got, err := Select(in, tc.arg, "Hunter")
			if tc.err != "" {
				require.ErrorIs(t, err, ErrUnknownVendor)
				assert.EqualError(t, err, tc.err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Hash)
		})
	}
}

func TestVendorItemName(t *testing.T) {
	t.Parallel()

	defs := &definitions.Defs{
		Items: map[uint32]definitions.ItemDef{
			1: {Name: "Glimmer"},
			2: {Name: "Ammo Synthesis (core)"},
		},
		VendorItems: map[uint32]string{
			2: "Ammo Synthesis",
			4: "Legendary Shards",
		},
	}

	tests := map[string]struct {
		want string
		hash uint32
	}{
		"vendor item name first": {
			hash: 2,
			want: "Ammo Synthesis",
		},
		"vendor item name only": {
			hash: 4,
			want: "Legendary Shards",
		},
		"item definition when the vendor items lack the hash": {
			hash: 1,
			want: "Glimmer",
		},
		"no name": {
			hash: 5,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, vendorItemName(defs, tc.hash))
		})
	}
}
