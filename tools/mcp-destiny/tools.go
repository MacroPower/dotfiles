package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP tool names. The registration in main.go, the handlers, and the log
// entries share these constants.
const (
	toolStatus              = "status"
	toolLogin               = "login"
	toolCompleteLogin       = "complete_login"
	toolListCharacters      = "list_characters"
	toolListItems           = "list_items"
	toolGetItem             = "get_item"
	toolCompareDuplicates   = "compare_duplicates"
	toolMoveItems           = "move_items"
	toolMoveStack           = "move_stack"
	toolSetLock             = "set_lock"
	toolSetPerk             = "set_perk"
	toolListLoadouts        = "list_loadouts"
	toolEquipLoadout        = "equip_loadout"
	toolListTags            = "list_tags"
	toolSetTag              = "set_tag"
	toolSetFavorite         = "set_favorite"
	toolListNotes           = "list_notes"
	toolSetNote             = "set_note"
	toolListPerkPreferences = "list_perk_preferences"
	toolSetPerkPreferences  = "set_perk_preferences"
	toolListAutotagRules    = "list_autotag_rules"
	toolSaveAutotagRule     = "save_autotag_rule"
	toolRemoveAutotagRule   = "remove_autotag_rule"
	toolSetAutotagEnabled   = "set_autotag_enabled"
	toolListActivityHistory = "list_activity_history"
	toolListVendors         = "list_vendors"
	toolListActivities      = "list_activities"
	toolGetStats            = "get_stats"
)

// Note kinds that the service stores.
const (
	noteInstance = "instance"
	noteItem     = "item"
	noteSet      = "set"
)

const (
	// snapshotTTL is how long the handlers reuse one Bungie profile.
	snapshotTTL = 30 * time.Second

	// configTTL is how long the handlers reuse the Bungie API key.
	configTTL = 10 * time.Minute

	defaultItemLimit = 50
	maxItemLimit     = 200

	defaultWeaponLimit = 10
	maxWeaponLimit     = 50

	defaultHistoryLimit = 25

	// maxHistoryLimit is the page size that the web client requests from
	// the activity history.
	maxHistoryLimit = 250

	// maxBatch caps the instance ids of one write. The service rejects a
	// tag write of more than 1000 rows, and an item contributes at most a
	// few rows.
	maxBatch = 200

	maxNoteLength  = 2000
	maxQueryLength = 1000

	stanceNone = "none"

	// targetInventory is the move_stack target that names the side of a
	// stack outside the vault.
	targetInventory = "inventory"
)

var (
	// perkModes lists the game modes in display order. The perk
	// preferences and the roll sheets share the list.
	perkModes   = []string{modePvE, modePvP}
	perkStances = []string{"up", "down"}

	// ErrInvalidInput reports a tool argument that the handler rejects
	// before any request.
	ErrInvalidInput = errors.New("invalid input")
)

// EmptyInput is the input schema of the tools that take no arguments.
type EmptyInput struct{}

// CompleteLoginInput is the input schema of the complete_login tool.
type CompleteLoginInput struct {
	RedirectURL string `json:"redirect_url" jsonschema:"Full URL from the browser address bar after Bungie redirected to Agg's Destiny Vault Manager"`
}

// ListItemsInput is the input schema of the list_items tool.
type ListItemsInput struct {
	Query        string `json:"query,omitzero"         jsonschema:"Case-insensitive substring of the item name or type, such as 'fatebringer' or 'hand cannon'"`
	Kind         string `json:"kind,omitzero"          jsonschema:"weapon, armor, gear (weapons and armor, the default), other, or all"`
	Slot         string `json:"slot,omitzero"          jsonschema:"Substring of the slot name, such as 'kinetic', 'energy', 'power', 'helmet', or 'class armor'"`
	Tier         string `json:"tier,omitzero"          jsonschema:"Rarity, such as 'exotic' or 'legendary'"`
	Class        string `json:"class,omitzero"         jsonschema:"Class that armor belongs to: hunter, titan, or warlock"`
	Damage       string `json:"damage,omitzero"        jsonschema:"Damage type, such as 'kinetic', 'arc', 'solar', 'void', 'stasis', or 'strand'"`
	Location     string `json:"location,omitzero"      jsonschema:"vault, postmaster, equipped, characters (any character), a character id, or a class name"`
	Tag          string `json:"tag,omitzero"           jsonschema:"Tag id from list_tags, 'none' for untagged items, or 'any' for tagged items"`
	Perk         string `json:"perk,omitzero"          jsonschema:"Case-insensitive substring of a perk the item has equipped, such as 'kill clip'"`
	Stat         string `json:"stat,omitzero"          jsonschema:"Case-insensitive full name of one stat, such as 'health' for armor or 'range' for weapons. Adds a column with its value, and min_stat and sort=stat read it"`
	Sort         string `json:"sort,omitzero"          jsonschema:"name (default), power (highest first), newest (most recently acquired first), total (highest armor stat total first), or stat (highest value of the stat that stat names first)"`
	MinPower     int    `json:"min_power,omitzero"     jsonschema:"Lowest power level to include"`
	MaxPower     int    `json:"max_power,omitzero"     jsonschema:"Highest power level to include"`
	MinTotal     int    `json:"min_total,omitzero"     jsonschema:"Lowest armor stat total to include. Only armor has a stat total, so a positive value returns only armor"`
	MinStat      int    `json:"min_stat,omitzero"      jsonschema:"Lowest value of the stat that stat names. Items that lack the stat do not match"`
	Limit        int    `json:"limit,omitzero"         jsonschema:"Maximum number of items to return (default 50, maximum 200)"`
	Offset       int    `json:"offset,omitzero"        jsonschema:"Number of matching items to skip (default 0)"`
	Favorite     bool   `json:"favorite,omitzero"      jsonschema:"Return only favorites"`
	IncludePerks bool   `json:"include_perks,omitzero" jsonschema:"Add a column with the equipped perks of each item"`
	Refresh      bool   `json:"refresh,omitzero"       jsonschema:"Fetch the inventory from Bungie again instead of reusing a copy up to 30 seconds old"`
}

// GetItemInput is the input schema of the get_item tool.
type GetItemInput struct {
	InstanceID string `json:"instance_id" jsonschema:"Instance id from list_items"`
}

// CompareDuplicatesInput is the input schema of the compare_duplicates
// tool. Without a name or an instance id, the tool compares every weapon
// that has more than one copy, a page at a time.
type CompareDuplicatesInput struct {
	Name       string `json:"name,omitzero"        jsonschema:"Weapon name in any letter case, such as 'Fatebringer'. A name that no weapon has selects the one weapon whose name contains it"`
	InstanceID string `json:"instance_id,omitzero" jsonschema:"Instance id of one copy, from list_items"`
	Limit      int    `json:"limit,omitzero"       jsonschema:"Without name and instance_id: maximum number of weapons to return (default 10, maximum 50)"`
	Offset     int    `json:"offset,omitzero"      jsonschema:"Without name and instance_id: number of weapons to skip (default 0)"`
}

