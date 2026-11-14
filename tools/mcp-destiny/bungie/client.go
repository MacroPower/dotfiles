package bungie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpbody"
)

const (
	defaultURL        = "https://www.bungie.net/Platform"
	defaultContentURL = "https://www.bungie.net"
	defaultUserAgent  = "MCP-Destiny"
	defaultTimeout    = 60 * time.Second

	// maxResponseBytes caps a JSON API response body.
	maxResponseBytes = 32 << 20

	// maxContentBytes caps one content file of the manifest.
	maxContentBytes = 128 << 20
)

// Bungie platform error codes that mean the access token is unusable.
const (
	bungieSuccess          = 1
	bungieWebAuthRequired  = 99
	bungieAccessTokenStale = 2111
)

// socketArrayDefault is the Bungie DestinySocketArrayType value that
// selects the socket list of an item, which the sockets component of a
// profile reports.
const socketArrayDefault = 0

var (
	// profileComponents lists the GetProfile components that
	// [*Client.Profile] requests. They cover profiles, profile inventories,
	// profile currencies, characters, character inventories, character
	// equipment, and character loadouts. The item components cover
	// instances, stats, sockets, and reusable plugs.
	profileComponents = []int{100, 102, 103, 200, 201, 205, 206, 300, 304, 305, 310}

	// vendorComponents lists the GetVendors components that
	// [*Client.Vendors] requests. They cover vendors, vendor categories, and
	// vendor sales.
	vendorComponents = []int{400, 401, 402}

	// activityComponents lists the GetProfile components that
	// [*Client.Activities] requests. They cover the activities of each
	// character.
	activityComponents = []int{204}

	// ErrPlatform marks failures that originate from the Bungie platform
	// API. A [*PlatformError] carries the platform error code.
	ErrPlatform = errors.New("bungie")

	// ErrUnauthorized reports that Bungie rejected the access token.
	// Callers refresh the tokens and retry.
	ErrUnauthorized = errors.New("unauthorized")
)

// PlatformError is a Bungie platform error response. It unwraps to
// [ErrPlatform], and to [ErrUnauthorized] when the code reports an
// unusable access token.
type PlatformError struct {
	Status  string
	Message string
	Code    int
}

// Error implements the error interface.
func (e *PlatformError) Error() string {
	return fmt.Sprintf("bungie: %s: %s", e.Status, e.Message)
}

// Unwrap returns the sentinel errors that this error matches.
func (e *PlatformError) Unwrap() []error {
	if e.Code == bungieWebAuthRequired || e.Code == bungieAccessTokenStale {
		return []error{ErrPlatform, ErrUnauthorized}
	}

	return []error{ErrPlatform}
}

// Credentials authenticate one Bungie platform request.
type Credentials struct {
	APIKey      string
	AccessToken string
}

// Membership identifies a Destiny account on one platform.
type Membership struct {
	ID   string `json:"membershipId"`
	Type int    `json:"membershipType"`
}

// IsZero reports whether the membership is unset.
func (m Membership) IsZero() bool {
	return m.ID == ""
}

// ProfileItem is one Bungie DestinyItemComponent.
type ProfileItem struct {
	InstanceID     string `json:"itemInstanceId"`
	Quantity       int    `json:"quantity"`
	Location       int    `json:"location"`
	TransferStatus int    `json:"transferStatus"`
	State          int    `json:"state"`
	Hash           uint32 `json:"itemHash"`
	BucketHash     uint32 `json:"bucketHash"`
	Lockable       bool   `json:"lockable"`
}

// ProfileCharacter is one Bungie DestinyCharacterComponent.
type ProfileCharacter struct {
	LastPlayed time.Time `json:"dateLastPlayed"`
	ID         string    `json:"characterId"`
	ClassType  int       `json:"classType"`
	Light      int       `json:"light"`
}

