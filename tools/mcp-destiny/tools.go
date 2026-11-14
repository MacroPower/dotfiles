package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/activities"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/auth"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/search"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/stats"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/vendors"
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
	toolStageJunk           = "stage_junk"
	toolSetLock             = "set_lock"
	toolSetPerk             = "set_perk"
	toolListLoadouts        = "list_loadouts"
	toolEquipLoadout        = "equip_loadout"
	toolSaveLoadout         = "save_loadout"
	toolClearLoadout        = "clear_loadout"
	toolListTags            = "list_tags"
	toolSetTag              = "set_tag"
	toolSetFavorite         = "set_favorite"
	toolListNotes           = "list_notes"
	toolSetNote             = "set_note"
	toolListPerkPreferences = "list_perk_preferences"
	toolSetPerkPreferences  = "set_perk_preferences"
	toolListAutotagRules    = "list_autotag_rules"
	toolSaveAutotagRule     = "save_autotag_rule"
	toolPreviewAutotagRule  = "preview_autotag_rule"
	toolRemoveAutotagRule   = "remove_autotag_rule"
	toolSetAutotagEnabled   = "set_autotag_enabled"
	toolRunAutotagRules     = "run_autotag_rules"
	toolListActivityHistory = "list_activity_history"
	toolListVendors         = "list_vendors"
	toolListActivities      = "list_activities"
	toolGetStats            = "get_stats"
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

	// matchesNew is the matches value of list_autotag_rules that shows
	// the rule that takes each item that the next claim offers to the
	// rules.
	matchesNew = "new"

	// matchesOwned is the matches value of list_autotag_rules that counts
	// the owned items that each rule matches after the enabled rules
	// before it took theirs.
	matchesOwned = "owned"
)

var (
	perkStances = []string{rolls.StanceUp, rolls.StanceDown}

	// ErrInvalidInput reports a tool argument that the handler rejects
	// before any request.
	ErrInvalidInput = errors.New("invalid input")

	// errNoLoadoutCharacter reports a loadout tool call without a
	// character. Each character numbers its own loadouts, so the tools do
	// not fall back to the most recently played character.
	errNoLoadoutCharacter = fmt.Errorf(
		"%w: pass the character of the loadout as a character id or a class name", inventory.ErrUnknownTarget,
	)

	// errNoAutotagState reports an autotag read that returns no state.
	// The service keeps the state after the first time a client turns
	// autotagging on, so the read after a claim request expects one.
	errNoAutotagState = fmt.Errorf("%w: the service returned no autotag state", dvm.ErrService)
)

// EmptyInput is the input schema of the tools that take no arguments.
type EmptyInput struct{}

// CompleteLoginInput is the input schema of the complete_login tool.
type CompleteLoginInput struct {
	RedirectURL string `json:"redirect_url" jsonschema:"Full URL from the browser address bar after Bungie redirected to Agg's Destiny Vault Manager"`
}