// ListActivityHistoryInput is the input schema of the
// list_activity_history tool.
type ListActivityHistoryInput struct {
	Character string `json:"character,omitzero" jsonschema:"Character id, or a class name that exactly one character has. Omit to list the runs of every character"`
	Limit     int    `json:"limit,omitzero"     jsonschema:"Maximum number of runs to return, newest first (default 25, maximum 250)"`
}

// ListVendorsInput is the input schema of the list_vendors tool. Without
// a vendor, the tool lists the vendors of the character.
type ListVendorsInput struct {
	Character string `json:"character,omitzero" jsonschema:"Character id, or a class name that exactly one character has. Omit to read the most recently played character"`
	Vendor    string `json:"vendor,omitzero"    jsonschema:"Vendor hash, or a vendor name in any letter case, such as 'Banshee-44'. A name that no vendor has selects the one vendor whose name contains it. Omit to list the vendors"`
}

// ListActivitiesInput is the input schema of the list_activities tool.
type ListActivitiesInput struct {
	Character string `json:"character,omitzero" jsonschema:"Character id, or a class name that exactly one character has. Omit to read the most recently played character"`
	Query     string `json:"query,omitzero"     jsonschema:"Case-insensitive substring of the activity name, the activity type, the location, the focused reward, or a bonus loot name, such as 'raid' or 'deepsight'"`
	Limit     int    `json:"limit,omitzero"     jsonschema:"Maximum number of activities to return (default 50, maximum 200)"`
	Offset    int    `json:"offset,omitzero"    jsonschema:"Number of matching activities to skip (default 0)"`
	Rewards   bool   `json:"rewards,omitzero"   jsonschema:"Return only the activities that still offer bonus drops, a focused reward, or bonus loot this week"`
}

// MoveItemsInput is the input schema of the move_items tool.
type MoveItemsInput struct {
	Target      string   `json:"target"         jsonschema:"Destination: 'vault', a character id, or a class name that exactly one character has"`
	InstanceIDs []string `json:"instance_ids"   jsonschema:"Instance ids from list_items"`
	Equip       bool     `json:"equip,omitzero" jsonschema:"Equip each item on the target character after the transfer"`
}

// MoveStackInput is the input schema of the move_stack tool.
type MoveStackInput struct {
	Target   string `json:"target"          jsonschema:"Destination: 'vault' or 'inventory'"`
	ItemHash uint32 `json:"item_hash"       jsonschema:"Item hash of the stack, from the list_items row"`
	Amount   int    `json:"amount,omitzero" jsonschema:"Number of units to move (default: every unit outside the destination)"`
}

// SetLockInput is the input schema of the set_lock tool.
type SetLockInput struct {
	InstanceIDs []string `json:"instance_ids" jsonschema:"Instance ids from list_items"`
	Locked      bool     `json:"locked"       jsonschema:"true to lock the items, false to unlock them"`
}

// SetPerkInput is the input schema of the set_perk tool.
type SetPerkInput struct {
	InstanceID string `json:"instance_id"      jsonschema:"Instance id from list_items"`
	Perk       string `json:"perk"             jsonschema:"Perk to make active in any letter case, such as 'Rampage'. get_item lists the perks that each socket can swap to"`
	Replace    string `json:"replace,omitzero" jsonschema:"Active perk to swap out, in any letter case. Pass it when more than one socket can swap to the perk"`
}

// ListLoadoutsInput is the input schema of the list_loadouts tool.
type ListLoadoutsInput struct {
	Character string `json:"character,omitzero" jsonschema:"Character id, or a class name that exactly one character has. Omit to list the loadouts of every character"`
}

// EquipLoadoutInput is the input schema of the equip_loadout tool.
type EquipLoadoutInput struct {
	Character string `json:"character" jsonschema:"Character id, or a class name that exactly one character has"`
	Loadout   int    `json:"loadout"   jsonschema:"Loadout number from list_loadouts, which counts the in-game loadout screen from 1"`
}

// SetAutotagEnabledInput is the input schema of the set_autotag_enabled
// tool.
type SetAutotagEnabledInput struct {
	Enabled bool `json:"enabled" jsonschema:"true to turn autotagging on, false to turn it off"`
}

// SetTagInput is the input schema of the set_tag tool.
type SetTagInput struct {
	Tag         string   `json:"tag"          jsonschema:"Tag id from list_tags, or 'none' to clear the tag"`
	InstanceIDs []string `json:"instance_ids" jsonschema:"Instance ids from list_items"`
}

// SetFavoriteInput is the input schema of the set_favorite tool.
type SetFavoriteInput struct {
	InstanceIDs []string `json:"instance_ids" jsonschema:"Instance ids from list_items"`
	Favorite    bool     `json:"favorite"     jsonschema:"true to mark the items as favorites, false to clear the mark"`
}

// SetNoteInput is the input schema of the set_note tool.
type SetNoteInput struct {
	TargetID string `json:"target_id"     jsonschema:"Instance id for an instance note, or the Collections key from list_notes for an item or set note"`
	Body     string `json:"body"          jsonschema:"Note text of at most 2000 characters. An empty body removes the note"`
	Kind     string `json:"kind,omitzero" jsonschema:"instance (default), item, or set"`
}

// PerkPreferenceInput is one entry of the set_perk_preferences tool input.
type PerkPreferenceInput struct {
	Mode   string `json:"mode"   jsonschema:"pve or pvp"`
	Perk   string `json:"perk"   jsonschema:"Weapon trait name in any letter case, such as 'Kill Clip'. An enhanced trait shares the preference of its base trait"`
	Stance string `json:"stance" jsonschema:"up (liked), down (disliked), or none to clear the preference"`
}

// SetPerkPreferencesInput is the input schema of the set_perk_preferences
// tool.
type SetPerkPreferencesInput struct {
	Preferences []PerkPreferenceInput `json:"preferences" jsonschema:"Preferences to set or clear"`
}

// SaveAutotagRuleInput is the input schema of the save_autotag_rule tool.
// A nil field keeps the stored value on an update.
type SaveAutotagRuleInput struct {
	Query    *string `json:"query,omitempty"    jsonschema:"Item search in the search syntax of Agg's Destiny Vault Manager, such as 'is:weapon misses:2 is:dupe'. Required for a new rule"`
	TagID    *string `json:"tag_id,omitempty"   jsonschema:"Tag id that the rule applies. An empty string makes the rule apply no tag, and the rule then needs vault=true"`
	Vault    *bool   `json:"vault,omitempty"    jsonschema:"Move matching items to the vault (default false)"`
	Enabled  *bool   `json:"enabled,omitempty"  jsonschema:"Whether the rule runs (default true)"`
	Position *int    `json:"position,omitempty" jsonschema:"Evaluation order, lowest first. A new rule defaults to last"`
	ID       string  `json:"id,omitzero"        jsonschema:"Id of the rule to update, from list_autotag_rules. Omit to create a rule"`
}

// RemoveAutotagRuleInput is the input schema of the remove_autotag_rule
// tool.
type RemoveAutotagRuleInput struct {
	ID string `json:"id" jsonschema:"Rule id from list_autotag_rules"`
}

