package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Values of the Bungie VendorItemStatus flags. A status of zero means that
// the character can buy the item. The DisplayOnly bit marks an item that
// the vendor shows but does not sell.
const (
	saleSuccess     = 0
	saleDisplayOnly = 8192
)

// uncategorized heads the group of the sales that no category of a vendor
// lists.
const uncategorized = "Uncategorized"

// ErrUnknownVendor reports a vendor argument that selects no vendor of the
// character, or that selects more than one.
var ErrUnknownVendor = errors.New("unknown vendor")

// Vendor is one vendor of a character, with the stock that Bungie reports
// joined to the definitions. Name is the name of the vendor definition,
// or "Vendor <hash>" when the definitions do not name the vendor.
// NextRefresh is the time when the stock rotates next, and it is zero when
// Bungie reports no time. Disabled marks a vendor that Bungie reports as
// not accessible in the game.
type Vendor struct {
	NextRefresh time.Time
	Name        string
	Subtitle    string
	Groups      []SaleGroup
	Hash        uint32
	Disabled    bool
}

// SaleCount returns the number of sales in the groups of the vendor.
func (v Vendor) SaleCount() int {
	n := 0

	for _, group := range v.Groups {
		n += len(group.Sales)
	}

	return n
}

// SaleGroup is one group of the sales of a vendor. Each category group
// holds the sales that its display category lists, in display order. The
// Uncategorized group holds the sales that no category lists, in vendor
// item index order.
type SaleGroup struct {
	Name  string
	Sales []Sale
}

// Sale is one item that a vendor sells. Available is true when the
// character can buy the item. Reasons holds the failure strings of the
// vendor definition that explain why the character cannot buy an
// unavailable item.
type Sale struct {
	Name      string
	Costs     []Cost
	Reasons   []string
	Quantity  int
	ItemHash  uint32
	Available bool
}

// Cost is the quantity of one currency that a sale costs.
type Cost struct {
	Name     string
	Quantity int
	ItemHash uint32
}

// readVendors joins a GetVendors response with the definitions. It returns
// every vendor that the response lists, ordered by name and then by hash.
func readVendors(resp *VendorsResponse, defs *Defs) []Vendor {
	hashes := map[uint32]bool{}

	for hash := range resp.Vendors.Data {
		hashes[hash] = true
	}

	// No vendor group lists the sub-vendors that hold the stock of
	// Eververse, so only the keys of the sales name them.
	for hash := range resp.Sales.Data {
		hashes[hash] = true
	}

	vendors := make([]Vendor, 0, len(hashes))
	for hash := range hashes {
		vendors = append(vendors, newVendor(resp, defs, hash))
	}

	slices.SortFunc(vendors, func(a, b Vendor) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Hash, b.Hash))
	})

	return vendors
}

// newVendor joins the response entries of one vendor with its definition.
// It groups the sales by the categories of the response, ordered by their
// display category index. The sales that no category lists follow in a
// group of their own, ordered by vendor item index.
func newVendor(resp *VendorsResponse, defs *Defs, hash uint32) Vendor {
	def := defs.Vendors[hash]

	v := Vendor{Hash: hash, Name: def.Name, Subtitle: def.Subtitle}
	if v.Name == "" {
		v.Name = fmt.Sprintf("Vendor %d", hash)
	}

	if live, ok := resp.Vendors.Data[hash]; ok {
		v.NextRefresh = live.NextRefreshDate
		v.Disabled = !live.Enabled
	}

	sales := map[int]Sale{}

	for index, raw := range resp.Sales.Data[hash].SaleItems {
		sale, ok := newSale(defs, def, raw)
		if ok {
			sales[index] = sale
		}
	}

	categories := slices.Clone(resp.Categories.Data[hash].Categories)
	slices.SortStableFunc(categories, func(a, b VendorCategory) int {
		return cmp.Compare(a.DisplayCategoryIndex, b.DisplayCategoryIndex)
	})

	listed := map[int]bool{}

	for _, category := range categories {
		group := SaleGroup{Name: def.Categories[category.DisplayCategoryIndex]}
		if group.Name == "" {
			group.Name = fmt.Sprintf("Category %d", category.DisplayCategoryIndex)
		}

		for _, index := range category.ItemIndexes {
			sale, ok := sales[index]
			if !ok {
				continue
			}

			listed[index] = true

			group.Sales = append(group.Sales, sale)
		}

		if len(group.Sales) > 0 {
			v.Groups = append(v.Groups, group)
		}
	}

	var rest []int

	for index := range sales {
		if !listed[index] {
			rest = append(rest, index)
		}
	}

	if len(rest) > 0 {
		slices.Sort(rest)

		group := SaleGroup{Name: uncategorized}
		for _, index := range rest {
			group.Sales = append(group.Sales, sales[index])
		}

		v.Groups = append(v.Groups, group)
	}

	return v
}