// ProfileInstance is one Bungie DestinyItemInstanceComponent. DamageType
// is the Bungie DamageType value of the instance. DamageTypeHash is the
// hash of the damage type definition of the instance, and zero when the
// instance has none.
type ProfileInstance struct {
	PrimaryStat    *ProfileStat `json:"primaryStat"`
	DamageType     int          `json:"damageType"`
	DamageTypeHash uint32       `json:"damageTypeHash"`
	IsEquipped     bool         `json:"isEquipped"`
}

// ProfileStat is one Bungie DestinyStat.
type ProfileStat struct {
	Hash  uint32 `json:"statHash"`
	Value int    `json:"value"`
}

// ProfileSocket is one Bungie DestinyItemSocketState.
type ProfileSocket struct {
	PlugHash  uint32 `json:"plugHash"`
	IsEnabled bool   `json:"isEnabled"`
	IsVisible bool   `json:"isVisible"`
}

// ProfilePlug is one Bungie DestinyItemPlugBase. CanInsert reports whether
// the plug meets the insertion rules of its definition, such as the level
// that unlocks an enhanced trait. Bungie can still refuse an insert, such
// as one that costs materials.
type ProfilePlug struct {
	Hash      uint32 `json:"plugItemHash"`
	CanInsert bool   `json:"canInsert"`
}

// ProfileItems is a list of items inside a profile component.
type ProfileItems struct {
	Items []ProfileItem `json:"items"`
}

// ProfileLoadout is one Bungie DestinyLoadoutComponent, a loadout that a
// character saved in the game. In a loadout that holds nothing, every item
// has the instance id "0". NameHash, IconHash, and ColorHash identify the
// name, the icon, and the color that the game shows for the loadout. Each
// one keys its own definition table of the Destiny manifest, such as
// DestinyLoadoutNameDefinition for NameHash.
type ProfileLoadout struct {
	Items     []ProfileLoadoutItem `json:"items"`
	NameHash  uint32               `json:"nameHash"`
	IconHash  uint32               `json:"iconHash"`
	ColorHash uint32               `json:"colorHash"`
}

// ProfileLoadoutItem is one Bungie DestinyLoadoutItemComponent.
type ProfileLoadoutItem struct {
	InstanceID string `json:"itemInstanceId"`
}

// ProfileLoadouts is the Bungie DestinyLoadoutsComponent of one character.
// The position of a loadout in Loadouts is its loadout index.
type ProfileLoadouts struct {
	Loadouts []ProfileLoadout `json:"loadouts"`
}

// Profile is the subset of a Bungie DestinyProfileResponse that the server
// reads.
type Profile struct {
	Characters struct {
		Data map[string]ProfileCharacter `json:"data"`
	} `json:"characters"`
	CharacterInventories struct {
		Data map[string]ProfileItems `json:"data"`
	} `json:"characterInventories"`
	CharacterEquipment struct {
		Data map[string]ProfileItems `json:"data"`
	} `json:"characterEquipment"`
	CharacterLoadouts struct {
		Data map[string]ProfileLoadouts `json:"data"`
	} `json:"characterLoadouts"`
	ItemComponents struct {
		Instances struct {
			Data map[string]ProfileInstance `json:"data"`
		} `json:"instances"`
		Stats struct {
			Data map[string]struct {
				Stats map[string]ProfileStat `json:"stats"`
			} `json:"data"`
		} `json:"stats"`
		Sockets struct {
			Data map[string]struct {
				Sockets []ProfileSocket `json:"sockets"`
			} `json:"data"`
		} `json:"sockets"`
		ReusablePlugs struct {
			Data map[string]struct {
				Plugs map[string][]ProfilePlug `json:"plugs"`
			} `json:"data"`
		} `json:"reusablePlugs"`
	} `json:"itemComponents"`
	ProfileInventory struct {
		Data ProfileItems `json:"data"`
	} `json:"profileInventory"`
	ProfileCurrencies struct {
		Data ProfileItems `json:"data"`
	} `json:"profileCurrencies"`
}