// GetStatsInput is the input schema of the get_stats tool.
type GetStatsInput struct {
	Period string `json:"period,omitzero" jsonschema:"24h (the last 24 hours, the default), 7d, 30d, or all (every recorded change)"`
}

// handler holds the shared state of the tool handlers. The mutex serializes
// tool calls, because moves, lock changes, perk changes, and loadout
// equips update the cached snapshot in place. The sheet store has no lock
// of its own, so the same mutex guards it.
type handler struct {
	service  *Service
	bungie   *Bungie
	auth     *Auth
	defs     *DefsLoader
	sheets   *SheetStore
	log      *slog.Logger
	now      func() time.Time
	snapshot *Snapshot
	apiKeyAt time.Time
	apiKey   string
	mu       sync.Mutex
}

func (h *handler) handleStatus(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	t, err := h.auth.Saved()
	if err != nil && !errors.Is(err, ErrNotSignedIn) {
		return h.toolError(ctx, toolStatus, err)
	}

	text := renderStatus(t, err == nil, h.service.BaseURL(), h.auth.store.Path(), h.now())

	return textResult(text), nil, nil
}

func (h *handler) handleLogin(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	u, err := h.auth.LoginURL(ctx)
	if err != nil {
		return h.toolError(ctx, toolLogin, err)
	}

	text := "Ask the user to do these steps:\n\n" +
		"1. Open " + u + " and approve access.\n" +
		"2. Bungie redirects to Agg's Destiny Vault Manager, which shows \"OAuth state mismatch\". " +
		"That message is expected, because this sign-in belongs to the MCP server.\n" +
		"3. Copy the full URL from the address bar.\n\n" +
		"Then pass that URL to the complete_login tool."

	return textResult(text), nil, nil
}

func (h *handler) handleCompleteLogin(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in CompleteLoginInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	t, err := h.auth.CompleteLogin(ctx, in.RedirectURL)
	if err != nil {
		return h.toolError(ctx, toolCompleteLogin, err)
	}

	// The cached snapshot might belong to a different account.
	h.snapshot = nil

	text := "Signed in as Bungie.net membership " + t.BungieID + "."
	if !t.RefreshExpires.IsZero() {
		text += " The sign-in lasts until " + t.RefreshExpires.Format(timeLayout) + "."
	}

	return textResult(text), nil, nil
}

func (h *handler) handleListCharacters(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListCharacters, err)
	}

	return textResult(renderCharacters(s)), nil, nil
}

func (h *handler) handleListItems(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ListItemsInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, _, err := h.loadAnnotated(ctx, in.Refresh)
	if err != nil {
		return h.toolError(ctx, toolListItems, err)
	}

	items, err := s.Filter(ItemFilter{
		Query:    in.Query,
		Kind:     in.Kind,
		Slot:     in.Slot,
		Tier:     in.Tier,
		Class:    in.Class,
		Damage:   in.Damage,
		Location: in.Location,
		Tag:      in.Tag,
		Perk:     in.Perk,
		Stat:     in.Stat,
		Sort:     in.Sort,
		MinPower: in.MinPower,
		MaxPower: in.MaxPower,
		MinTotal: in.MinTotal,
		MinStat:  in.MinStat,
		Favorite: in.Favorite,
	})
	if err != nil {
		return h.toolError(ctx, toolListItems, err)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultItemLimit
	}

	limit = min(limit, maxItemLimit)
	offset := min(max(in.Offset, 0), len(items))
	page := items[offset:min(offset+limit, len(items))]

	return textResult(renderItems(s, page, offset, len(items), in.Stat, in.IncludePerks)), nil, nil
}

func (h *handler) handleGetItem(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in GetItemInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, _, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolGetItem, err)
	}

	item, ok := s.Find(in.InstanceID)
	if !ok {
		return h.toolError(ctx, toolGetItem, unknownInstances([]string{in.InstanceID}))
	}

	// Only weapons have roll ratings, so every other item costs no sheet
	// request. A sheet that does not load costs only its rating line.
	var sheets map[string]SheetState
	if item.Kind == kindWeapon {
		sheets = h.sheets.Load(ctx)
	}

	return textResult(renderItem(s, item, sheets)), nil, nil
}

func (h *handler) handleCompareDuplicates(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in CompareDuplicatesInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	name := strings.TrimSpace(in.Name)
	if name != "" && in.InstanceID != "" {
		return h.toolError(ctx, toolCompareDuplicates,
			fmt.Errorf("%w: pass name or instance_id, not both", ErrInvalidInput))
	}

	s, _, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolCompareDuplicates, err)
	}

	if in.InstanceID != "" {
		item, ok := s.Find(in.InstanceID)
		if !ok {
			return h.toolError(ctx, toolCompareDuplicates, unknownInstances([]string{in.InstanceID}))
		}

		if item.Kind != kindWeapon {
			return h.toolError(ctx, toolCompareDuplicates,
				fmt.Errorf("%w: %s (%s) is not a weapon", ErrInvalidInput, item.InstanceID, item.Name))
		}

		return textResult(renderWeaponGroup(s, s.GroupOf(item))), nil, nil
	}

	if name != "" {
		group, err := s.CopiesOf(name)
		if err != nil {
			return h.toolError(ctx, toolCompareDuplicates, err)
		}

		return textResult(renderWeaponGroup(s, group)), nil, nil
	}

	var dupes []WeaponGroup

	for _, group := range s.WeaponGroups() {
		if len(group.Copies) > 1 {
			dupes = append(dupes, group)
		}
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultWeaponLimit
	}

	limit = min(limit, maxWeaponLimit)
	offset := min(max(in.Offset, 0), len(dupes))
	page := dupes[offset:min(offset+limit, len(dupes))]

	return textResult(renderDuplicates(s, page, offset, len(dupes))), nil, nil
}

func (h *handler) handleListActivityHistory(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ListActivityHistoryInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	limit := in.Limit
	if limit <= 0 {
		limit = defaultHistoryLimit
	}

	limit = min(limit, maxHistoryLimit)

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListActivityHistory, err)
	}

	characters := s.Characters
	scope := "the account"

	if in.Character != "" {
		c, err := s.ResolveCharacter(in.Character)
		if err != nil {
			return h.toolError(ctx, toolListActivityHistory, err)
		}

		characters = []Character{c}
		scope = s.OwnerLabel(c.ID)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolListActivityHistory, err)
	}

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return h.toolError(ctx, toolListActivityHistory, fmt.Errorf("loading definitions: %w", err))
	}

	pages := make([][]HistoryPeriod, len(characters))

	err = h.withBungie(ctx, func(creds Credentials) error {
		var wg sync.WaitGroup

		errs := make([]error, len(characters))

		// Each of the newest limit runs of the account is among the newest
		// limit runs of its character. Page 0 of each character therefore
		// holds them all.
		for i, c := range characters {
			wg.Go(func() {
				pages[i], errs[i] = h.bungie.ActivityHistory(ctx, creds, membership, c.ID, limit, 0)
				if errs[i] != nil {
					errs[i] = fmt.Errorf("character %s: %w", c.ID, errs[i])
				}
			})
		}

		wg.Wait()

		return errors.Join(errs...)
	})
	if err != nil {
		return h.toolError(ctx, toolListActivityHistory, fmt.Errorf("fetching the activity history: %w", err))
	}

	var runs []historyRun

	for i, c := range characters {
		for _, period := range pages[i] {
			runs = append(runs, historyRun{HistoryPeriod: period, Character: s.OwnerLabel(c.ID)})
		}
	}

	slices.SortStableFunc(runs, func(a, b historyRun) int { return b.Period.Compare(a.Period) })

	runs = runs[:min(limit, len(runs))]

	return textResult(renderActivityHistory(defs, runs, scope, limit)), nil, nil
}

