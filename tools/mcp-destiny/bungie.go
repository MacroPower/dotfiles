package main

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
)

const defaultBungieURL = "https://www.bungie.net/Platform"

// Bungie platform error codes that mean the access token is unusable.
const (
	bungieSuccess          = 1
	bungieWebAuthRequired  = 99
	bungieAccessTokenStale = 2111
)

// Inventory bucket hashes that the inventory logic treats specially.
const (
	bucketVault      uint32 = 138197802
	bucketPostmaster uint32 = 215593132
)

// itemLocationPostmaster is the Postmaster value of the Bungie ItemLocation
// enum.
const itemLocationPostmaster = 4

// Bit flags of the Bungie TransferStatuses and ItemState enums.
const (
	transferNotTransferable = 2

	stateLocked     = 1
	stateMasterwork = 4
	stateCrafted    = 8
)

var (
	// profileComponents lists the GetProfile components that
	// [*Bungie.Profile] requests. They cover profiles, profile inventories,
	// profile currencies, characters, character inventories, and character
	// equipment. The item components cover instances, stats, sockets, and
	// reusable plugs.
	profileComponents = []int{100, 102, 103, 200, 201, 205, 300, 304, 305, 310}

	// ErrBungie marks failures that originate from the Bungie platform API.
	// A [*BungieError] carries the platform error code.
	ErrBungie = errors.New("bungie")
)

// BungieError is a Bungie platform error response. It unwraps to
// [ErrBungie], and to [ErrUnauthorized] when the code reports an unusable
// access token.
type BungieError struct {
	Status  string
	Message string
	Code    int
}

// Error implements the error interface.
func (e *BungieError) Error() string {
	return fmt.Sprintf("bungie: %s: %s", e.Status, e.Message)
}

// Unwrap returns the sentinel errors that this error matches.
func (e *BungieError) Unwrap() []error {
	if e.Code == bungieWebAuthRequired || e.Code == bungieAccessTokenStale {
		return []error{ErrBungie, ErrUnauthorized}
	}

	return []error{ErrBungie}
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

// ProfileInstance is one Bungie DestinyItemInstanceComponent.
type ProfileInstance struct {
	PrimaryStat *ProfileStat `json:"primaryStat"`
	DamageType  int          `json:"damageType"`
	IsEquipped  bool         `json:"isEquipped"`
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

// ProfilePlug is one Bungie DestinyItemPlugBase.
type ProfilePlug struct {
	Hash uint32 `json:"plugItemHash"`
}

// ProfileItems is a list of items inside a profile component.
type ProfileItems struct {
	Items []ProfileItem `json:"items"`
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

// ItemAction identifies one item for a transfer, an equip, a Postmaster
// pull, or a lock change. CharacterID names the character that holds the
// item on a transfer to the vault or a lock change, and the character that
// receives it otherwise. A lock change of an item in the vault names the
// most recently played character. StackSize is the number of units of a
// stackable item, and zero means one.
type ItemAction struct {
	InstanceID  string
	CharacterID string
	Membership  Membership
	StackSize   int
	ItemHash    uint32
}

// BungieOption configures a [*Bungie]. See [WithBungieURL].
type BungieOption func(*Bungie)

// WithBungieURL overrides the platform base URL. It is a [BungieOption].
func WithBungieURL(u string) BungieOption {
	return func(b *Bungie) { b.baseURL = strings.TrimRight(u, "/") }
}

// Bungie is the HTTP client for the Bungie platform API. Every request
// carries the API key of Agg's Destiny Vault Manager and the access token of the
// signed-in account.
type Bungie struct {
	http      *http.Client
	now       func() time.Time
	baseURL   string
	userAgent string
}

// NewBungie returns a [*Bungie] configured with the given options.
func NewBungie(opts ...BungieOption) *Bungie {
	b := &Bungie{
		http:      &http.Client{Timeout: defaultTimeout},
		now:       time.Now,
		baseURL:   defaultBungieURL,
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(b)
	}

	return b
}

// PrimaryMembership returns the Destiny membership that the signed-in
// account plays on. It prefers the cross save primary and falls back to the
// first membership.
func (b *Bungie) PrimaryMembership(ctx context.Context, creds Credentials) (Membership, error) {
	var out struct {
		PrimaryMembershipID string       `json:"primaryMembershipId"`
		DestinyMemberships  []Membership `json:"destinyMemberships"`
	}

	err := b.call(ctx, creds, http.MethodGet, "/User/GetMembershipsForCurrentUser/", nil, &out)
	if err != nil {
		return Membership{}, err
	}

	if len(out.DestinyMemberships) == 0 {
		return Membership{}, fmt.Errorf("%w: account has no Destiny memberships", ErrBungie)
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
func (b *Bungie) Profile(ctx context.Context, creds Credentials, m Membership) (*Profile, error) {
	components := make([]string, 0, len(profileComponents))
	for _, c := range profileComponents {
		components = append(components, strconv.Itoa(c))
	}

	// Bungie serves a cached snapshot for a repeated URL, so a changing
	// query parameter requests the latest state.
	path := fmt.Sprintf(
		"/Destiny2/%d/Profile/%s/?components=%s&_=%d",
		m.Type, m.ID, strings.Join(components, ","), b.now().UnixMilli(),
	)

	var out Profile

	err := b.call(ctx, creds, http.MethodGet, path, nil, &out)
	if err != nil {
		return nil, err
	}

	return &out, nil
}

// ActivityHistory fetches one page of the runs of a character, newest
// first. Page 0 holds the newest count runs. A page past the last run
// returns no rows, because Bungie leaves the activities out of that
// response.
//
// The web client requests this page without the changing query parameter
// of [*Bungie.Profile], and so does ActivityHistory. A page that Bungie
// serves from its cache might therefore miss a run that ended in the last
// minute.
func (b *Bungie) ActivityHistory(
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
func (b *Bungie) Transfer(ctx context.Context, creds Credentials, action ItemAction, toVault bool) error {
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
func (b *Bungie) Equip(ctx context.Context, creds Credentials, action ItemAction) error {
	body := map[string]any{
		"itemId":         action.InstanceID,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/EquipItem/", body, nil)
}

// PullFromPostmaster moves an item from the Postmaster into the inventory
// of the character that owns it.
func (b *Bungie) PullFromPostmaster(ctx context.Context, creds Credentials, action ItemAction) error {
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
func (b *Bungie) SetLockState(ctx context.Context, creds Credentials, action ItemAction, locked bool) error {
	body := map[string]any{
		"state":          locked,
		"itemId":         action.InstanceID,
		"characterId":    action.CharacterID,
		"membershipType": action.Membership.Type,
	}

	return b.call(ctx, creds, http.MethodPost, "/Destiny2/Actions/Items/SetLockState/", body, nil)
}

// call performs one platform request and unwraps the Bungie response
// envelope into dst. A nil dst discards the response.
func (b *Bungie) call(ctx context.Context, creds Credentials, method, path string, body, dst any) error {
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

	resp, err := b.http.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}

	defer closeBody(ctx, resp.Body, u)

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

		return fmt.Errorf("%w: %s returned status %d without a platform response", ErrBungie, path, resp.StatusCode)
	}

	if envelope.ErrorCode != bungieSuccess {
		return &BungieError{Code: envelope.ErrorCode, Status: envelope.ErrorStatus, Message: envelope.Message}
	}

	if dst == nil {
		return nil
	}

	err = json.Unmarshal(envelope.Response, dst)
	if err != nil {
		return fmt.Errorf("%w: decoding %s: %w", ErrBungie, path, err)
	}

	return nil
}