// HistoryActivity is the subset of a Bungie DestinyHistoricalStatsActivity
// that the server reads. ReferenceID is the hash of the activity
// definition. DirectorActivityHash is the hash of the activity that the
// director launched, which differs from ReferenceID for a run that a
// playlist started. InstanceID identifies the run.
type HistoryActivity struct {
	InstanceID           string `json:"instanceId"`
	ReferenceID          uint32 `json:"referenceId"`
	DirectorActivityHash uint32 `json:"directorActivityHash"`
	Mode                 int    `json:"mode"`
}

// HistoryValue is the subset of a Bungie DestinyHistoricalStatsValue that
// the server reads.
type HistoryValue struct {
	Basic struct {
		Value float64 `json:"value"`
	} `json:"basic"`
}

// HistoryPeriod is one Bungie DestinyHistoricalStatsPeriodGroup, which
// records one run of an activity by one character. Period is the start time
// of the run. Values maps each stat id, such as "kills", to its value.
type HistoryPeriod struct {
	Period   time.Time               `json:"period"`
	Values   map[string]HistoryValue `json:"values"`
	Activity HistoryActivity         `json:"activityDetails"`
}

// Value returns the value of the stat with the id key, or zero when the
// run lacks the stat.
func (p HistoryPeriod) Value(key string) float64 {
	return p.Values[key].Basic.Value
}

// VendorComponent is the subset of a Bungie DestinyVendorComponent that
// the server reads. Enabled is false when the vendor might not be visible
// in the game. NextRefreshDate is the time when the stock of the vendor
// rotates next.
type VendorComponent struct {
	NextRefreshDate time.Time `json:"nextRefreshDate"`
	Hash            uint32    `json:"vendorHash"`
	Enabled         bool      `json:"enabled"`
}

// VendorCategory is one Bungie DestinyVendorCategory. DisplayCategoryIndex
// indexes the display categories of the vendor definition. ItemIndexes
// lists the vendor item indexes of the sales in the category, in display
// order.
type VendorCategory struct {
	ItemIndexes          []int `json:"itemIndexes"`
	DisplayCategoryIndex int   `json:"displayCategoryIndex"`
}

// VendorCategories is the subset of a Bungie
// DestinyVendorCategoriesComponent that the server reads.
type VendorCategories struct {
	Categories []VendorCategory `json:"categories"`
}

// VendorCost is one Bungie DestinyItemQuantity among the costs of a sale.
// ItemHash names the currency.
type VendorCost struct {
	ItemHash uint32 `json:"itemHash"`
	Quantity int    `json:"quantity"`
}

// VendorSale is the subset of a Bungie DestinyVendorSaleItemComponent that
// the server reads. SaleStatus holds the Bungie VendorItemStatus flags,
// which are zero when the character can buy the item. FailureIndexes
// index the failure strings of the vendor definition.
type VendorSale struct {
	FailureIndexes  []int        `json:"failureIndexes"`
	Costs           []VendorCost `json:"costs"`
	VendorItemIndex int          `json:"vendorItemIndex"`
	Quantity        int          `json:"quantity"`
	SaleStatus      int          `json:"saleStatus"`
	ItemHash        uint32       `json:"itemHash"`
}

// VendorSales is the subset of a Bungie
// PersonalDestinyVendorSaleItemSetComponent that the server reads. The
// vendor item index keys SaleItems.
type VendorSales struct {
	SaleItems map[int]VendorSale `json:"saleItems"`
}

// VendorsResponse is the subset of a Bungie DestinyVendorsResponse that the
// server reads. The vendor hash keys each map.
type VendorsResponse struct {
	Vendors struct {
		Data map[uint32]VendorComponent `json:"data"`
	} `json:"vendors"`
	Categories struct {
		Data map[uint32]VendorCategories `json:"data"`
	} `json:"categories"`
	Sales struct {
		Data map[uint32]VendorSales `json:"data"`
	} `json:"sales"`
}