func (h *handler) handleListVendors(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ListVendorsInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListVendors, err)
	}

	c, err := s.ResolveCharacter(in.Character)
	if err != nil {
		return h.toolError(ctx, toolListVendors, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolListVendors, err)
	}

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return h.toolError(ctx, toolListVendors, fmt.Errorf("loading definitions: %w", err))
	}

	var resp *VendorsResponse

	err = h.withBungie(ctx, func(creds Credentials) error {
		var err error

		resp, err = h.bungie.Vendors(ctx, creds, membership, c.ID)

		return err
	})
	if err != nil {
		return h.toolError(ctx, toolListVendors, fmt.Errorf("fetching the vendors: %w", err))
	}

	vendors := readVendors(resp, defs)
	owner := s.OwnerLabel(c.ID)

	if strings.TrimSpace(in.Vendor) == "" {
		return textResult(renderVendors(vendors, owner)), nil, nil
	}

	v, err := selectVendor(vendors, in.Vendor, owner)
	if err != nil {
		return h.toolError(ctx, toolListVendors, err)
	}

	return textResult(renderVendor(v, owner)), nil, nil
}

func (h *handler) handleListActivities(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ListActivitiesInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListActivities, err)
	}

	c, err := s.ResolveCharacter(in.Character)
	if err != nil {
		return h.toolError(ctx, toolListActivities, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolListActivities, err)
	}

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return h.toolError(ctx, toolListActivities, fmt.Errorf("loading definitions: %w", err))
	}

	var activities map[string]CharacterActivities

	// The rewards change when the character claims one, so every call
	// fetches the activities again instead of reusing the snapshot.
	err = h.withBungie(ctx, func(creds Credentials) error {
		var err error

		activities, err = h.bungie.Activities(ctx, creds, membership)

		return err
	})
	if err != nil {
		return h.toolError(ctx, toolListActivities, fmt.Errorf("fetching the activities: %w", err))
	}

	listed := activities[c.ID].Available
	all := readActivities(listed, defs)
	rows := filterActivities(all, in.Query, in.Rewards)

	limit := in.Limit
	if limit <= 0 {
		limit = defaultItemLimit
	}

	limit = min(limit, maxItemLimit)
	offset := min(max(in.Offset, 0), len(rows))
	page := rows[offset:min(offset+limit, len(rows))]

	return textResult(renderActivities(page, offset, len(rows), len(all), len(listed), s.OwnerLabel(c.ID))), nil, nil
}

func (h *handler) handleMoveItems(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in MoveItemsInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolMoveItems, err)
	}

	items, err := findItems(s, in.InstanceIDs)
	if err != nil {
		return h.toolError(ctx, toolMoveItems, err)
	}

	target, err := s.ResolveOwner(in.Target)
	if err != nil {
		return h.toolError(ctx, toolMoveItems, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolMoveItems, err)
	}

	mover := bungieMover{h: h, membership: membership}
	verb := "Moved"

	if in.Equip {
		verb = "Equipped"
	}

	var b strings.Builder

	failed := 0
	changed := false

	for _, item := range items {
		owner, postmaster, equipped := item.Owner, item.Postmaster, item.Equipped

		replacement, err := s.Move(ctx, mover, item, target, in.Equip)

		// A move that fails can still complete some of its steps.
		if replacement != nil || item.Owner != owner || item.Postmaster != postmaster || item.Equipped != equipped {
			changed = true
		}

		if err != nil {
			failed++

			h.log.WarnContext(ctx, "move failed",
				slog.String("instance_id", item.InstanceID),
				slog.Any("error", err),
			)
			fmt.Fprintf(&b, "- %s (%s) did not move: %v. It is at %s.",
				item.Name, item.InstanceID, err, s.LocationLabel(item))
		} else {
			fmt.Fprintf(&b, "- %s %s (%s). It is at %s.", verb, item.Name, item.InstanceID, s.LocationLabel(item))
		}

		if replacement != nil {
			fmt.Fprintf(&b, " Equipped %s (%s) in its place.", replacement.Name, replacement.InstanceID)
		}

		b.WriteString("\n")
	}

	// Bungie can serve the state from before a move for some seconds. A
	// snapshot that a move updated therefore stays in use for a full TTL.
	// A call that moved nothing keeps the age of the snapshot, so a stale
	// snapshot still expires.
	if changed {
		s.Fetched = h.now()
	}

	result := textResult(strings.TrimRight(b.String(), "\n"))
	result.IsError = failed == len(items)

	return result, nil, nil
}

func (h *handler) handleMoveStack(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in MoveStackInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := strings.ToLower(strings.TrimSpace(in.Target))
	if target != ownerVault && target != targetInventory {
		return h.toolError(ctx, toolMoveStack,
			fmt.Errorf("%w: target %q is not vault or inventory", ErrInvalidInput, in.Target))
	}

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolMoveStack, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolMoveStack, err)
	}

	mover := bungieMover{h: h, membership: membership}

	name, moved, err := s.MoveStack(ctx, mover, in.ItemHash, in.Amount, target == ownerVault)

	if moved > 0 {
		s.Fetched = h.now()
	}

	if err != nil {
		if moved > 0 {
			err = fmt.Errorf("moved %d of %s to the %s, then stopped: %w", moved, name, target, err)
		}

		return h.toolError(ctx, toolMoveStack, err)
	}

	return textResult(fmt.Sprintf("Moved %d of %s to the %s.", moved, name, target)), nil, nil
}

func (h *handler) handleSetLock(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetLockInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolSetLock, err)
	}

	items, err := findItems(s, in.InstanceIDs)
	if err != nil {
		return h.toolError(ctx, toolSetLock, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolSetLock, err)
	}

	locker := bungieMover{h: h, membership: membership}
	verb, done := "lock", "Locked"

	if !in.Locked {
		verb, done = "unlock", "Unlocked"
	}

	var b strings.Builder

	failed := 0

	for _, item := range items {
		err := s.SetLock(ctx, locker, item, in.Locked)
		if err != nil {
			failed++

			h.log.WarnContext(ctx, "lock change failed",
				slog.String("instance_id", item.InstanceID),
				slog.Any("error", err),
			)
			fmt.Fprintf(&b, "- %s (%s) did not %s: %v.\n", item.Name, item.InstanceID, verb, err)

			continue
		}

		fmt.Fprintf(&b, "- %s %s (%s).\n", done, item.Name, item.InstanceID)
	}

	// Bungie can serve the lock state from before a change for some
	// seconds. A snapshot that a change updated therefore stays in use for
	// a full TTL.
	if failed < len(items) {
		s.Fetched = h.now()
	}

	result := textResult(strings.TrimRight(b.String(), "\n"))
	result.IsError = failed == len(items)

	return result, nil, nil
}