// newSale joins one sale with the definitions. It reports false for a
// sale that the vendor displays without selling the item. It also reports
// false for an item that the definitions do not name. The web client
// leaves out both sales.
func newSale(defs *Defs, vendor VendorDef, raw VendorSale) (Sale, bool) {
	if raw.SaleStatus&saleDisplayOnly != 0 {
		return Sale{}, false
	}

	name := vendorItemName(defs, raw.ItemHash)
	if name == "" {
		return Sale{}, false
	}

	sale := Sale{
		Name:      name,
		ItemHash:  raw.ItemHash,
		Quantity:  raw.Quantity,
		Available: raw.SaleStatus == saleSuccess,
	}

	for _, cost := range raw.Costs {
		if cost.ItemHash == 0 || cost.Quantity == 0 {
			continue
		}

		currency := vendorItemName(defs, cost.ItemHash)
		if currency == "" {
			currency = fmt.Sprintf("Item %d", cost.ItemHash)
		}

		sale.Costs = append(sale.Costs, Cost{Name: currency, Quantity: cost.Quantity, ItemHash: cost.ItemHash})
	}

	if sale.Available {
		return sale, true
	}

	for _, at := range raw.FailureIndexes {
		if at >= 0 && at < len(vendor.FailureStrings) && vendor.FailureStrings[at] != "" {
			sale.Reasons = append(sale.Reasons, vendor.FailureStrings[at])
		}
	}

	return sale, true
}

// vendorItemName names an item that a vendor sells or charges. It reads
// the vendor item names first and the item definitions second, in the
// order of the web client. Many vendors sell dummy items, and the item
// definitions leave them out. It returns an empty string when neither
// names the item.
func vendorItemName(defs *Defs, hash uint32) string {
	if name := defs.VendorItems[hash]; name != "" {
		return name
	}

	return defs.Items[hash].Name
}

// selectVendor returns the vendor that arg selects among vendors. A
// decimal arg selects the vendor with that hash. Any other arg selects by
// name. An exact name comes first, then a name that matches in any letter
// case, then a name that contains arg in any letter case. Several vendors
// that match at the first rank with a match return an error that names
// some of them with their hashes. Owner names the character for the
// error that an unknown vendor returns.
func selectVendor(vendors []Vendor, arg, owner string) (Vendor, error) {
	arg = strings.TrimSpace(arg)

	hash, err := strconv.ParseUint(arg, 10, 32)
	if err == nil {
		for _, v := range vendors {
			if uint64(v.Hash) == hash {
				return v, nil
			}
		}

		return Vendor{}, fmt.Errorf("%w: no vendor of %s has the hash %d", ErrUnknownVendor, owner, hash)
	}

	lower := strings.ToLower(arg)

	var exact, folded, similar []Vendor

	for _, v := range vendors {
		switch {
		case v.Name == arg:
			exact = append(exact, v)
		case strings.EqualFold(v.Name, arg):
			folded = append(folded, v)
		case containsFold(v.Name, lower):
			similar = append(similar, v)
		}
	}

	for _, matches := range [][]Vendor{exact, folded, similar} {
		switch len(matches) {
		case 0:
			continue
		case 1:
			return matches[0], nil
		}

		names := make([]string, 0, min(len(matches), maxSuggestions))
		for _, v := range matches[:min(len(matches), maxSuggestions)] {
			names = append(names, fmt.Sprintf("%s (%d)", v.Name, v.Hash))
		}

		return Vendor{}, fmt.Errorf(
			"%w: %q matches %d vendors, so pass a vendor hash, such as %s",
			ErrUnknownVendor, arg, len(matches), strings.Join(names, ", "),
		)
	}

	return Vendor{}, fmt.Errorf("%w: no vendor of %s has a name that contains %q", ErrUnknownVendor, owner, arg)
}