// CharacterActivities is the subset of a Bungie
// DestinyCharacterActivitiesComponent that the server reads. Available
// lists the activities that the character can play.
type CharacterActivities struct {
	Available []AvailableActivity `json:"availableActivities"`
}

// AvailableActivity is the subset of a Bungie DestinyActivity that the
// server reads. Hash is the hash of the activity definition. Visible is
// false when the game hides the activity, and the decoder sets it to true
// when Bungie leaves the field out. RecommendedLight is zero when the
// activity has no recommended power. Focused marks an activity in the
// featured carousel of the Portal.
type AvailableActivity struct {
	VisibleRewards   []ActivityRewardMapping `json:"visibleRewards"`
	RecommendedLight int                     `json:"recommendedLight"`
	Hash             uint32                  `json:"activityHash"`
	Visible          bool                    `json:"isVisible"`
	Focused          bool                    `json:"isFocusedActivity"`
}

// UnmarshalJSON decodes a Bungie DestinyActivity. The web client skips an
// activity only when isVisible is false, so a missing isVisible decodes as
// true.
func (a *AvailableActivity) UnmarshalJSON(raw []byte) error {
	type plain AvailableActivity

	out := plain{Visible: true}

	err := json.Unmarshal(raw, &out)
	if err != nil {
		return fmt.Errorf("decoding activity: %w", err)
	}

	*a = AvailableActivity(out)

	return nil
}

// RewardItems returns the reward items of every reward mapping of the
// activity, in order.
func (a AvailableActivity) RewardItems() []ActivityRewardItem {
	var items []ActivityRewardItem

	for _, mapping := range a.VisibleRewards {
		items = append(items, mapping.RewardItems...)
	}

	return items
}

// ActivityRewardMapping is the subset of a Bungie
// DestinyActivityRewardMapping that the server reads.
type ActivityRewardMapping struct {
	RewardItems []ActivityRewardItem `json:"rewardItems"`
}

// ActivityRewardItem is one Bungie DestinyActivityRewardItem, a reward that
// an activity still offers this week. UIStyle names how the game shows the
// reward, such as "extra_engram" for a bonus drop, and it is empty for
// ordinary loot. Bungie drops a reward from the list after the character
// claims it, and a Quantity of zero also marks a reward that Bungie
// omits.
type ActivityRewardItem struct {
	UIStyle      string `json:"uiStyle"`
	ItemQuantity struct {
		ItemHash uint32 `json:"itemHash"`
		Quantity int    `json:"quantity"`
	} `json:"itemQuantity"`
}

// ItemAction identifies one item for a transfer, an equip, a Postmaster
// pull, a lock change, or a plug insert. CharacterID names the character
// that receives the item on a transfer from the vault, an equip, or a
// Postmaster pull. Every other action names the character that holds the
// item. For an item that no character holds, such as one in the vault or
// in an account-wide bucket, it names the most recently played character.
// StackSize is the number of units of a stackable item, and zero means
// one.
type ItemAction struct {
	InstanceID  string
	CharacterID string
	Membership  Membership
	StackSize   int
	ItemHash    uint32
}

// LoadoutAction identifies one loadout slot of a character in the game.
// The slot can hold a saved loadout or be empty. Index is the position of
// the slot on the in-game loadout screen, counted from zero.
type LoadoutAction struct {
	CharacterID string
	Membership  Membership
	Index       int
}

// LoadoutIdentifiers holds the hashes of the name, the icon, and the
// color that the game shows for a saved loadout. Each hash keys its own
// definition table of the Destiny manifest.
type LoadoutIdentifiers struct {
	NameHash  uint32
	IconHash  uint32
	ColorHash uint32
}

// Option configures a [*Client] via [New].
//
// Functions of this type:
//   - [WithURL]
//   - [WithContentURL]
//   - [WithUserAgent]
//   - [WithClock]
type Option func(*Client)