// ListItemsInput is the input schema of the list_items tool.
type ListItemsInput struct {
	Query          string `json:"query,omitzero"           jsonschema:"Case-insensitive substring of the item name or type, such as 'fatebringer' or 'hand cannon'"`
	Kind           string `json:"kind,omitzero"            jsonschema:"weapon, armor, gear (weapons and armor, the default), other, or all"`
	Slot           string `json:"slot,omitzero"            jsonschema:"Substring of the slot name, such as 'kinetic', 'energy', 'power', 'helmet', or 'class armor'"`
	Tier           string `json:"tier,omitzero"            jsonschema:"Rarity, such as 'exotic' or 'legendary'"`
	Class          string `json:"class,omitzero"           jsonschema:"Class that armor belongs to: hunter, titan, or warlock"`
	Damage         string `json:"damage,omitzero"          jsonschema:"Damage type, such as 'kinetic', 'arc', 'solar', 'void', 'stasis', or 'strand'"`
	Location       string `json:"location,omitzero"        jsonschema:"vault, postmaster, equipped, characters (any character), a character id, or a class name"`
	Tag            string `json:"tag,omitzero"             jsonschema:"Tag id from list_tags, 'none' for untagged items, or 'any' for tagged items"`
	Perk           string `json:"perk,omitzero"            jsonschema:"Case-insensitive substring of a perk the item has equipped, such as 'kill clip'"`
	Stat           string `json:"stat,omitzero"            jsonschema:"Case-insensitive full name of one stat, such as 'health' for armor or 'range' for weapons. Adds a column with its value, and min_stat and sort=stat read it"`
	Sort           string `json:"sort,omitzero"            jsonschema:"name (default), power (highest first), newest (most recently acquired first), total (highest armor stat total first), or stat (highest value of the stat that stat names first)"`
	MinPvERating   string `json:"min_pve_rating,omitzero"  jsonschema:"Lowest roll rating on the Aegis sheet (PvE) to include, a tier from S (best) to F. Only weapon copies whose roll has a tier on that sheet match"`
	MinPvPRating   string `json:"min_pvp_rating,omitzero"  jsonschema:"Lowest roll rating on the PvP sheet to include, a tier from S (best) to F. Only weapon copies whose roll has a tier on that sheet match"`
	MinPower       int    `json:"min_power,omitzero"       jsonschema:"Lowest power level to include"`
	MaxPower       int    `json:"max_power,omitzero"       jsonschema:"Highest power level to include"`
	MinTotal       int    `json:"min_total,omitzero"       jsonschema:"Lowest armor stat total to include. Only armor has a stat total, so a positive value returns only armor"`
	MinStat        int    `json:"min_stat,omitzero"        jsonschema:"Lowest value of the stat that stat names. Items that lack the stat do not match"`
	Limit          int    `json:"limit,omitzero"           jsonschema:"Maximum number of items to return (default 50, maximum 200)"`
	Offset         int    `json:"offset,omitzero"          jsonschema:"Number of matching items to skip (default 0)"`
	Favorite       bool   `json:"favorite,omitzero"        jsonschema:"Return only favorites"`
	IncludePerks   bool   `json:"include_perks,omitzero"   jsonschema:"Add a column with the equipped perks of each item"`
	IncludeRatings bool   `json:"include_ratings,omitzero" jsonschema:"Add the columns 'pve rating' and 'pvp rating' with the roll rating of each weapon copy on the Aegis sheet and on the PvP sheet"`
	Refresh        bool   `json:"refresh,omitzero"         jsonschema:"Fetch the inventory from Bungie again instead of reusing a copy up to 30 seconds old"`
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

// StageJunkInput is the input schema of the stage_junk tool.
type StageJunkInput struct {
	Slot      string `json:"slot"               jsonschema:"Part of the name of a weapon or armor slot in any letter case, such as 'kinetic' or 'helmet'"`
	Character string `json:"character,omitzero" jsonschema:"Character id, or a class name that exactly one character has. Omit to stage on the most recently played character"`
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

// SaveLoadoutInput is the input schema of the save_loadout tool.
type SaveLoadoutInput struct {
	Character string `json:"character"     jsonschema:"Character id, or a class name that exactly one character has"`
	Name      string `json:"name,omitzero" jsonschema:"Loadout name from the list that the game offers, in any letter case, such as 'Crucible' or 'raid'. Omit to keep the name of a saved loadout. An empty slot then takes the first name of the list"`
	Number    int    `json:"number"        jsonschema:"Loadout slot to save into, counted from 1 on the in-game loadout screen as in list_loadouts"`
}

// ClearLoadoutInput is the input schema of the clear_loadout tool.
type ClearLoadoutInput struct {
	Character string `json:"character" jsonschema:"Character id, or a class name that exactly one character has"`
	Number    int    `json:"number"    jsonschema:"Loadout number from list_loadouts, which counts the in-game loadout screen from 1"`
}

// ListAutotagRulesInput is the input schema of the list_autotag_rules
// tool.
type ListAutotagRulesInput struct {
	Matches string `json:"matches,omitzero" jsonschema:"new to show which rule would take each item that the next claim offers to the rules, without claiming, or owned to count the owned items that each rule matches after the rules before it"`
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

// PreviewAutotagRuleInput is the input schema of the preview_autotag_rule
// tool.
type PreviewAutotagRuleInput struct {
	Query   string `json:"query"            jsonschema:"Item search in the search syntax of Agg's Destiny Vault Manager, such as 'is:weapon is:unlocked -tag:keep'. The tool evaluates only the filters that its description names"`
	Limit   int    `json:"limit,omitzero"   jsonschema:"Maximum number of matching items to return, and separately of undecided items to list in their own section (default 50, maximum 200)"`
	Offset  int    `json:"offset,omitzero"  jsonschema:"Number of matching items to skip (default 0)"`
	Refresh bool   `json:"refresh,omitzero" jsonschema:"Fetch the inventory from Bungie again instead of reusing a copy up to 30 seconds old"`
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
// tool calls, because moves, lock changes, perk changes, and the equips,
// saves, and clears of loadouts update the cached snapshot in place. The
// sheet store and the loadout table store have no lock of their own, so
// the same mutex guards them.
type handler struct {
	service       *dvm.Client
	bungie        *bungie.Client
	auth          *auth.Auth
	defs          *definitions.Loader
	sheets        *rolls.SheetStore
	loadoutTables *bungie.LoadoutTableStore
	log           *slog.Logger
	now           func() time.Time
	snapshot      *inventory.Snapshot
	apiKeyAt      time.Time
	apiKey        string
	mu            sync.Mutex
}

func (h *handler) handleStatus(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	t, err := h.auth.Saved()
	if err != nil && !errors.Is(err, auth.ErrNotSignedIn) {
		return h.toolError(ctx, toolStatus, err)
	}

	text := renderStatus(t, err == nil, h.service.BaseURL(), h.auth.Store().Path(), h.now())

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

	// A value that is not a tier letter fails the call before the first
	// request.
	minimums, err := ratingMinimums(in)
	if err != nil {
		return h.toolError(ctx, toolListItems, err)
	}

	s, _, err := h.loadAnnotated(ctx, in.Refresh)
	if err != nil {
		return h.toolError(ctx, toolListItems, err)
	}

	items, err := s.Filter(inventory.ItemFilter{
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

	// The inputs alone decide the rating requests. A call without
	// include_ratings and without a minimum sends none. Every other call
	// sends them for every valid filter, also when no item matches. A
	// minimum on a game mode without ratings therefore always returns
	// the reason. The snapshot keeps no rating, so the minimums and the
	// renderer each rate a weapon from the sources.
	var sources map[string]rolls.RatingSource
	if in.IncludeRatings || len(minimums) > 0 {
		sources = h.ratingSources(ctx)
	}

	// The minimums apply before the paging, so the totals and the
	// offsets count the items that pass them. A game mode without
	// ratings fails the call only when the call sets its minimum.
	items, err = keepRated(items, minimums, sources)
	if err != nil {
		return h.toolError(ctx, toolListItems, err)
	}

	// A minimum adds no column, so the renderer gets the sources only
	// with include_ratings. In the table, a game mode without ratings
	// costs only its rating cells.
	if !in.IncludeRatings {
		sources = nil
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultItemLimit
	}

	limit = min(limit, maxItemLimit)
	offset := min(max(in.Offset, 0), len(items))
	page := items[offset:min(offset+limit, len(items))]

	return textResult(renderItems(s, page, offset, len(items), in.Stat, in.IncludePerks, sources)), nil, nil
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

	// Only weapons have roll ratings, so every other item costs no
	// request for a sheet or for the perk preferences. A rating input
	// that the server cannot read costs only the rating lines. The
	// server rates the copy on every call and keeps no rating. A perk
	// change edits the cached snapshot in place, so a kept rating does
	// not follow the change.
	var ratings map[string]rolls.Rating
	if item.Kind == inventory.KindWeapon {
		sources := h.ratingSources(ctx)
		ratings = make(map[string]rolls.Rating, len(rolls.Modes))

		for _, mode := range rolls.Modes {
			ratings[mode] = rolls.Rate(sources[mode], item)
		}
	}

	return textResult(renderItem(s, item, ratings)), nil, nil
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

	// Every table shows the roll ratings, so one read of the rating
	// inputs serves every form of the result. The read returns no
	// error, so it does not change the result of a check that follows
	// it. A call that returns no table still sends the rating requests,
	// such as a call with the instance id of an armor piece. A rating
	// input that the server cannot read costs only the rating cells.
	sources := h.ratingSources(ctx)

	if in.InstanceID != "" {
		item, ok := s.Find(in.InstanceID)
		if !ok {
			return h.toolError(ctx, toolCompareDuplicates, unknownInstances([]string{in.InstanceID}))
		}

		if item.Kind != inventory.KindWeapon {
			return h.toolError(ctx, toolCompareDuplicates,
				fmt.Errorf("%w: %s (%s) is not a weapon", ErrInvalidInput, item.InstanceID, item.Name))
		}

		return textResult(renderSingleGroup(s, s.GroupOf(item), sources)), nil, nil
	}

	if name != "" {
		group, err := s.CopiesOf(name)
		if err != nil {
			return h.toolError(ctx, toolCompareDuplicates, err)
		}

		return textResult(renderSingleGroup(s, group, sources)), nil, nil
	}

	var dupes []inventory.WeaponGroup

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

	return textResult(renderDuplicates(s, page, offset, len(dupes), sources)), nil, nil
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

		characters = []inventory.Character{c}
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

	pages := make([][]bungie.HistoryPeriod, len(characters))

	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
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

	var resp *bungie.VendorsResponse

	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
		var err error

		resp, err = h.bungie.Vendors(ctx, creds, membership, c.ID)

		return err //nolint:wrapcheck // The client error reaches the tool text unchanged.
	})
	if err != nil {
		return h.toolError(ctx, toolListVendors, fmt.Errorf("fetching the vendors: %w", err))
	}

	listed := vendors.Read(resp, defs)
	owner := s.OwnerLabel(c.ID)

	if strings.TrimSpace(in.Vendor) == "" {
		return textResult(renderVendors(listed, owner)), nil, nil
	}

	v, err := vendors.Select(listed, in.Vendor, owner)
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

	var byCharacter map[string]bungie.CharacterActivities

	// The rewards change when the character claims one, so every call
	// fetches the activities again instead of reusing the snapshot.
	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
		var err error

		byCharacter, err = h.bungie.Activities(ctx, creds, membership)

		return err //nolint:wrapcheck // The client error reaches the tool text unchanged.
	})
	if err != nil {
		return h.toolError(ctx, toolListActivities, fmt.Errorf("fetching the activities: %w", err))
	}

	listed := byCharacter[c.ID].Available
	all := activities.Read(listed, defs)
	rows := activities.Filter(all, in.Query, in.Rewards)

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
	if target != inventory.OwnerVault && target != targetInventory {
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

	name, moved, err := s.MoveStack(ctx, mover, in.ItemHash, in.Amount, target == inventory.OwnerVault)

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

func (h *handler) handleStageJunk(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in StageJunkInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s, _, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolStageJunk, err)
	}

	c, err := s.ResolveCharacter(in.Character)
	if err != nil {
		return h.toolError(ctx, toolStageJunk, err)
	}

	bucket, err := s.ResolveSlot(in.Slot)
	if err != nil {
		return h.toolError(ctx, toolStageJunk, err)
	}

	plan := s.JunkPlan(c, bucket)

	// At the end of a run the user has moved the keepers back, so the slot
	// holds keepers when no junk is left.
	if len(plan.Staged) == 0 && len(plan.Waiting) == 0 {
		return textResult(renderNoJunk(s, c, bucket, plan)), nil, nil
	}

	// The game cannot undo a dismantle, so the tool adds no junk to a slot
	// that holds an item that the user did not tag junk.
	if len(plan.Keepers) > 0 {
		return h.toolError(ctx, toolStageJunk, keeperRefusal(s, c, bucket, plan.Keepers))
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolStageJunk, err)
	}

	mover := bungieMover{h: h, membership: membership}
	batch := plan.Waiting[:min(plan.Room, len(plan.Waiting))]

	var failures []string

	changed := false

	for _, item := range batch {
		owner := item.Owner

		// The batch holds only unequipped items, so no move equips a
		// replacement.
		_, err := s.Move(ctx, mover, item, c.ID, false)

		// A move that fails can still complete its transfer to the vault.
		if item.Owner != owner {
			changed = true
		}

		if err != nil {
			h.log.WarnContext(ctx, "move failed",
				slog.String("instance_id", item.InstanceID),
				slog.Any("error", err),
			)

			failures = append(failures, fmt.Sprintf("- %s (%s) did not move: %v. It is at %s.",
				item.Name, item.InstanceID, err, s.LocationLabel(item)))
		}
	}

	// Bungie can serve the state from before a move for some seconds. A
	// snapshot that a move changed therefore stays in use for a full TTL,
	// and a call that moved nothing keeps the age of the snapshot.
	if changed {
		s.Fetched = h.now()
	}

	// Move updates the snapshot after each step that succeeds. A second
	// plan therefore matches the game, also after a move that stopped
	// halfway.
	result := textResult(renderStagedJunk(s, c, bucket, s.JunkPlan(c, bucket), failures))
	result.IsError = len(batch) > 0 && len(failures) == len(batch)

	return result, nil, nil
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

		characters = []inventory.Character{c}
	}

	// The loadouts do not depend on the names, so a table that does not
	// load leaves the loadouts in the result.
	names := h.loadoutTables.Load(ctx, bungie.LoadoutNameTable)[bungie.LoadoutNameTable]

	return textResult(renderLoadouts(s, characters, names)), nil, nil
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
		return h.toolError(ctx, toolEquipLoadout, errNoLoadoutCharacter)
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

func (h *handler) handleSaveLoadout(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in SaveLoadoutInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if in.Character == "" {
		return h.toolError(ctx, toolSaveLoadout, errNoLoadoutCharacter)
	}

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolSaveLoadout, err)
	}

	c, err := s.ResolveCharacter(in.Character)
	if err != nil {
		return h.toolError(ctx, toolSaveLoadout, err)
	}

	slot, saved, err := s.LoadoutSlot(c.ID, in.Number)
	if err != nil {
		return h.toolError(ctx, toolSaveLoadout, err)
	}

	// The name table also names the loadout in the result, so every save
	// loads it. Only an empty slot reads the icon and the color tables.
	tables := []bungie.LoadoutTable{bungie.LoadoutNameTable}
	if !saved {
		tables = append(tables, bungie.LoadoutIconTable, bungie.LoadoutColorTable)
	}

	states := h.loadoutTables.Load(ctx, tables...)

	kept := bungie.LoadoutIdentifiers{NameHash: slot.NameHash, IconHash: slot.IconHash, ColorHash: slot.ColorHash}

	ids, err := bungie.ChooseLoadoutIdentifiers(kept, saved, in.Name, states)
	if err != nil {
		return h.toolError(ctx, toolSaveLoadout, err)
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolSaveLoadout, err)
	}

	action := bungie.LoadoutAction{CharacterID: c.ID, Membership: membership, Index: in.Number - 1}

	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
		return h.bungie.SnapshotLoadout(ctx, creds, action, ids)
	})
	if err != nil {
		return h.toolError(ctx, toolSaveLoadout, fmt.Errorf(
			"saving loadout %d on %s: %w", in.Number, s.OwnerLabel(c.ID), err,
		))
	}

	// Bungie can serve the loadouts from before a save for some seconds. A
	// snapshot that a save updated therefore stays in use for a full TTL.
	s.RecordLoadoutSave(c.ID, in.Number, ids)

	s.Fetched = h.now()

	return textResult(renderLoadoutSave(s, c.ID, in.Number, ids.NameHash, states[bungie.LoadoutNameTable])), nil, nil
}