func (h *handler) handleSetPerk(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetPerkInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	name := strings.TrimSpace(in.Perk)
	if name == "" {
		return h.toolError(ctx, toolSetPerk, fmt.Errorf("%w: a perk change needs a perk name", ErrInvalidInput))
	}

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolSetPerk, err)
	}

	item, ok := s.Find(in.InstanceID)
	if !ok {
		return h.toolError(ctx, toolSetPerk, unknownInstances([]string{in.InstanceID}))
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolSetPerk, err)
	}

	inserter := bungieMover{h: h, membership: membership}

	from, to, err := s.SetPerk(ctx, inserter, item, name, strings.TrimSpace(in.Replace))
	if err != nil {
		return h.toolError(ctx, toolSetPerk, err)
	}

	if from == to {
		return textResult(
			fmt.Sprintf("%s is already active on %s (%s).", to.Name, item.Name, item.InstanceID),
		), nil, nil
	}

	// Bungie can serve the sockets from before a change for some seconds.
	// A snapshot that a change updated therefore stays in use for a full
	// TTL.
	s.Fetched = h.now()

	return textResult(fmt.Sprintf(
		"Replaced %s with %s on %s (%s). Its stats keep their old values until the next inventory fetch.",
		from.Name, to.Name, item.Name, item.InstanceID,
	)), nil, nil
}

func (h *handler) handleListLoadouts(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ListLoadoutsInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListLoadouts, err)
	}

	characters := s.Characters

	if in.Character != "" {
		c, err := s.ResolveCharacter(in.Character)
		if err != nil {
			return h.toolError(ctx, toolListLoadouts, err)
		}

		characters = []Character{c}
	}

	return textResult(renderLoadouts(s, characters)), nil, nil
}

func (h *handler) handleEquipLoadout(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in EquipLoadoutInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Each character numbers its own loadouts, so an empty name returns an
	// error instead of selecting the most recently played character.
	if in.Character == "" {
		return h.toolError(ctx, toolEquipLoadout, fmt.Errorf(
			"%w: pass the character of the loadout as a character id or a class name", ErrUnknownTarget,
		))
	}

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolEquipLoadout, err)
	}

	c, err := s.ResolveCharacter(in.Character)
	if err != nil {
		return h.toolError(ctx, toolEquipLoadout, err)
	}

	loadout, err := s.FindLoadout(c.ID, in.Loadout)
	if err != nil {
		return h.toolError(ctx, toolEquipLoadout, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolEquipLoadout, err)
	}

	result, err := s.EquipLoadout(ctx, bungieMover{h: h, membership: membership}, loadout)

	// Bungie can serve the state from before an equip for some seconds. A
	// snapshot that the call updated therefore stays in use for a full TTL.
	if result.Changed {
		s.Fetched = h.now()
	}

	if err != nil {
		h.log.WarnContext(ctx, "loadout equip failed",
			slog.String("character_id", c.ID),
			slog.Int("loadout", loadout.Number),
			slog.Any("error", err),
		)
	}

	out := textResult(renderLoadoutEquip(s, loadout, result, err))
	out.IsError = err != nil

	return out, nil, nil
}

func (h *handler) handleListTags(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, _, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListTags, err)
	}

	return textResult(renderTags(s)), nil, nil
}

func (h *handler) handleSetTag(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetTagInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tag := strings.ToLower(strings.TrimSpace(in.Tag))
	if tag == tagNone {
		tag = ""
	}

	if tag != "" && !slices.Contains(tagOrder, tag) {
		return h.toolError(ctx, toolSetTag, fmt.Errorf(
			"%w: tag %q is not none or one of %s",
			ErrInvalidInput, in.Tag, strings.Join(tagOrder, ", "),
		))
	}

	s, rows, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolSetTag, err)
	}

	items, err := findItems(s, in.InstanceIDs)
	if err != nil {
		return h.toolError(ctx, toolSetTag, err)
	}

	var changes []ItemTag

	for _, item := range items {
		changes = append(changes, tagChanges(rows, item.InstanceID, item.Hash, tag)...)
	}

	err = h.writeTags(ctx, changes)
	if err != nil {
		return h.toolError(ctx, toolSetTag, err)
	}

	for _, item := range items {
		item.Tag = tag
	}

	if tag == "" {
		return textResult(fmt.Sprintf("Cleared the tag of %d items.", len(items))), nil, nil
	}

	return textResult(fmt.Sprintf("Tagged %d items as %s.", len(items), tag)), nil, nil
}

func (h *handler) handleSetFavorite(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetFavoriteInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolSetFavorite, err)
	}

	items, err := findItems(s, in.InstanceIDs)
	if err != nil {
		return h.toolError(ctx, toolSetFavorite, err)
	}

	changes := make([]ItemTag, 0, len(items))
	for _, item := range items {
		changes = append(changes, ItemTag{
			InstanceID: item.InstanceID,
			ItemHash:   item.Hash,
			TagID:      tagFavorite,
			Removed:    !in.Favorite,
		})
	}

	err = h.writeTags(ctx, changes)
	if err != nil {
		return h.toolError(ctx, toolSetFavorite, err)
	}

	for _, item := range items {
		item.Favorite = in.Favorite
	}

	if in.Favorite {
		return textResult(fmt.Sprintf("Marked %d items as favorites.", len(items))), nil, nil
	}

	return textResult(fmt.Sprintf("Cleared the favorite mark of %d items.", len(items))), nil, nil
}

func (h *handler) handleListNotes(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolListNotes, err)
	}

	notes, err := h.notes(ctx)
	if err != nil {
		return h.toolError(ctx, toolListNotes, err)
	}

	return textResult(renderNotes(s, notes)), nil, nil
}