// WithURL overrides the platform base URL. It is an [Option].
func WithURL(u string) Option {
	return func(b *Client) { b.baseURL = strings.TrimRight(u, "/") }
}

// WithContentURL overrides the base URL of the content files that the
// Destiny manifest names. It is an [Option].
func WithContentURL(u string) Option {
	return func(b *Client) { b.contentURL = strings.TrimRight(u, "/") }
}

// WithUserAgent sets the User-Agent header of every request. The header
// defaults to MCP-Destiny. It is an [Option].
func WithUserAgent(ua string) Option {
	return func(b *Client) { b.userAgent = ua }
}

// WithClock sets the clock that the client reads for the changing query
// parameter of a component request. The clock defaults to [time.Now]. It
// is an [Option].
func WithClock(now func() time.Time) Option {
	return func(b *Client) { b.now = now }
}

// Client is the HTTP client for the Bungie platform API. Every platform
// request carries the API key of Agg's Destiny Vault Manager and the
// access token of the signed-in account. The client also downloads the
// content files that the Destiny manifest names, and those requests carry
// no credentials.
type Client struct {
	http       *http.Client
	now        func() time.Time
	baseURL    string
	contentURL string
	userAgent  string
}

// New returns a [*Client] configured with the given options.
func New(opts ...Option) *Client {
	b := &Client{
		http:       &http.Client{Timeout: defaultTimeout},
		now:        time.Now,
		baseURL:    defaultURL,
		contentURL: defaultContentURL,
		userAgent:  defaultUserAgent,
	}
	for _, opt := range opts {
		opt(b)
	}

	return b
}

// PrimaryMembership returns the Destiny membership that the signed-in
// account plays on. It prefers the cross save primary and falls back to the
// first membership.
func (b *Client) PrimaryMembership(ctx context.Context, creds Credentials) (Membership, error) {
	var out struct {
		PrimaryMembershipID string       `json:"primaryMembershipId"`
		DestinyMemberships  []Membership `json:"destinyMemberships"`
	}

	err := b.call(ctx, creds, http.MethodGet, "/User/GetMembershipsForCurrentUser/", nil, &out)
	if err != nil {
		return Membership{}, err
	}

	if len(out.DestinyMemberships) == 0 {
		return Membership{}, fmt.Errorf("%w: account has no Destiny memberships", ErrPlatform)
	}

	for _, m := range out.DestinyMemberships {
		if m.ID == out.PrimaryMembershipID {
			return m, nil
		}
	}

	return out.DestinyMemberships[0], nil
}