func (h *handler) handleClearLoadout(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ClearLoadoutInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if in.Character == "" {
		return h.toolError(ctx, toolClearLoadout, errNoLoadoutCharacter)
	}

	s, err := h.loadSnapshot(ctx, false)
	if err != nil {
		return h.toolError(ctx, toolClearLoadout, err)
	}

	c, err := s.ResolveCharacter(in.Character)
	if err != nil {
		return h.toolError(ctx, toolClearLoadout, err)
	}

	slot, saved, err := s.LoadoutSlot(c.ID, in.Number)
	if err != nil {
		return h.toolError(ctx, toolClearLoadout, err)
	}

	if !saved {
		return h.toolError(ctx, toolClearLoadout, fmt.Errorf(
			"%w: %s has no saved loadout in slot %d, so there is nothing to clear",
			inventory.ErrEmptyLoadoutSlot, s.OwnerLabel(c.ID), in.Number,
		))
	}

	membership, err := h.membership(ctx)
	if err != nil {
		return h.toolError(ctx, toolClearLoadout, err)
	}

	action := bungie.LoadoutAction{CharacterID: c.ID, Membership: membership, Index: in.Number - 1}

	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
		return h.bungie.ClearLoadout(ctx, creds, action)
	})
	if err != nil {
		return h.toolError(ctx, toolClearLoadout, fmt.Errorf(
			"clearing loadout %d on %s: %w", in.Number, s.OwnerLabel(c.ID), err,
		))
	}

	// Bungie can serve the loadouts from before a clear for some seconds. A
	// snapshot that a clear updated therefore stays in use for a full TTL.
	s.RecordLoadoutClear(c.ID, in.Number)

	s.Fetched = h.now()

	return textResult(renderLoadoutClear(s, slot)), nil, nil
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
	if tag == inventory.TagNone {
		tag = ""
	}

	if tag != "" && !slices.Contains(dvm.TagOrder, tag) {
		return h.toolError(ctx, toolSetTag, fmt.Errorf(
			"%w: tag %q is not none or one of %s",
			ErrInvalidInput, in.Tag, strings.Join(dvm.TagOrder, ", "),
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

	var changes []dvm.ItemTag

	for _, item := range items {
		changes = append(changes, dvm.TagChanges(rows, item.InstanceID, item.Hash, tag, "")...)
	}

	err = h.writeTags(ctx, changes)
	if err != nil {
		return h.toolError(ctx, toolSetTag, err)
	}

	// A tag that a person sets carries no rule id.
	for _, item := range items {
		item.Tag = tag
		item.TagRule = ""
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

	changes := make([]dvm.ItemTag, 0, len(items))
	for _, item := range items {
		changes = append(changes, dvm.ItemTag{
			InstanceID: item.InstanceID,
			ItemHash:   item.Hash,
			TagID:      dvm.TagFavorite,
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
		kind = dvm.NoteInstance
	}

	if !slices.Contains([]string{dvm.NoteInstance, dvm.NoteItem, dvm.NoteSet}, kind) {
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

	change := dvm.Note{Kind: kind, TargetID: in.TargetID, Body: body, Removed: body == ""}

	var item *inventory.Item

	if kind == dvm.NoteInstance {
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
	changes := make([]dvm.PerkPreference, 0, len(in.Preferences))

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

// handleListAutotagRules lists the autotag rules and the autotag state.
// With matches=new, it shows the rule that takes each item that the next
// claim offers to the rules. It claims nothing. With matches=owned, it
// counts the owned items that each rule matches, as the rule editor of the
// web client counts them. It refuses any other matches value before it
// sends a request.
func (h *handler) handleListAutotagRules(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in ListAutotagRulesInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	matches := strings.ToLower(strings.TrimSpace(in.Matches))
	if matches != "" && matches != matchesNew && matches != matchesOwned {
		return h.toolError(ctx, toolListAutotagRules,
			fmt.Errorf("%w: matches %q is not %s or %s", ErrInvalidInput, in.Matches, matchesNew, matchesOwned))
	}

	snapshot, err := h.autotag(ctx)
	if err != nil {
		return h.toolError(ctx, toolListAutotagRules, err)
	}

	var text string

	switch matches {
	case matchesNew:
		text, err = h.newItemMatches(ctx, snapshot)
	case matchesOwned:
		text, err = h.ownedMatches(ctx, snapshot)
	default:
		return textResult(renderAutotag(snapshot)), nil, nil
	}

	if err != nil {
		return h.toolError(ctx, toolListAutotagRules, err)
	}

	return textResult(text), nil, nil
}

// newItemMatches returns the text of list_autotag_rules with matches=new.
// It reads a fresh profile, because a cached profile can miss the newest
// drops. It reads the profile only while autotagging is on, because the
// web client runs the rules only then. It returns the error of the
// profile, tag, or note read.
func (h *handler) newItemMatches(ctx context.Context, snapshot dvm.AutotagSnapshot) (string, error) {
	switch {
	case snapshot.State == nil:
		return "Autotagging has never been turned on, so no item waits for the rules.", nil
	case !snapshot.State.Enabled:
		return "Autotagging is off, so no item waits for the rules.", nil
	}

	s, _, err := h.loadAnnotated(ctx, true)
	if err != nil {
		return "", err
	}

	if _, ok := s.LastPlayed(); !ok {
		return "The account has no character, so the server cannot tell which items the rules would take.", nil
	}

	stored := snapshot.State.Mark

	newest := newestMark(s)
	if newest <= stored {
		return fmt.Sprintf("No owned item has an instance id above the stored mark %s, "+
			"so the next claim offers no item to the rules.", stored), nil
	}

	held, vault := newItems(s, stored)
	evals := evalRules(
		s,
		liveRules(snapshot.Rules),
		func() map[string]rolls.RatingSource { return h.ratingSources(ctx) },
	)

	return renderNewMatches(s, stored, newest, evals, firstMatch(evals, held), vault), nil
}

// ownedMatches returns the text of list_autotag_rules with
// matches=owned. It reads the profile only when a live rule exists, and
// it reuses a snapshot up to 30 seconds old. The counts do not depend on
// the autotag state, because the rule editor of the web client shows them
// whether autotagging is on or off. It returns the error of the profile,
// tag, or note read.
func (h *handler) ownedMatches(ctx context.Context, snapshot dvm.AutotagSnapshot) (string, error) {
	rules := liveRules(snapshot.Rules)
	if len(rules) == 0 {
		return "No rules, so nothing is counted.", nil
	}

	s, _, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return "", err
	}

	if _, ok := s.LastPlayed(); !ok {
		return "The account has no character, so the server cannot count the items that the rules match.", nil
	}

	// The reasons of each count follow the order of the items, so the
	// sort keeps the text the same from one call to the next.
	items := ownedItems(s)
	slices.SortStableFunc(items, inventory.ByNameAndID)

	evals := evalRules(s, rules, func() map[string]rolls.RatingSource { return h.ratingSources(ctx) })

	return renderOwnedCounts(ownedCounts(evals, items)), nil
}

// handleSaveAutotagRule writes the rule that [mergeRule] builds. When the
// call passes a query, it refuses a query that holds a value that the web
// client rejects before it writes the rule. After the write, the result
// counts the owned items that the query matches. The line of [unbuiltLine]
// follows the count when the count leaves out items that the web client
// does not build.
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

	var query compiledQuery

	if in.Query != nil {
		query, err = compileRuleQuery(rule.Query)
		if err != nil {
			return h.toolError(ctx, toolSaveAutotagRule, err)
		}
	}

	err = h.withSession(ctx, func(session string) error {
		return h.service.WriteAutotagRules(ctx, session, []dvm.AutotagRule{rule})
	})
	if err != nil {
		return h.toolError(ctx, toolSaveAutotagRule, err)
	}

	text := "Saved the rule:\n" + renderRule(rule)
	if in.Query != nil {
		count, unbuilt := h.countRuleMatches(ctx, query)

		text += "\n" + renderSaveCount(count)
		if unbuilt != "" {
			text += "\n" + unbuilt
		}
	}

	return textResult(text), nil, nil
}

// compileRuleQuery parses and compiles query after it trims the query, as
// save_autotag_rule trims the query that it stores. It returns an error
// that wraps [ErrInvalidInput] for a query that does not parse or that
// holds a value that the web client rejects. The compiled query can still
// hold terms that the preview does not evaluate.
func compileRuleQuery(query string) (compiledQuery, error) {
	root, err := search.ParseQuery(strings.TrimSpace(query))
	if err != nil {
		return compiledQuery{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	compiled := compileQuery(root)
	if len(compiled.invalid) > 0 {
		return compiledQuery{}, invalidQuery(compiled.invalid)
	}

	return compiled, nil
}

// countRuleMatches counts the owned items that query matches over the
// items that preview_autotag_rule evaluates. It also counts the items
// whose outcome the server cannot decide. It reads the inventory only when
// the preview evaluates every term of query, and it reuses a snapshot up
// to 30 seconds old. It reads the rating sources only for a query with a
// rating filter. A game mode without ratings that the query reads gives
// the reason of the mode in place of the counts.
//
// The second result is the line of [unbuiltLine] for the counts. It is
// "" when the result holds no counts.
func (h *handler) countRuleMatches(ctx context.Context, query compiledQuery) (saveCount, string) {
	if len(query.unsupported) > 0 {
		return saveCount{unsupported: query.unsupported}, ""
	}

	s, _, err := h.loadAnnotated(ctx, false)
	if err != nil {
		return saveCount{err: err}, ""
	}

	current, ok := s.LastPlayed()
	if !ok {
		return saveCount{noCharacter: true}, ""
	}

	sources, reason := h.querySources(ctx, query)
	if reason != "" {
		return saveCount{unrated: reason}, ""
	}

	result := query.evaluate(searchContext{current: current, items: builtItems(s), sources: sources}, ownedItems(s))

	return saveCount{matches: len(result.matches), undecided: len(result.undecided)}, unbuiltLine(s)
}

// querySources returns the [rolls.RatingSource] of each game mode for a query
// with a rating filter. It also returns the [rolls.RatingReason] of the first
// game mode without ratings that the query reads, or "" when each such
// mode has ratings. A query without a rating filter gets no sources, and
// querySources then sends no request.
func (h *handler) querySources(ctx context.Context, query compiledQuery) (map[string]rolls.RatingSource, string) {
	if len(query.modes) == 0 {
		return nil, ""
	}

	sources := h.ratingSources(ctx)

	return sources, query.unratedReason(sources)
}

// handlePreviewAutotagRule lists the owned items that a rule query
// matches. It refuses a query that does not parse, that holds a value that
// the web client rejects, or that holds a term outside the filter table
// before it sends any request. It reads the rating sources only for a
// query with a rating filter, after the inventory. A game mode without
// ratings that the query reads fails the call with the reason of the
// mode. The result lists the items whose outcome the server cannot decide
// in a section of their own. The line of [unbuiltLine] ends the result,
// after a blank line, when the preview leaves out items that the web
// client does not build.
func (h *handler) handlePreviewAutotagRule(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in PreviewAutotagRuleInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	query, err := compileRuleQuery(in.Query)
	if err != nil {
		return h.toolError(ctx, toolPreviewAutotagRule, err)
	}

	if len(query.unsupported) > 0 {
		return h.toolError(ctx, toolPreviewAutotagRule, unsupportedQuery(query.unsupported))
	}

	s, _, err := h.loadAnnotated(ctx, in.Refresh)
	if err != nil {
		return h.toolError(ctx, toolPreviewAutotagRule, err)
	}

	current, ok := s.LastPlayed()
	if !ok {
		return h.toolError(ctx, toolPreviewAutotagRule, errNoCharacter)
	}

	sources, reason := h.querySources(ctx, query)
	if reason != "" {
		return h.toolError(ctx, toolPreviewAutotagRule, unratedQuery(reason))
	}

	owned := ownedItems(s)
	result := query.evaluate(searchContext{current: current, items: builtItems(s), sources: sources}, owned)
	matches := result.matches

	limit := in.Limit
	if limit <= 0 {
		limit = defaultItemLimit
	}

	limit = min(limit, maxItemLimit)
	offset := min(max(in.Offset, 0), len(matches))
	page := matches[offset:min(offset+limit, len(matches))]

	text := renderPreview(s, page, offset, len(matches), len(owned), result.undecided, limit)
	if unbuilt := unbuiltLine(s); unbuilt != "" {
		text += "\n\n" + unbuilt
	}

	return textResult(text), nil, nil
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

	i := slices.IndexFunc(rules, func(rule dvm.AutotagRule) bool { return rule.ID == in.ID })
	if i < 0 {
		return h.toolError(ctx, toolRemoveAutotagRule,
			fmt.Errorf("%w: no autotag rule has the id %q", ErrInvalidInput, in.ID))
	}

	rule := rules[i]
	rule.Removed = true
	rule.UpdatedAt = 0

	err = h.withSession(ctx, func(session string) error {
		return h.service.WriteAutotagRules(ctx, session, []dvm.AutotagRule{rule})
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

		mark = newestMark(s)
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
			fmt.Errorf("%w: the service did not apply the change", dvm.ErrService))

	case in.Enabled:
		return textResult("Autotagging is on. The rules apply to items acquired from now on. " +
			"The web client runs them while it is open, " +
			"and run_autotag_rules runs them when a caller invokes it."), nil, nil

	default:
		return textResult("Autotagging is off."), nil, nil
	}
}

// handleRunAutotagRules runs the autotag rules on the new items, as the
// web client does while it is open. It reads the autotag state and a
// fresh profile, and it evaluates every enabled rule over the new items
// before it claims anything. It claims the new items through the service
// only when it can apply every enabled rule the way the web client does.
// After an applied claim, it writes the tag of each rule with the rule id.
// It then moves each item of a rule with the vault flag that the web
// client moves to the vault, as [vaultBlock] decides, also after a failed
// tag write. Every result is an error result except the result of an
// applied claim whose tag write and moves succeeded. A claim happens at
// most once, so the handler retries neither the claim, the tag write, nor
// a move.
func (h *handler) handleRunAutotagRules(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ EmptyInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	refuse := func(text string) (*mcp.CallToolResult, any, error) {
		return h.toolRefusal(ctx, toolRunAutotagRules, text)
	}

	snapshot, err := h.autotag(ctx)
	if err != nil {
		return refuse("run_autotag_rules claimed nothing, because the server could not read the autotag state: " +
			errorText(err))
	}

	switch {
	case snapshot.State == nil:
		return refuse("Autotagging has never been turned on, so run_autotag_rules claimed nothing. " +
			"Turn it on with set_autotag_enabled first.")

	case !snapshot.State.Enabled:
		return refuse("Autotagging is off, so run_autotag_rules claimed nothing. " +
			"Turn it on with set_autotag_enabled first.")
	}

	// A cached profile can miss the newest drops and claim up to a lower
	// mark.
	s, rows, err := h.loadAnnotated(ctx, true)
	if err != nil {
		return refuse("run_autotag_rules claimed nothing, because the server could not read the inventory: " +
			errorText(err))
	}

	if _, ok := s.LastPlayed(); !ok {
		return refuse("The account has no character, so run_autotag_rules claimed nothing.")
	}

	stored := snapshot.State.Mark

	// The service refuses a claim whose new mark is not higher than the
	// stored mark.
	newest := newestMark(s)
	if newest <= stored {
		return refuse(fmt.Sprintf("No owned item has an instance id above the stored mark %s, "+
			"so run_autotag_rules claimed nothing.", stored))
	}

	held, vault := newItems(s, stored)
	evals := evalRules(
		s,
		liveRules(snapshot.Rules),
		func() map[string]rolls.RatingSource { return h.ratingSources(ctx) },
	)
	m := firstMatch(evals, held)

	refusal := renderRunRefusal(evals, m)
	if refusal != "" {
		return refuse(refusal)
	}

	tags := ruleTags(evals, m)

	// The handler reads the membership and the definitions before the
	// claim. A failed read then leaves the items of the vault rules to the
	// next claim, and never strands claimed items that no move reached.
	var (
		mover   bungieMover
		vaulted []vaultItem
	)

	if slices.ContainsFunc(evals, func(e ruleEval) bool { return e.rule.Enabled && e.rule.Vault }) {
		membership, err := h.membership(ctx)
		if err != nil {
			return refuse("run_autotag_rules claimed nothing, because the server could not read the inventory: " +
				errorText(err))
		}

		defs, err := h.defs.Get(ctx)
		if err != nil {
			return refuse("run_autotag_rules claimed nothing, because the server could not read the inventory: " +
				errorText(fmt.Errorf("loading definitions: %w", err)))
		}

		mover = bungieMover{h: h, membership: membership}
		vaulted = vaultItems(evals, m, defs)
	}

	// The service checks the session before it reads the claim, so the
	// repeat of withSession after a rejected session claims at most once.
	applied, err := sessionValue(ctx, h, func(session string) (bool, error) {
		return h.service.ClaimAutotag(ctx, session, stored, newest)
	})
	if err != nil {
		return refuse(h.claimFailure(ctx, stored, err, tags, vaulted))
	}

	if !applied {
		return refuse("The service did not apply the claim, so run_autotag_rules tagged nothing. " +
			"Another runner, such as the open web client, claimed these items first, or autotagging was turned off.")
	}

	var changes []dvm.ItemTag

	for _, t := range tags {
		changes = append(changes, dvm.TagChanges(rows, t.item.InstanceID, t.item.Hash, t.rule.TagID, t.rule.ID)...)
	}

	tagErr := h.writeTags(ctx, changes)
	if tagErr == nil {
		// The cached snapshot holds the tags that the rules set.
		for _, t := range tags {
			t.item.Tag = t.rule.TagID
			t.item.TagRule = t.rule.ID
		}
	}

	// The web client queues its tag write and moves the items of the
	// vault rules whatever the write gives.
	h.moveToVault(ctx, s, mover, vaulted)

	if tagErr != nil {
		return refuse(renderTagFailure(s, stored, newest, tagErr, tags, vaulted))
	}

	text := renderRun(s, stored, newest, evals, m, vault, vaulted)

	// The user moves the item of a failed move by hand.
	if slices.ContainsFunc(vaulted, func(v vaultItem) bool { return v.err != nil }) {
		return refuse(text)
	}

	return textResult(text), nil, nil
}

// moveToVault moves each item of items without a reason to the vault, one
// item at a time. It records the error of each move that fails, and a
// failed move does not stop the next one. A move that changed an item
// resets the fetch time of s, as move_items does.
func (h *handler) moveToVault(
	ctx context.Context,
	s *inventory.Snapshot,
	mover inventory.ItemMover,
	items []vaultItem,
) {
	changed := false

	for i := range items {
		v := &items[i]
		if v.reason != "" {
			continue
		}

		owner, postmaster := v.item.Owner, v.item.Postmaster

		// vaultBlock keeps an equipped item in place, so no move equips a
		// replacement.
		_, err := s.Move(ctx, mover, v.item, inventory.OwnerVault, false)

		// A move that fails can still pull the item from the Postmaster.
		if v.item.Owner != owner || v.item.Postmaster != postmaster {
			changed = true
		}

		if err != nil {
			v.err = err

			h.log.WarnContext(ctx, "move failed",
				slog.String("instance_id", v.item.InstanceID),
				slog.Any("error", err),
			)
		}
	}

	if changed {
		s.Fetched = h.now()
	}
}

// claimFailure returns the text of a claim request that returned err. The
// request might still have run the claim in the service, so claimFailure
// reads the autotag state again and compares its mark with stored. When
// the mark moved or the read does not succeed, the text names each item
// of tags with the tag of its rule. It also names each item of vaulted
// that the web client moves to the vault, and it ends with [claimOnce].
func (h *handler) claimFailure(
	ctx context.Context,
	stored string,
	err error,
	tags []ruleTag,
	vaulted []vaultItem,
) string {
	again, againErr := h.autotag(ctx)
	if againErr == nil && again.State == nil {
		againErr = errNoAutotagState
	}

	switch {
	case againErr != nil:
		return renderUnsureClaim(fmt.Sprintf("The claim request failed: %v. "+
			"The server could not read the autotag state again: %v. The claim might have applied. "+
			"run_autotag_rules tagged nothing.", err, againErr), tags, vaulted)

	case again.State.Mark == stored:
		return fmt.Sprintf("The claim request failed: %v. The stored mark is still %s, "+
			"so run_autotag_rules claimed nothing and tagged nothing.", err, stored)

	default:
		return renderUnsureClaim(fmt.Sprintf("The claim request failed: %v. The stored mark moved from %s to %s, "+
			"so the claim might have applied, for this call or for another runner. "+
			"run_autotag_rules tagged nothing.", err, stored, again.State.Mark), tags, vaulted)
	}
}

func (h *handler) handleGetStats(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in GetStatsInput,
) (*mcp.CallToolResult, any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	period, err := stats.PeriodOf(in.Period)
	if err != nil {
		return h.toolError(ctx, toolGetStats, err)
	}

	to := h.now()
	from := period.Start(to)

	defs, err := h.defs.Get(ctx)
	if err != nil {
		return h.toolError(ctx, toolGetStats, fmt.Errorf("loading definitions: %w", err))
	}

	cursor := stats.CursorFor(from)

	chunks, err := sessionValue(ctx, h, func(session string) ([]dvm.StatChunk, error) {
		return h.service.Stats(ctx, session, cursor)
	})
	if err != nil {
		return h.toolError(ctx, toolGetStats, fmt.Errorf("reading recorded changes: %w", err))
	}

	summary := stats.Summarize(chunks, from, to)
	summary.ReadFrom = time.UnixMilli(cursor.UpdatedAt).In(to.Location())

	return textResult(renderStats(defs, period, summary)), nil, nil
}

// ratingMinimum is one minimum roll rating of a list_items call. Input
// names the tool input that sets it. Tier is the lowest tier of the
// game mode that passes, and it is one of [rolls.Tiers].
type ratingMinimum struct {
	input string
	mode  string
	tier  string
}

// ratingMinimums returns the minimum roll ratings that in sets, in the
// order of [rolls.Modes]. An empty value sets no minimum. It reads a
// tier letter without regard to case and without trimming. A value that
// is not a tier letter returns an error that wraps [ErrInvalidInput] and
// names the first such input.
func ratingMinimums(in ListItemsInput) ([]ratingMinimum, error) {
	var minimums []ratingMinimum

	for _, m := range []struct{ input, mode, value string }{
		{input: "min_pve_rating", mode: rolls.ModePvE, value: in.MinPvERating},
		{input: "min_pvp_rating", mode: rolls.ModePvP, value: in.MinPvPRating},
	} {
		if m.value == "" {
			continue
		}

		tier := strings.ToUpper(m.value)
		if !slices.Contains(rolls.Tiers, tier) {
			return nil, fmt.Errorf("%w: %s %q is not S, A, B, C, D, E, or F", ErrInvalidInput, m.input, m.value)
		}

		minimums = append(minimums, ratingMinimum{input: m.input, mode: m.mode, tier: tier})
	}

	return minimums, nil
}

// keepRated removes the items that do not pass every minimum from
// items, in place, and returns the rest. Sources holds the
// [rolls.RatingSource] of each game mode.
//
// An item passes a minimum when it is a weapon and [rolls.RatedAtLeast]
// accepts the [rolls.Rating] that [rolls.Rate] gives it for the game mode. Only
// weapons have roll ratings, so every other item passes no minimum,
// also when a sheet lists its hash.
//
// A game mode without ratings cannot tell which copies pass. When a
// minimum names such a mode, keepRated removes no item. It returns an
// error that gives the name of the input and then the reason of the
// mode, for the first such minimum. A mode without ratings therefore
// never reads as a result without items.
func keepRated(
	items []*inventory.Item,
	minimums []ratingMinimum,
	sources map[string]rolls.RatingSource,
) ([]*inventory.Item, error) {
	for _, m := range minimums {
		err := sources[m.mode].Err
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m.input, err)
		}
	}

	for _, m := range minimums {
		source := sources[m.mode]

		items = slices.DeleteFunc(items, func(item *inventory.Item) bool {
			return item.Kind != inventory.KindWeapon || !rolls.RatedAtLeast(rolls.Rate(source, item), m.tier)
		})
	}

	return items, nil
}

// perkChange validates one preference and converts it to a service row.
//
// The web client matches a stored preference to a perk by exact display
// name. perkChange therefore replaces the given name with the display name
// of the weapon trait in traits. It rejects a name that is no trait,
// because the web client never matches such a row. A removal passes an
// unknown name through, so the tool can clear a row that an earlier write
// left behind.
func perkChange(in PerkPreferenceInput, traits map[string]string) (dvm.PerkPreference, error) {
	mode := strings.ToLower(in.Mode)
	stance := strings.ToLower(in.Stance)
	perk := definitions.BasePerkName(in.Perk)

	if !slices.Contains(rolls.Modes, mode) {
		return dvm.PerkPreference{}, fmt.Errorf("%w: mode %q is not pve or pvp", ErrInvalidInput, in.Mode)
	}

	if perk == "" {
		return dvm.PerkPreference{}, fmt.Errorf("%w: a preference needs a perk name", ErrInvalidInput)
	}

	if stance != stanceNone && !slices.Contains(perkStances, stance) {
		return dvm.PerkPreference{}, fmt.Errorf("%w: stance %q is not up, down, or none", ErrInvalidInput, in.Stance)
	}

	name, known := traits[strings.ToLower(perk)]
	if known {
		perk = name
	}

	if stance == stanceNone {
		// The service validates the stance of a removed row too.
		return dvm.PerkPreference{Mode: mode, Perk: perk, Stance: perkStances[0], Removed: true}, nil
	}

	if !known {
		return dvm.PerkPreference{}, unknownTrait(perk, traits)
	}

	return dvm.PerkPreference{Mode: mode, Perk: perk, Stance: stance}, nil
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
// creates a rule that evaluates last. When the call passes a query,
// mergeRule parses the query with [search.ParseQuery]. It rejects a query
// that the search parser of the web client rejects and a query that the
// web client runs differently from its text. It leaves a stored query
// unchecked, so a call can still disable, move, or retag a stored rule
// whose query does not parse.
//
// mergeRule also rejects a built rule that has no action, which is a rule
// with an empty tag id and a false vault flag. An enabled rule without an
// action takes the items that it matches from the rules after it and
// neither tags nor vaults them. A stored rule without an action also
// disables the Save button of the rule editor of the web client until the
// user gives the rule an action or removes it.
func mergeRule(rules []dvm.AutotagRule, in SaveAutotagRuleInput) (dvm.AutotagRule, error) {
	var rule dvm.AutotagRule

	if in.ID == "" {
		if in.Query == nil {
			return dvm.AutotagRule{}, fmt.Errorf("%w: a new rule needs a query", ErrInvalidInput)
		}

		id, err := newRuleID()
		if err != nil {
			return dvm.AutotagRule{}, err
		}

		rule = dvm.AutotagRule{ID: id, Enabled: true}

		for _, other := range rules {
			rule.Position = max(rule.Position, other.Position+1)
		}
	} else {
		i := slices.IndexFunc(rules, func(rule dvm.AutotagRule) bool { return rule.ID == in.ID })
		if i < 0 {
			return dvm.AutotagRule{}, fmt.Errorf("%w: no autotag rule has the id %q", ErrInvalidInput, in.ID)
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
		return dvm.AutotagRule{}, fmt.Errorf(
			"%w: a rule query has between 1 and %d characters", ErrInvalidInput, maxQueryLength,
		)
	}

	if in.Query != nil {
		_, err := search.ParseQuery(rule.Query)
		if err != nil {
			return dvm.AutotagRule{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}

	if rule.TagID != "" && !slices.Contains(dvm.TagOrder, rule.TagID) {
		return dvm.AutotagRule{}, fmt.Errorf(
			"%w: tag_id %q is not one of %s",
			ErrInvalidInput, rule.TagID, strings.Join(dvm.TagOrder, ", "),
		)
	}

	if rule.TagID == "" && !rule.Vault {
		return dvm.AutotagRule{}, fmt.Errorf(
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
func findItems(s *inventory.Snapshot, ids []string) ([]*inventory.Item, error) {
	if len(ids) == 0 || len(ids) > maxBatch {
		return nil, fmt.Errorf("%w: pass between 1 and %d instance ids", ErrInvalidInput, maxBatch)
	}

	items := make([]*inventory.Item, 0, len(ids))

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
func (h *handler) loadSnapshot(ctx context.Context, refresh bool) (*inventory.Snapshot, error) {
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

	var profile *bungie.Profile

	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
		var err error

		profile, err = h.bungie.Profile(ctx, creds, membership)

		return err //nolint:wrapcheck // The client error reaches the tool text unchanged.
	})
	if err != nil {
		return nil, fmt.Errorf("fetching the profile: %w", err)
	}

	h.snapshot = inventory.NewSnapshot(profile, defs, now)

	return h.snapshot, nil
}

// loadAnnotated returns the snapshot with the tags and notes that the
// service stores at the time of the call. It also returns the tag rows.
func (h *handler) loadAnnotated(ctx context.Context, refresh bool) (*inventory.Snapshot, []dvm.ItemTag, error) {
	s, err := h.loadSnapshot(ctx, refresh)
	if err != nil {
		return nil, nil, err
	}

	var (
		tags  []dvm.ItemTag
		notes []dvm.Note
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

func (h *handler) writeTags(ctx context.Context, changes []dvm.ItemTag) error {
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
		if note.Kind == dvm.NoteInstance && note.TargetID == instanceID && !note.Removed && note.ItemHash != nil {
			return note.ItemHash, nil
		}
	}

	return nil, fmt.Errorf(
		"%w: no item in the inventory and no saved note has the instance id %s",
		ErrInvalidInput, instanceID,
	)
}

func (h *handler) notes(ctx context.Context) ([]dvm.Note, error) {
	return sessionValue(ctx, h, func(session string) ([]dvm.Note, error) {
		return h.service.Notes(ctx, session)
	})
}

func (h *handler) perkPreferences(ctx context.Context) ([]dvm.PerkPreference, error) {
	return sessionValue(ctx, h, func(session string) ([]dvm.PerkPreference, error) {
		return h.service.PerkPreferences(ctx, session)
	})
}

// ratingSources returns the [rolls.RatingSource] of each game mode, keyed by
// the mode. It does not read the item, so one result rates every item
// of a call.
//
// It reads the roll sheets through the sheet store, then the perk
// preferences from the service, then the definitions through the
// definition loader. The read of the preferences runs on every call,
// also when no mode has a sheet, and the handler keeps no copy of them.
// A preference that the user changes in the web client therefore counts
// in the next call. The read of the definitions also runs on every
// call. The loader serves the loaded definitions from memory between two
// checks of the manifest version, so most calls send no request for them.
//
// A mode without a sheet carries the reason of the sheet. When the
// server cannot read the preferences, every other mode carries that
// reason. When the server reads the preferences but cannot read the
// definitions, every other mode carries the reason of the definitions.
// Both reasons wrap [rolls.ErrNoRating] and the error of the read. The method
// therefore returns no error, and each caller decides what a mode
// without ratings costs.
//
// The definitions can belong to a later manifest version than the
// definitions that built the cached snapshot. A rating takes the names
// of the picks and the plug sets of the weapon from them. The later
// version therefore changes only those names and the socket of a column
// in which the copy rolled no pick. The next profile fetch builds the
// snapshot from the later version.
func (h *handler) ratingSources(ctx context.Context) map[string]rolls.RatingSource {
	states := h.sheets.Load(ctx)

	prefs, prefsErr := h.perkPreferences(ctx)
	if prefsErr != nil {
		prefsErr = fmt.Errorf("%w: reading perk preferences: %w", rolls.ErrNoRating, prefsErr)
	}

	defs, defsErr := h.defs.Get(ctx)
	if defsErr != nil {
		defsErr = fmt.Errorf("%w: loading definitions: %w", rolls.ErrNoRating, defsErr)
	}

	stances := rolls.StancesByMode(prefs)
	sources := make(map[string]rolls.RatingSource, len(rolls.Modes))

	// The cases follow the order of the reads, so the reason of an
	// earlier read stays in place of the reason of a later read.
	for _, mode := range rolls.Modes {
		state := states[mode]

		switch {
		case state.Err != nil:
			sources[mode] = rolls.RatingSource{Err: state.Err}
		case prefsErr != nil:
			sources[mode] = rolls.RatingSource{Err: prefsErr}
		case defsErr != nil:
			sources[mode] = rolls.RatingSource{Err: defsErr}
		default:
			sources[mode] = rolls.RatingSource{Sheet: state.Sheet, Stances: stances[mode], Defs: defs}
		}
	}

	return sources
}

func (h *handler) autotag(ctx context.Context) (dvm.AutotagSnapshot, error) {
	return sessionValue(ctx, h, func(session string) (dvm.AutotagSnapshot, error) {
		return h.service.Autotag(ctx, session)
	})
}

// membership returns the Destiny membership of the signed-in account. The
// first call after a sign-in asks Bungie and records the answer in the
// token file.
func (h *handler) membership(ctx context.Context) (bungie.Membership, error) {
	t, err := h.auth.Tokens(ctx)
	if err != nil {
		return bungie.Membership{}, err //nolint:wrapcheck // The sign-in error reaches the tool text unchanged.
	}

	if !t.Membership.IsZero() {
		return t.Membership, nil
	}

	var m bungie.Membership

	err = h.withBungie(ctx, func(creds bungie.Credentials) error {
		var err error

		m, err = h.bungie.PrimaryMembership(ctx, creds)

		return err //nolint:wrapcheck // The client error reaches the tool text unchanged.
	})
	if err != nil {
		return bungie.Membership{}, fmt.Errorf("resolving the Destiny membership: %w", err)
	}

	err = h.auth.SetMembership(t.BungieID, m)
	if err != nil {
		h.log.WarnContext(ctx, "recording membership", slog.Any("error", err))
	}

	return m, nil
}

// withBungie runs fn with Bungie credentials. When Bungie rejects the
// access token, it refreshes the tokens and runs fn a second time.
func (h *handler) withBungie(ctx context.Context, fn func(bungie.Credentials) error) error {
	t, err := h.auth.Tokens(ctx)
	if err != nil {
		return err //nolint:wrapcheck // The sign-in error reaches the tool text unchanged.
	}

	key, err := h.bungieKey(ctx)
	if err != nil {
		return err
	}

	err = fn(bungie.Credentials{APIKey: key, AccessToken: t.AccessToken})
	if !errors.Is(err, bungie.ErrUnauthorized) {
		return err
	}

	t, err = h.auth.Refresh(ctx, t.AccessToken)
	if err != nil {
		return err //nolint:wrapcheck // The sign-in error reaches the tool text unchanged.
	}

	return fn(bungie.Credentials{APIKey: key, AccessToken: t.AccessToken})
}

// withSession runs fn with the service session token. When the service
// rejects the session, it refreshes the tokens and runs fn a second time.
func (h *handler) withSession(ctx context.Context, fn func(session string) error) error {
	t, err := h.auth.Tokens(ctx)
	if err != nil {
		return err //nolint:wrapcheck // The sign-in error reaches the tool text unchanged.
	}

	err = fn(t.Session)
	if !errors.Is(err, dvm.ErrUnauthorized) {
		return err
	}

	t, err = h.auth.Refresh(ctx, t.AccessToken)
	if err != nil {
		return err //nolint:wrapcheck // The sign-in error reaches the tool text unchanged.
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

// bungieMover adapts the handler to [inventory.ItemMover],
// [inventory.ItemLocker], [inventory.PlugInserter], and
// [inventory.LoadoutEquipper]. It adds the membership and the credentials
// to each action.
type bungieMover struct {
	h          *handler
	membership bungie.Membership
}

// Transfer implements [inventory.ItemMover].
func (m bungieMover) Transfer(ctx context.Context, action bungie.ItemAction, toVault bool) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		return m.h.bungie.Transfer(ctx, creds, action, toVault)
	})
}

// Equip implements [inventory.ItemMover].
func (m bungieMover) Equip(ctx context.Context, action bungie.ItemAction) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		return m.h.bungie.Equip(ctx, creds, action)
	})
}

// PullFromPostmaster implements [inventory.ItemMover].
func (m bungieMover) PullFromPostmaster(ctx context.Context, action bungie.ItemAction) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		return m.h.bungie.PullFromPostmaster(ctx, creds, action)
	})
}

// SetLockState implements [inventory.ItemLocker].
func (m bungieMover) SetLockState(ctx context.Context, action bungie.ItemAction, locked bool) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		return m.h.bungie.SetLockState(ctx, creds, action, locked)
	})
}

// InsertSocketPlugFree implements [inventory.PlugInserter].
func (m bungieMover) InsertSocketPlugFree(
	ctx context.Context,
	action bungie.ItemAction,
	socket int,
	plugHash uint32,
) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		return m.h.bungie.InsertSocketPlugFree(ctx, creds, action, socket, plugHash)
	})
}