func (h *handler) handleSetNote(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetNoteInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	kind := strings.ToLower(in.Kind)
	if kind == "" {
		kind = noteInstance
	}

	if !slices.Contains([]string{noteInstance, noteItem, noteSet}, kind) {
		return h.toolError(ctx, toolSetNote,
			fmt.Errorf("%w: kind %q is not instance, item, or set", ErrInvalidInput, in.Kind))
	}

	if in.TargetID == "" || utf16Len(in.Body) > maxNoteLength {
		return h.toolError(ctx, toolSetNote, fmt.Errorf(
			"%w: a note needs a target_id and a body of at most %d characters",
			ErrInvalidInput, maxNoteLength,
		))
	}

	body := in.Body
	if strings.TrimSpace(body) == "" {
		body = ""
	}

	change := Note{Kind: kind, TargetID: in.TargetID, Body: body, Removed: body == ""}

	var item *Item

	if kind == noteInstance {
		s, err := h.loadSnapshot(ctx, false)
		if err != nil {
			return h.toolError(ctx, toolSetNote, err)
		}

		found, ok := s.Find(in.TargetID)

		switch {
		case ok:
			item = found
			change.ItemHash = &item.Hash

		case change.Removed:
			// The service requires an item hash, and the item left the
			// inventory, so the stored note supplies the hash.
			change.ItemHash, err = h.noteItemHash(ctx, in.TargetID)
			if err != nil {
				return h.toolError(ctx, toolSetNote, err)
			}

		default:
			return h.toolError(ctx, toolSetNote, unknownInstances([]string{in.TargetID}))
		}
	}

	err := h.withSession(ctx, func(session string) error {
		return h.service.WriteNote(ctx, session, change)
	})
	if err != nil {
		return h.toolError(ctx, toolSetNote, err)
	}

	if item != nil {
		item.Note = body
	}

	if change.Removed {
		return textResult("Removed the note."), nil, nil
	}

	return textResult("Saved the note."), nil, nil
}

func (h *handler) handleListPerkPreferences(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	prefs, err := h.perkPreferences(ctx)
	if err != nil {
		return h.toolError(ctx, toolListPerkPreferences, err)
	}

	return textResult(renderPerkPreferences(prefs)), nil, nil
}

func (h *handler) handleSetPerkPreferences(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetPerkPreferencesInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(in.Preferences) == 0 || len(in.Preferences) > maxBatch {
		return h.toolError(ctx, toolSetPerkPreferences, fmt.Errorf(
			"%w: pass between 1 and %d preferences", ErrInvalidInput, maxBatch,
		))
	}

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return h.toolError(ctx, toolSetPerkPreferences, fmt.Errorf("loading definitions: %w", err))
	}

	traits := defs.TraitNames()
	changes := make([]PerkPreference, 0, len(in.Preferences))

	for _, pref := range in.Preferences {
		change, err := perkChange(pref, traits)
		if err != nil {
			return h.toolError(ctx, toolSetPerkPreferences, err)
		}

		changes = append(changes, change)
	}

	err = h.withSession(ctx, func(session string) error {
		return h.service.WritePerkPreferences(ctx, session, changes)
	})
	if err != nil {
		return h.toolError(ctx, toolSetPerkPreferences, err)
	}

	prefs, err := h.perkPreferences(ctx)
	if err != nil {
		return h.toolError(ctx, toolSetPerkPreferences, err)
	}

	return textResult("Saved. Preferences are:\n\n" + renderPerkPreferences(prefs)), nil, nil
}

func (h *handler) handleListAutotagRules(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	snapshot, err := h.autotag(ctx)
	if err != nil {
		return h.toolError(ctx, toolListAutotagRules, err)
	}

	return textResult(renderAutotag(snapshot)), nil, nil
}

func (h *handler) handleSaveAutotagRule(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SaveAutotagRuleInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	snapshot, err := h.autotag(ctx)
	if err != nil {
		return h.toolError(ctx, toolSaveAutotagRule, err)
	}

	rule, err := mergeRule(liveRules(snapshot.Rules), in)
	if err != nil {
		return h.toolError(ctx, toolSaveAutotagRule, err)
	}

	err = h.withSession(ctx, func(session string) error {
		return h.service.WriteAutotagRules(ctx, session, []AutotagRule{rule})
	})
	if err != nil {
		return h.toolError(ctx, toolSaveAutotagRule, err)
	}

	return textResult("Saved the rule:\n" + renderRule(rule)), nil, nil
}

func (h *handler) handleRemoveAutotagRule(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in RemoveAutotagRuleInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	snapshot, err := h.autotag(ctx)
	if err != nil {
		return h.toolError(ctx, toolRemoveAutotagRule, err)
	}

	rules := liveRules(snapshot.Rules)

	i := slices.IndexFunc(rules, func(rule AutotagRule) bool { return rule.ID == in.ID })
	if i < 0 {
		return h.toolError(ctx, toolRemoveAutotagRule,
			fmt.Errorf("%w: no autotag rule has the id %q", ErrInvalidInput, in.ID))
	}

	rule := rules[i]
	rule.Removed = true
	rule.UpdatedAt = 0

	err = h.withSession(ctx, func(session string) error {
		return h.service.WriteAutotagRules(ctx, session, []AutotagRule{rule})
	})
	if err != nil {
		return h.toolError(ctx, toolRemoveAutotagRule, err)
	}

	return textResult(fmt.Sprintf("Removed the rule %q.", rule.Query)), nil, nil
}

func (h *handler) handleSetAutotagEnabled(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SetAutotagEnabledInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	mark := ""

	if in.Enabled {
		// The mark must cover every item held at this moment, so a cached
		// profile does not serve.
		s, err := h.loadSnapshot(ctx, true)
		if err != nil {
			return h.toolError(ctx, toolSetAutotagEnabled, err)
		}

		mark = s.NewestMark()
	}

	applied, err := sessionValue(ctx, h, func(session string) (bool, error) {
		return h.service.SetAutotagEnabled(ctx, session, in.Enabled, mark)
	})
	if err != nil {
		return h.toolError(ctx, toolSetAutotagEnabled, err)
	}

	switch {
	case !applied:
		return h.toolError(ctx, toolSetAutotagEnabled,
			fmt.Errorf("%w: the service did not apply the change", ErrService))

	case in.Enabled:
		return textResult("Autotagging is on. The rules apply to items acquired from now on, " +
			"and the web client runs them while it is open."), nil, nil

	default:
		return textResult("Autotagging is off."), nil, nil
	}
}

func (h *handler) handleGetStats(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in GetStatsInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	period, err := statPeriodOf(in.Period)
	if err != nil {
		return h.toolError(ctx, toolGetStats, err)
	}

	to := h.now()
	from := period.start(to)

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return h.toolError(ctx, toolGetStats, fmt.Errorf("loading definitions: %w", err))
	}

	cursor := statCursorFor(from)

	chunks, err := sessionValue(ctx, h, func(session string) ([]StatChunk, error) {
		return h.service.Stats(ctx, session, cursor)
	})
	if err != nil {
		return h.toolError(ctx, toolGetStats, fmt.Errorf("reading recorded changes: %w", err))
	}

	summary := summarizeStats(chunks, from, to)
	summary.readFrom = time.UnixMilli(cursor.UpdatedAt).In(to.Location())

	return textResult(renderStats(defs, period, summary)), nil, nil
}