// Profile fetches the inventory of every character and the vault, and the
// currency balances of the account.
func (b *Client) Profile(ctx context.Context, creds Credentials, m Membership) (*Profile, error) {
	path := fmt.Sprintf("/Destiny2/%d/Profile/%s/?%s", m.Type, m.ID, b.componentsQuery(profileComponents))

	var out Profile

	err := b.call(ctx, creds, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// Vendors fetches the vendors of one character and the stock of each one
// at the moment of the call. Bungie lists the vendors whose stock can
// rotate, so the response leaves out preview vendors and kiosks. The stock
// and the sale status differ per character.
func (b *Client) Vendors(
	ctx context.Context,
	creds Credentials,
	m Membership,
	characterID string,
) (*VendorsResponse, error) {
	path := fmt.Sprintf(
		"/Destiny2/%d/Profile/%s/Character/%s/Vendors/?%s",
		m.Type, m.ID, characterID, b.componentsQuery(vendorComponents),
	)

	var out VendorsResponse

	err := b.call(ctx, creds, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// Activities fetches the activities that each character can play, keyed
// by character id, with the rewards that each activity still offers this
// week.
func (b *Client) Activities(
	ctx context.Context,
	creds Credentials,
	m Membership,
) (map[string]CharacterActivities, error) {
	path := fmt.Sprintf("/Destiny2/%d/Profile/%s/?%s", m.Type, m.ID, b.componentsQuery(activityComponents))

	var out struct {
		CharacterActivities struct {
			Data map[string]CharacterActivities `json:"data"`
		} `json:"characterActivities"`
	}

	err := b.call(ctx, creds, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.CharacterActivities.Data, nil
}

// Manifest returns the path of the English content file of each
// definition table of the Destiny manifest, keyed by the name of the
// table, such as DestinyLoadoutNameDefinition. [*Client.Content] opens a
// path. The map is empty when the manifest lists no English tables.
func (b *Client) Manifest(ctx context.Context, creds Credentials) (map[string]string, error) {
	var out struct {
		Paths map[string]map[string]string `json:"jsonWorldComponentContentPaths"`
	}

	err := b.call(ctx, creds, http.MethodGet, "/Destiny2/Manifest/", nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Paths["en"], nil
}

// Content opens one content file that the Destiny manifest names, such as
// a definition table. Path is a path that [*Client.Manifest] returns. The
// file lives under the content base URL, not under the platform base URL.
// The caller closes the returned reader.
func (b *Client) Content(ctx context.Context, path string) (io.ReadCloser, error) {
	u := b.contentURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", b.userAgent)

	//nolint:bodyclose // The caller closes the returned reader.
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		httpbody.Close(ctx, resp.Body, u)

		return nil, fmt.Errorf("%w: content file returned status %d", ErrPlatform, resp.StatusCode)
	}

	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, maxContentBytes), resp.Body}, nil
}

// componentsQuery returns the query string that requests components from
// a Destiny endpoint. Bungie serves a cached snapshot for a repeated URL,
// so the query also carries a changing parameter that requests the latest
// state.
func (b *Client) componentsQuery(components []int) string {
	parts := make([]string, 0, len(components))
	for _, c := range components {
		parts = append(parts, strconv.Itoa(c))
	}

	return fmt.Sprintf("components=%s&_=%d", strings.Join(parts, ","), b.now().UnixMilli())
}

// ActivityHistory fetches one page of the runs of a character, newest
// first. Page 0 holds the newest count runs. A page past the last run
// returns no rows, because Bungie leaves the activities out of that
// response.
//
// The web client requests this page without the changing query parameter
// of [*Client.Profile], and so does ActivityHistory. A page that Bungie
// serves from its cache might therefore miss a run that ended in the last
// minute.
func (b *Client) ActivityHistory(
	ctx context.Context,
	creds Credentials,
	m Membership,
	characterID string,
	count, page int,
) ([]HistoryPeriod, error) {
	path := fmt.Sprintf(
		"/Destiny2/%d/Account/%s/Character/%s/Stats/Activities/?count=%d&page=%d",
		m.Type, m.ID, characterID, count, page,
	)

	var out struct {
		Activities []HistoryPeriod `json:"activities"`
	}

	err := b.call(ctx, creds, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Activities, nil
}

// Transfer moves an item between the vault and a character.
func (b *Client) Transfer(ctx context.Context, creds Credentials, action ItemAction, toVault bool) error {
	body := map[string]any{
		"itemReferenceHash": action.ItemHash,
		"stackSize":         max(action.StackSize, 1),
		"transferToVault":   toVault,
		"itemId":            action.InstanceID,
		"characterId":       action.CharacterID,
		"membershipType":    action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/TransferItem/", body, nil)
}

// Equip equips an item that the character already holds.
func (b *Client) Equip(ctx context.Context, creds Credentials, action ItemAction) error {
	body := map[string]any{
		"itemId":         action.InstanceID,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/EquipItem/", body, nil)
}

// PullFromPostmaster moves an item from the Postmaster into the inventory
// of the character that owns it.
func (b *Client) PullFromPostmaster(ctx context.Context, creds Credentials, action ItemAction) error {
	body := map[string]any{
		"itemReferenceHash": action.ItemHash,
		"stackSize":         1,
		"itemId":            action.InstanceID,
		"characterId":       action.CharacterID,
		"membershipType":    action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/PullFromPostmaster/", body, nil)
}

// SetLockState locks or unlocks an instanced item.
func (b *Client) SetLockState(ctx context.Context, creds Credentials, action ItemAction, locked bool) error {
	body := map[string]any{
		"state":          locked,
		"itemId":         action.InstanceID,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/SetLockState/", body, nil)
}

// InsertSocketPlugFree inserts the plug with the item hash plugHash into
// the socket at index socket of an instanced item. Bungie inserts only a
// plug that costs nothing and that a later insert can reverse, such as a
// weapon perk.
func (b *Client) InsertSocketPlugFree(
	ctx context.Context,
	creds Credentials,
	action ItemAction,
	socket int,
	plugHash uint32,
) error {
	body := map[string]any{
		"plug": map[string]any{
			"socketIndex":     socket,
			"socketArrayType": socketArrayDefault,
			"plugItemHash":    plugHash,
		},
		"itemId":         action.InstanceID,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/InsertSocketPlugFree/", body, nil)
}

// EquipLoadout equips a loadout that the character saved in the game.
func (b *Client) EquipLoadout(ctx context.Context, creds Credentials, action LoadoutAction) error {
	body := map[string]any{
		"loadoutIndex":   action.Index,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Loadouts/EquipLoadout/", body, nil)
}

// SnapshotLoadout saves the gear that the character has equipped, with
// the plugs of that gear, into one loadout slot of the character. The
// saved loadout carries the name, the icon, and the color that ids
// gives.
func (b *Client) SnapshotLoadout(
	ctx context.Context,
	creds Credentials,
	action LoadoutAction,
	ids LoadoutIdentifiers,
) error {
	body := map[string]any{
		"loadoutIndex":   action.Index,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
		"nameHash":       ids.NameHash,
		"iconHash":       ids.IconHash,
		"colorHash":      ids.ColorHash,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Loadouts/SnapshotLoadout/", body, nil)
}

// ClearLoadout empties one loadout slot of the character.
func (b *Client) ClearLoadout(ctx context.Context, creds Credentials, action LoadoutAction) error {
	body := map[string]any{
		"loadoutIndex":   action.Index,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Loadouts/ClearLoadout/", body, nil)
}

// call performs one platform request and unwraps the Bungie response
// envelope into dst. A nil dst discards the response.
func (b *Client) call(ctx context.Context, creds Credentials, method, path string, body, dst any) error {
	u := b.baseURL + path

	var reader io.Reader = http.NoBody

	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}

		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", b.userAgent)
	req.Header.Set("X-Api-Key", creds.APIKey)
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	//nolint:bodyclose // The deferred httpbody.Close closes the body.
	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}

	defer httpbody.Close(ctx, resp.Body, u)

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var envelope struct {
		ErrorStatus string          `json:"ErrorStatus"`
		Message     string          `json:"Message"`
		Response    json.RawMessage `json:"Response"`
		ErrorCode   int             `json:"ErrorCode"`
	}

	err = json.Unmarshal(raw, &envelope)
	if err != nil || envelope.ErrorCode == 0 {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: bungie returned status %d", ErrUnauthorized, resp.StatusCode)
		}

		return fmt.Errorf("%w: %s returned status %d without a platform response", ErrPlatform, path, resp.StatusCode)
	}

	if envelope.ErrorCode != bungieSuccess {
		return &PlatformError{Code: envelope.ErrorCode, Status: envelope.ErrorStatus, Message: envelope.Message}
	}

	if dst == nil {
		return nil
	}

	err = json.Unmarshal(envelope.Response, dst)
	if err != nil {
		return fmt.Errorf("%w: decoding %s: %w", ErrPlatform, path, err)
	}

	return nil
}