// EquipLoadout implements [inventory.LoadoutEquipper].
func (m bungieMover) EquipLoadout(ctx context.Context, action bungie.LoadoutAction) error {
	action.Membership = m.membership

	return m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		return m.h.bungie.EquipLoadout(ctx, creds, action)
	})
}

// bungieManifest adapts the handler to [bungie.ManifestSource]. It adds the
// credentials to each manifest read.
type bungieManifest struct {
	h *handler
}

// Manifest implements [bungie.ManifestSource].
func (m bungieManifest) Manifest(ctx context.Context) (map[string]string, error) {
	var paths map[string]string

	err := m.h.withBungie(ctx, func(creds bungie.Credentials) error {
		var err error

		paths, err = m.h.bungie.Manifest(ctx, creds)

		return err //nolint:wrapcheck // The client error reaches the tool text unchanged.
	})

	return paths, err
}

// Content implements [bungie.ManifestSource].
func (m bungieManifest) Content(ctx context.Context, path string) (io.ReadCloser, error) {
	return m.h.bungie.Content(ctx, path) //nolint:wrapcheck // The client error reaches the tool text unchanged.
}

// toolError logs err and returns it as a tool result with
// [*mcp.CallToolResult.IsError] set, so the model reads the reason.
func (h *handler) toolError(ctx context.Context, tool string, err error) (*mcp.CallToolResult, any, error) {
	h.log.WarnContext(ctx, "tool error",
		slog.String("tool", tool),
		slog.Any("error", err),
	)

	return errorResult(errorText(err)), nil, nil
}

// toolRefusal logs text and returns it as a tool result with
// [*mcp.CallToolResult.IsError] set. A handler calls it for an error
// result whose text it writes in full.
func (h *handler) toolRefusal(ctx context.Context, tool, text string) (*mcp.CallToolResult, any, error) {
	h.log.WarnContext(ctx, "tool error",
		slog.String("tool", tool),
		slog.String("error", text),
	)

	return errorResult(text), nil, nil
}

// errorText returns the text of err for a tool result. When nobody is
// signed in, the text tells the model to call the login tool.
func errorText(err error) string {
	text := err.Error()
	if errors.Is(err, auth.ErrNotSignedIn) {
		text += ". Call the login tool to sign in."
	}

	return text
}

func errorResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
		IsError: true,
	}
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