// perkChange validates one preference and converts it to a service row.
//
// The web client matches a stored preference to a perk by exact display
// name. perkChange therefore replaces the given name with the display name
// of the weapon trait in traits. It rejects a name that is no trait,
// because the web client never matches such a row. A removal passes an
// unknown name through, so the tool can clear a row that an earlier write
// left behind.
func perkChange(in PerkPreferenceInput, traits map[string]string) (PerkPreference, error) {
	mode := strings.ToLower(in.Mode)
	stance := strings.ToLower(in.Stance)
	perk := basePerkName(in.Perk)

	if !slices.Contains(perkModes, mode) {
		return PerkPreference{}, fmt.Errorf("%w: mode %q is not pve or pvp", ErrInvalidInput, in.Mode)
	}

	if perk == "" {
		return PerkPreference{}, fmt.Errorf("%w: a preference needs a perk name", ErrInvalidInput)
	}

	if stance != stanceNone && !slices.Contains(perkStances, stance) {
		return PerkPreference{}, fmt.Errorf("%w: stance %q is not up, down, or none", ErrInvalidInput, in.Stance)
	}

	name, known := traits[strings.ToLower(perk)]
	if known {
		perk = name
	}

	if stance == stanceNone {
		// The service validates the stance of a removed row too.
		return PerkPreference{Mode: mode, Perk: perk, Stance: perkStances[0], Removed: true}, nil
	}

	if !known {
		return PerkPreference{}, unknownTrait(perk, traits)
	}

	return PerkPreference{Mode: mode, Perk: perk, Stance: stance}, nil
}

// unknownTrait reports a perk name that is no weapon trait, with the traits
// whose names contain it.
func unknownTrait(perk string, traits map[string]string) error {
	const limit = 5

	var similar []string

	for lower, name := range traits {
		if strings.Contains(lower, strings.ToLower(perk)) {
			similar = append(similar, name)
		}
	}

	if len(similar) == 0 {
		return fmt.Errorf("%w: perk %q is not a weapon trait", ErrInvalidInput, perk)
	}

	slices.Sort(similar)

	similar = similar[:min(len(similar), limit)]

	return fmt.Errorf(
		"%w: perk %q is not a weapon trait. Similar traits: %s",
		ErrInvalidInput, perk, strings.Join(similar, ", "),
	)
}

// mergeRule builds the rule that a save_autotag_rule call writes. With an
// id it overlays the given fields on the stored rule. Without an id it
// creates a rule that evaluates last. It rejects a built rule that has no
// action, which is a rule with an empty tag id and a false vault flag. An
// enabled rule without an action takes the items that it matches from the
// rules after it and neither tags nor vaults them. A stored rule without an
// action also disables the Save button of the rule editor of the web client
// until the user gives the rule an action or removes it.
func mergeRule(rules []AutotagRule, in SaveAutotagRuleInput) (AutotagRule, error) {
	var rule AutotagRule

	if in.ID == "" {
		if in.Query == nil {
			return AutotagRule{}, fmt.Errorf("%w: a new rule needs a query", ErrInvalidInput)
		}

		id, err := newRuleID()
		if err != nil {
			return AutotagRule{}, err
		}

		rule = AutotagRule{ID: id, Enabled: true}

		for _, other := range rules {
			rule.Position = max(rule.Position, other.Position+1)
		}
	} else {
		i := slices.IndexFunc(rules, func(rule AutotagRule) bool { return rule.ID == in.ID })
		if i < 0 {
			return AutotagRule{}, fmt.Errorf("%w: no autotag rule has the id %q", ErrInvalidInput, in.ID)
		}

		rule = rules[i]
		rule.UpdatedAt = 0
	}

	if in.Query != nil {
		rule.Query = strings.TrimSpace(*in.Query)
	}

	if in.TagID != nil {
		rule.TagID = strings.ToLower(strings.TrimSpace(*in.TagID))
	}

	if in.Vault != nil {
		rule.Vault = *in.Vault
	}

	if in.Enabled != nil {
		rule.Enabled = *in.Enabled
	}

	if in.Position != nil {
		rule.Position = *in.Position
	}

	if rule.Query == "" || utf16Len(rule.Query) > maxQueryLength {
		return AutotagRule{}, fmt.Errorf(
			"%w: a rule query has between 1 and %d characters", ErrInvalidInput, maxQueryLength,
		)
	}

	if rule.TagID != "" && !slices.Contains(tagOrder, rule.TagID) {
		return AutotagRule{}, fmt.Errorf(
			"%w: tag_id %q is not one of %s",
			ErrInvalidInput, rule.TagID, strings.Join(tagOrder, ", "),
		)
	}

	if rule.TagID == "" && !rule.Vault {
		return AutotagRule{}, fmt.Errorf(
			"%w: a rule needs a tag_id or vault=true, so add one or remove the rule with %s",
			ErrInvalidInput, toolRemoveAutotagRule,
		)
	}

	return rule, nil
}

