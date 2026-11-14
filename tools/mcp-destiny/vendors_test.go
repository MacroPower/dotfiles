package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

func TestReadVendors(t *testing.T) {
	t.Parallel()

	// Twelve vendors share a name. A walk over a map of twelve hashes
	// almost never yields them in hash order.
	shared := make(map[uint32]VendorDef, 12)
	order := make([]uint32, 0, 12)

	for hash := uint32(1); hash <= 12; hash++ {
		shared[hash] = VendorDef{Name: "Xûr"}
		order = append(order, hash)
	}

	tests := map[string]struct {
		vendors map[uint32]VendorDef
		want    []uint32
	}{
		"vendors that share a name follow hash order": {
			vendors: shared,
			want:    order,
		},
		"name order before hash order": {
			vendors: map[uint32]VendorDef{
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

			got := readVendors(resp, &Defs{Vendors: tc.vendors})

			hashes := make([]uint32, 0, len(got))
			for _, v := range got {
				hashes = append(hashes, v.Hash)
			}

			assert.Equal(t, tc.want, hashes)
		})
	}
}

func TestSelectVendor(t *testing.T) {
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

	tests := map[string]struct {
		arg     string
		err     string
		vendors []Vendor
		want    uint32
	}{
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

			got, err := selectVendor(in, tc.arg, "Hunter")
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

	defs := &Defs{
		Items: map[uint32]ItemDef{
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