// newRuleID returns a random UUID, the id format that the web client gives
// its rules.
func newRuleID() (string, error) {
	var raw [16]byte

	_, err := rand.Read(raw[:])
	if err != nil {
		return "", fmt.Errorf("generating rule id: %w", err)
	}

	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

// findItems resolves instance ids against the snapshot. It rejects the
// whole batch when an id is unknown, so a write never covers part of a
// request.
func findItems(s *Snapshot, ids []string) ([]*Item, error) {
	if len(ids) == 0 || len(ids) > maxBatch {
		return nil, fmt.Errorf("%w: pass between 1 and %d instance ids", ErrInvalidInput, maxBatch)
	}

	items := make([]*Item, 0, len(ids))

	var unknown []string

	for _, id := range ids {
		item, ok := s.Find(id)
		if !ok {
			unknown = append(unknown, id)

			continue
		}

		if !slices.Contains(items, item) {
			items = append(items, item)
		}
	}

	if len(unknown) > 0 {
		return nil, unknownInstances(unknown)
	}

	return items, nil
}

func unknownInstances(ids []string) error {
	return fmt.Errorf(
		"%w: no item in the inventory has the instance id %s",
		ErrInvalidInput, strings.Join(ids, ", "),
	)
}

// loadSnapshot returns the cached snapshot, and fetches the profile from
// Bungie when the cache is empty, older than [snapshotTTL], or refresh is
// set.
func (h *handler) loadSnapshot(ctx context.Context, refresh bool) (*Snapshot, error) {
	now := h.now()
	if h.snapshot != nil && !refresh && now.Sub(h.snapshot.Fetched) < snapshotTTL {
		return h.snapshot, nil
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return nil, err
	}

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading definitions: %w", err)
	}

	var profile *Profile

	err = h.withBungie(ctx, func(creds Credentials) error {
		var err error

		profile, err = h.bungie.Profile(ctx, creds, membership)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("fetching the profile: %w", err)
	}

	h.snapshot = NewSnapshot(profile, defs, now)

	return h.snapshot, nil
}

// loadAnnotated returns the snapshot with the tags and notes that the
// service stores at the time of the call. It also returns the tag rows.
func (h *handler) loadAnnotated(ctx context.Context, refresh bool) (*Snapshot, []ItemTag, error) {
	s, err := h.loadSnapshot(ctx, refresh)
	if err != nil {
		return nil, nil, err
	}

	var (
		tags  []ItemTag
		notes []Note
	)

	err = h.withSession(ctx, func(session string) error {
		var (
			wg       sync.WaitGroup
			tagsErr  error
			notesErr error
		)

		// The two reads are independent, so they share one wait.
		wg.Go(func() { tags, tagsErr = h.service.Tags(ctx, session) })
		wg.Go(func() { notes, notesErr = h.service.Notes(ctx, session) })
		wg.Wait()

		return errors.Join(tagsErr, notesErr)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("reading tags and notes: %w", err)
	}

	s.Annotate(tags, notes)

	return s, tags, nil
}

func (h *handler) writeTags(ctx context.Context, changes []ItemTag) error {
	const chunk = 1000

	for part := range slices.Chunk(changes, chunk) {
		err := h.withSession(ctx, func(session string) error {
			return h.service.WriteTags(ctx, session, part)
		})
		if err != nil {
			return fmt.Errorf("writing tags: %w", err)
		}
	}

	return nil
}

// noteItemHash returns the item hash that the service stores with the
// instance note of instanceID.
func (h *handler) noteItemHash(ctx context.Context, instanceID string) (*uint32, error) {
	notes, err := h.notes(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading notes: %w", err)
	}

	for _, note := range notes {
		if note.Kind == noteInstance && note.TargetID == instanceID && !note.Removed && note.ItemHash != nil {
			return note.ItemHash, nil
		}
	}

	return nil, fmt.Errorf(
		"%w: no item in the inventory and no saved note has the instance id %s",
		ErrInvalidInput, instanceID,
	)
}

func (h *handler) notes(ctx context.Context) ([]Note, error) {
	return sessionValue(ctx, h, func(session string) ([]Note, error) {
		return h.service.Notes(ctx, session)
	})
}

func (h *handler) perkPreferences(ctx context.Context) ([]PerkPreference, error) {
	return sessionValue(ctx, h, func(session string) ([]PerkPreference, error) {
		return h.service.PerkPreferences(ctx, session)
	})
}

func (h *handler) autotag(ctx context.Context) (AutotagSnapshot, error) {
	return sessionValue(ctx, h, func(session string) (AutotagSnapshot, error) {
		return h.service.Autotag(ctx, session)
	})
}

// membership returns the Destiny membership of the signed-in account. The
// first call after a sign-in asks Bungie and records the answer in the
// token file.
func (h *handler) membership(ctx context.Context) (Membership, error) {
	t, err := h.auth.Tokens(ctx)
	if err != nil {
		return Membership{}, err
	}

	if !t.Membership.IsZero() {
		return t.Membership, nil
	}

	var m Membership

	err = h.withBungie(ctx, func(creds Credentials) error {
		var err error

		m, err = h.bungie.PrimaryMembership(ctx, creds)

		return err
	})
	if err != nil {
		return Membership{}, fmt.Errorf("resolving the Destiny membership: %w", err)
	}

	err = h.auth.SetMembership(t.BungieID, m)
	if err != nil {
		h.log.WarnContext(ctx, "recording membership", slog.Any("error", err))
	}

	return m, nil
}

// withBungie runs fn with Bungie credentials. When Bungie rejects the
// access token, it refreshes the tokens and runs fn a second time.
func (h *handler) withBungie(ctx context.Context, fn func(Credentials) error) error {
	t, err := h.auth.Tokens(ctx)
	if err != nil {
		return err
	}

	key, err := h.bungieKey(ctx)
	if err != nil {
		return err
	}

	err = fn(Credentials{APIKey: key, AccessToken: t.AccessToken})
	if !errors.Is(err, ErrUnauthorized) {
		return err
	}

	t, err = h.auth.Refresh(ctx, t.AccessToken)
	if err != nil {
		return err
	}

	return fn(Credentials{APIKey: key, AccessToken: t.AccessToken})
}

// withSession runs fn with the service session token. When the service
// rejects the session, it refreshes the tokens and runs fn a second time.
func (h *handler) withSession(ctx context.Context, fn func(session string) error) error {
	t, err := h.auth.Tokens(ctx)
	if err != nil {
		return err
	}

	err = fn(t.Session)
	if !errors.Is(err, ErrUnauthorized) {
		return err
	}

	t, err = h.auth.Refresh(ctx, t.AccessToken)
	if err != nil {
		return err
	}

	return fn(t.Session)
}

// sessionValue runs fn through [*handler.withSession] and returns the value
// of the last run.
func sessionValue[T any](ctx context.Context, h *handler, fn func(session string) (T, error)) (T, error) {
	var out T

	err := h.withSession(ctx, func(session string) error {
		var err error

		out, err = fn(session)

		return err
	})

	return out, err
}

// utf16Len returns the length of s in UTF-16 code units. The service
// enforces its length limits in that unit.
func utf16Len(s string) int {
	n := 0

	for _, r := range s {
		n += utf16.RuneLen(r)
	}

	return n
}

func (h *handler) bungieKey(ctx context.Context) (string, error) {
	now := h.now()
	if h.apiKey != "" && now.Sub(h.apiKeyAt) < configTTL {
		return h.apiKey, nil
	}

	cfg, err := h.service.Config(ctx)
	if err != nil {
		return "", fmt.Errorf("reading service config: %w", err)
	}

	h.apiKey = cfg.APIKey
	h.apiKeyAt = now

	return h.apiKey, nil
}

// bungieMover adapts the handler to [ItemMover], [ItemLocker],
// [PlugInserter], and [LoadoutEquipper]. It adds the membership and the
// credentials to each action.
type bungieMover struct {
	h          *handler
	membership Membership
}

// Transfer implements [ItemMover].
func (m bungieMover) Transfer(ctx context.Context, action ItemAction, toVault bool) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds Credentials) error {
		return m.h.bungie.Transfer(ctx, creds, action, toVault)
	})
}

// Equip implements [ItemMover].
func (m bungieMover) Equip(ctx context.Context, action ItemAction) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds Credentials) error {
		return m.h.bungie.Equip(ctx, creds, action)
	})
}

// PullFromPostmaster implements [ItemMover].
func (m bungieMover) PullFromPostmaster(ctx context.Context, action ItemAction) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds Credentials) error {
		return m.h.bungie.PullFromPostmaster(ctx, creds, action)
	})
}

// SetLockState implements [ItemLocker].
func (m bungieMover) SetLockState(ctx context.Context, action ItemAction, locked bool) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds Credentials) error {
		return m.h.bungie.SetLockState(ctx, creds, action, locked)
	})
}

// InsertSocketPlugFree implements [PlugInserter].
func (m bungieMover) InsertSocketPlugFree(ctx context.Context, action ItemAction, socket int, plugHash uint32) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds Credentials) error {
		return m.h.bungie.InsertSocketPlugFree(ctx, creds, action, socket, plugHash)
	})
}

// EquipLoadout implements [LoadoutEquipper].
func (m bungieMover) EquipLoadout(ctx context.Context, action LoadoutAction) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds Credentials) error {
		return m.h.bungie.EquipLoadout(ctx, creds, action)
	})
}

// toolError logs err and returns it as a tool result with
// [*mcp.CallToolResult.IsError] set, so the model reads the reason.
func (h *handler) toolError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, any, error) {
	h.log.WarnContext(ctx, "tool error",
		slog.String("tool", tool),
		slog.Any("error", err),
	)

	text := err.Error()
	if errors.Is(err, ErrNotSignedIn) {
		text += ". Call the login tool to sign in."
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}, nil, nil
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
