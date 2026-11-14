package dvm

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpbody"
)

// DefaultURL is the base URL of the deployment of Agg's Destiny Vault
// Manager that a [*Client] calls when no option sets one.
const DefaultURL = "https://destiny-vault-manager.penumbraos.workers.dev"

const (
	defaultUserAgent = "MCP-Destiny"
	defaultTimeout   = 60 * time.Second

	// maxResponseBytes caps a JSON API response body.
	maxResponseBytes = 32 << 20

	// maxArtifactBytes caps one definition artifact.
	maxArtifactBytes = 128 << 20

	// statPage is the most chunks that one page of /api/stats holds, the
	// STAT_PAGE of the service.
	statPage = 100

	// maxStatChunkLength is the longest chunk body that the service
	// accepts, in UTF-16 code units. It is the MAX_CHUNK_LENGTH of the
	// service.
	maxStatChunkLength = 1_000_000

	// maxStatPageBytes caps one page of /api/stats. Each UTF-16 code unit
	// of a chunk body takes at most 3 bytes of the JSON string that holds
	// it. The other fields of a chunk take less than 1 KiB.
	maxStatPageBytes = statPage * (3*maxStatChunkLength + 1<<10)

	// grantRefusal opens the message that the service returns when the
	// Bungie token endpoint answers with a 4xx status.
	grantRefusal = "Token endpoint returned 4"
)

var (
	// ErrService marks failures that originate from Agg's Destiny Vault
	// Manager, such as a failure status or a body that does not decode. A
	// status 401 of a call other than [*Client.Artifact] matches
	// [ErrUnauthorized] and not ErrService.
	ErrService = errors.New("service")

	// ErrUnauthorized reports that Agg's Destiny Vault Manager rejected the
	// presented token. Callers refresh the tokens and retry.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrGrantRejected reports that Bungie refused an authorization code or
	// a refresh token. The user signs in again to recover.
	ErrGrantRejected = errors.New("grant rejected")
)

// ArtifactIndex lists the definition artifacts the service publishes.
// Files maps an artifact name to its chunk file names.
type ArtifactIndex struct {
	Files           map[string][]string `json:"files"`
	ManifestVersion string              `json:"manifestVersion"`
}

// Config is the response body of /api/config. The Bungie API key and
// OAuth client id are public values that the web client also reads.
type Config struct {
	APIKey     string        `json:"apiKey"`
	ClientID   string        `json:"clientId"`
	AuthOrigin string        `json:"authOrigin"`
	Artifacts  ArtifactIndex `json:"artifacts"`
}

// TokenResponse is the response body of /api/auth/token and
// /api/auth/refresh. Session authenticates calls to the service itself, and
// AccessToken authenticates calls to Bungie.
type TokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	MembershipID     string `json:"membership_id"`
	Session          string `json:"session"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
}

// ItemTag ties one tag to one item instance. The service keeps removed rows,
// so Removed marks a tag that an item used to carry.
type ItemTag struct {
	InstanceID string `json:"instanceId"`
	TagID      string `json:"tagId"`
	RuleID     string `json:"ruleId,omitempty"`
	UpdatedAt  int64  `json:"updatedAt,omitzero"`
	ItemHash   uint32 `json:"itemHash"`
	Removed    bool   `json:"removed"`
}

// Note is free text attached to an item instance, a Collections entry, or a
// Collections armor set. ItemHash is set only for the instance kind.
type Note struct {
	ItemHash  *uint32 `json:"itemHash,omitempty"`
	Kind      string  `json:"kind"`
	TargetID  string  `json:"targetId"`
	Body      string  `json:"body"`
	UpdatedAt int64   `json:"updatedAt,omitzero"`
	Removed   bool    `json:"removed"`
}

// Note kinds that the service stores. Each kind names what the TargetID of
// a [Note] identifies.
const (
	// NoteInstance is the kind of a note on one item instance.
	NoteInstance = "instance"
	// NoteItem is the kind of a note on a Collections entry.
	NoteItem = "item"
	// NoteSet is the kind of a note on a Collections armor set.
	NoteSet = "set"
)

// PerkPreference records a liked or disliked perk for one game mode.
type PerkPreference struct {
	Mode      string `json:"mode"`
	Perk      string `json:"perk"`
	Stance    string `json:"stance"`
	UpdatedAt int64  `json:"updatedAt,omitzero"`
	Removed   bool   `json:"removed"`
}

// AutotagRule tags, vaults, or tags and vaults newly acquired items that
// match Query.
type AutotagRule struct {
	ID        string `json:"id"`
	Query     string `json:"query"`
	TagID     string `json:"tagId,omitempty"`
	UpdatedAt int64  `json:"updatedAt,omitzero"`
	Position  int    `json:"position"`
	Vault     bool   `json:"vault"`
	Enabled   bool   `json:"enabled"`
	Removed   bool   `json:"removed"`
}

// AutotagState reports whether the web client runs autotag rules. Mark is
// the highest instance id that the rules have already handled.
type AutotagState struct {
	Mark      string `json:"mark"`
	UpdatedAt int64  `json:"updatedAt"`
	Enabled   bool   `json:"enabled"`
}

// AutotagSnapshot is the response body of /api/autotag.
type AutotagSnapshot struct {
	State *AutotagState `json:"state"`
	Rules []AutotagRule `json:"rules"`
}

// StatChange is one profile change that the web client recorded. The web
// client diffs each Bungie profile that it fetches against the one before,
// and each change sits between the two profiles. From and At are the mint
// times of those profiles, in Unix milliseconds.
//
// Kind selects the fields that a change sets. A currency or stack change
// sets ItemHash, Before, and After. A currency change reads the account
// balance that the profile currencies hold. A stack change sums the
// quantity across the vault and every character, so a transfer between
// them records no change. An objective change also sets InstanceID and
// ObjectiveHash, and its Before and After are the progress of that
// objective on that item. The web client records more kinds, and
// [StatChange] decodes only the fields that the get_stats tool reads.
type StatChange struct {
	Kind          string `json:"kind"`
	InstanceID    string `json:"instanceId"`
	From          int64  `json:"from"`
	At            int64  `json:"at"`
	Before        int64  `json:"before"`
	After         int64  `json:"after"`
	ItemHash      uint32 `json:"itemHash"`
	ObjectiveHash uint32 `json:"objectiveHash"`
}

// StatChunk holds the changes of one 5-minute bucket. StartsAt, EndsAt,
// and UpdatedAt are Unix milliseconds. UpdatedAt is the service time of
// the write that stored the chunk.
type StatChunk struct {
	Changes   []StatChange
	StartsAt  int64
	EndsAt    int64
	UpdatedAt int64
}

// StatCursor marks a position in the order of /api/stats, which sorts the
// chunks by UpdatedAt and then by StartsAt. A read returns the chunks that
// follow the cursor.
type StatCursor struct {
	UpdatedAt int64
	StartsAt  int64
}

// statPageBody is one page of /api/stats.
type statPageBody struct {
	Chunks []StatChunk
	More   bool
}

// Option configures a [*Client] via [New].
//
// Functions of this type:
//   - [WithURL]
//   - [WithUserAgent]
type Option func(*Client)

// WithURL overrides the base URL of Agg's Destiny Vault Manager. It is an
// [Option].
func WithURL(u string) Option {
	return func(s *Client) { s.baseURL = strings.TrimRight(u, "/") }
}

// WithUserAgent sets the User-Agent header of every request. The header
// defaults to MCP-Destiny. It is an [Option].
func WithUserAgent(ua string) Option {
	return func(s *Client) { s.userAgent = ua }
}

// Client is the HTTP client for Agg's Destiny Vault Manager. The deployment
// stores tags, notes, perk preferences, autotag rules, and the profile
// changes that the web client records, all per Bungie account. It also
// brokers the Bungie OAuth exchange and serves the definition artifacts.
type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
}

// New returns a [*Client] configured with the given options.
func New(opts ...Option) *Client {
	s := &Client{
		http:      &http.Client{Timeout: defaultTimeout},
		baseURL:   DefaultURL,
		userAgent: defaultUserAgent,
	}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// BaseURL returns the base URL of the deployment.
func (s *Client) BaseURL() string {
	return s.baseURL
}

// Config calls /api/config.
func (s *Client) Config(ctx context.Context) (Config, error) {
	var out Config

	err := s.call(ctx, http.MethodGet, "/api/config", "", nil, &out)
	if err != nil {
		return Config{}, err
	}

	if out.APIKey == "" || out.ClientID == "" {
		return Config{}, fmt.Errorf("%w: /api/config carries no Bungie credentials", ErrService)
	}

	return out, nil
}

// ExchangeCode trades a Bungie OAuth authorization code for tokens.
func (s *Client) ExchangeCode(ctx context.Context, code string) (TokenResponse, error) {
	return s.tokens(ctx, "/api/auth/token", map[string]string{"code": code})
}

// RefreshTokens trades a Bungie refresh token for a fresh set of tokens.
func (s *Client) RefreshTokens(ctx context.Context, refreshToken string) (TokenResponse, error) {
	return s.tokens(ctx, "/api/auth/refresh", map[string]string{"refreshToken": refreshToken})
}

func (s *Client) tokens(ctx context.Context, path string, body any) (TokenResponse, error) {
	var out TokenResponse

	err := s.call(ctx, http.MethodPost, path, "", body, &out)
	if errors.Is(err, ErrService) && strings.Contains(err.Error(), grantRefusal) {
		return TokenResponse{}, fmt.Errorf("%w: %w", ErrGrantRejected, err)
	}

	if err != nil {
		return TokenResponse{}, err
	}

	if out.AccessToken == "" {
		return TokenResponse{}, fmt.Errorf("%w: %s returned no access token", ErrService, path)
	}

	return out, nil
}

// Artifact opens one definition artifact file. The caller closes the
// returned reader.
func (s *Client) Artifact(ctx context.Context, file string) (io.ReadCloser, error) {
	u := s.baseURL + "/artifacts/" + url.PathEscape(file)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Header.Set("User-Agent", s.userAgent)

	//nolint:bodyclose // The caller closes the returned reader.
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("performing request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		httpbody.Close(ctx, resp.Body, u)

		return nil, fmt.Errorf("%w: %s returned status %d", ErrService, u, resp.StatusCode)
	}

	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(resp.Body, maxArtifactBytes), resp.Body}, nil
}

// Tags returns every item tag row of the signed-in account, removed rows
// included.
func (s *Client) Tags(ctx context.Context, session string) ([]ItemTag, error) {
	var out struct {
		Items []ItemTag `json:"items"`
	}

	err := s.call(ctx, http.MethodGet, "/api/tags?since=0", session, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Items, nil
}

// WriteTags upserts item tag rows.
func (s *Client) WriteTags(ctx context.Context, session string, changes []ItemTag) error {
	body := map[string]any{"defs": []any{}, "items": changes}

	return s.call(ctx, http.MethodPost, "/api/tags", session, body, nil)
}

// Notes returns every note of the signed-in account, removed notes included.
func (s *Client) Notes(ctx context.Context, session string) ([]Note, error) {
	var out struct {
		Notes []Note `json:"notes"`
	}

	err := s.call(ctx, http.MethodGet, "/api/notes?since=0", session, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Notes, nil
}

// WriteNote upserts one note. An empty Body removes the note.
func (s *Client) WriteNote(ctx context.Context, session string, change Note) error {
	return s.call(ctx, http.MethodPost, "/api/notes", session, change, nil)
}

// PerkPreferences returns every perk preference of the signed-in account,
// removed preferences included.
func (s *Client) PerkPreferences(ctx context.Context, session string) ([]PerkPreference, error) {
	var out struct {
		Preferences []PerkPreference `json:"preferences"`
	}

	err := s.call(ctx, http.MethodGet, "/api/perk-preferences?since=0", session, nil, &out)
	if err != nil {
		return nil, err
	}

	return out.Preferences, nil
}

// WritePerkPreferences upserts perk preferences.
func (s *Client) WritePerkPreferences(ctx context.Context, session string, changes []PerkPreference) error {
	body := map[string]any{"preferences": changes}

	return s.call(ctx, http.MethodPost, "/api/perk-preferences", session, body, nil)
}

// Autotag returns every autotag rule of the signed-in account, removed rules
// included, and the autotag state.
func (s *Client) Autotag(ctx context.Context, session string) (AutotagSnapshot, error) {
	var out AutotagSnapshot

	err := s.call(ctx, http.MethodGet, "/api/autotag?since=0", session, nil, &out)
	if err != nil {
		return AutotagSnapshot{}, err
	}

	return out, nil
}

// WriteAutotagRules upserts autotag rules.
func (s *Client) WriteAutotagRules(ctx context.Context, session string, changes []AutotagRule) error {
	body := map[string]any{"rules": changes}

	return s.call(ctx, http.MethodPost, "/api/autotag", session, body, nil)
}

// SetAutotagEnabled turns autotagging on or off and reports whether the
// service applied the change. Turning it on stores mark as the highest
// instance id that the rules skip, unless autotagging is already on.
func (s *Client) SetAutotagEnabled(ctx context.Context, session string, enabled bool, mark string) (bool, error) {
	body := map[string]any{"kind": "disable"}
	if enabled {
		body = map[string]any{"kind": "enable", "mark": mark}
	}

	return s.changeAutotagState(ctx, session, body)
}

// ClaimAutotag moves the stored autotag mark to the value of the to
// parameter and reports whether the service applied the claim. The
// service applies a claim only while autotagging is on and the stored mark
// equals the value of the from parameter. At most one runner therefore
// claims the items between the two marks. The service refuses a claim
// whose to mark is not higher than its from mark.
func (s *Client) ClaimAutotag(ctx context.Context, session, from, to string) (bool, error) {
	return s.changeAutotagState(ctx, session, map[string]any{"kind": "claim", "from": from, "to": to})
}

// changeAutotagState posts one change of the autotag state and returns the
// applied field of the answer.
func (s *Client) changeAutotagState(ctx context.Context, session string, body map[string]any) (bool, error) {
	var out struct {
		Applied bool `json:"applied"`
	}

	err := s.call(ctx, http.MethodPost, "/api/autotag/state", session, body, &out)
	if err != nil {
		return false, err
	}

	return out.Applied, nil
}

// Stats reads the chunks of recorded profile changes that follow cursor. It
// reads the pages of /api/stats in order and stops after a page that
// reports no more chunks. A recorder that replaces a stored bucket moves it
// to a later page, so Stats keeps the last copy of each bucket that it
// reads. It returns the chunks in the order of StartsAt.
func (s *Client) Stats(ctx context.Context, session string, cursor StatCursor) ([]StatChunk, error) {
	byStart := map[int64]StatChunk{}

	for {
		page, err := s.statPage(ctx, session, cursor)
		if err != nil {
			return nil, err
		}

		for _, chunk := range page.Chunks {
			byStart[chunk.StartsAt] = chunk
		}

		if !page.More || len(page.Chunks) == 0 {
			break
		}

		last := page.Chunks[len(page.Chunks)-1]
		next := StatCursor{UpdatedAt: last.UpdatedAt, StartsAt: last.StartsAt}

		// A page that ends at or before its cursor repeats forever.
		if !cursor.before(next) {
			return nil, fmt.Errorf(
				"%w: /api/stats returned a page that ends at %d/%d, which does not follow the cursor %d/%d",
				ErrService, next.UpdatedAt, next.StartsAt, cursor.UpdatedAt, cursor.StartsAt,
			)
		}

		cursor = next
	}

	chunks := slices.Collect(maps.Values(byStart))
	slices.SortFunc(chunks, func(a, b StatChunk) int { return cmp.Compare(a.StartsAt, b.StartsAt) })

	return chunks, nil
}

// before reports whether c comes before other in the order of /api/stats.
func (c StatCursor) before(other StatCursor) bool {
	return c.UpdatedAt < other.UpdatedAt || (c.UpdatedAt == other.UpdatedAt && c.StartsAt < other.StartsAt)
}

// statPage reads the page of /api/stats that follows cursor.
func (s *Client) statPage(ctx context.Context, session string, cursor StatCursor) (statPageBody, error) {
	path := fmt.Sprintf("/api/stats?since=%d&after=%d", cursor.UpdatedAt, cursor.StartsAt)

	var page statPageBody

	err := s.stream(ctx, http.MethodGet, path, session, nil, func(r io.Reader) error {
		var err error

		page, err = decodeStatPage(io.LimitReader(r, maxStatPageBytes))
		if err != nil {
			return fmt.Errorf("%w: decoding %s: %w", ErrService, path, err)
		}

		return nil
	})

	return page, err
}

// decodeStatPage decodes one page of /api/stats a chunk at a time, so it
// holds one chunk body in memory at once.
func decodeStatPage(r io.Reader) (statPageBody, error) {
	dec := json.NewDecoder(r)

	err := expectDelim(dec, '{')
	if err != nil {
		return statPageBody{}, err
	}

	var page statPageBody

	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return statPageBody{}, fmt.Errorf("reading a key: %w", err)
		}

		switch key {
		case "chunks":
			page.Chunks, err = decodeStatChunks(dec)
		case "more":
			err = dec.Decode(&page.More)
		default:
			var skip json.RawMessage

			err = dec.Decode(&skip)
		}

		if err != nil {
			return statPageBody{}, fmt.Errorf("decoding %v: %w", key, err)
		}
	}

	err = expectDelim(dec, '}')
	if err != nil {
		return statPageBody{}, err
	}

	return page, nil
}

// decodeStatChunks decodes the array of chunks that dec reads next.
func decodeStatChunks(dec *json.Decoder) ([]StatChunk, error) {
	err := expectDelim(dec, '[')
	if err != nil {
		return nil, err
	}

	var chunks []StatChunk

	for dec.More() {
		var raw struct {
			Body      string `json:"body"`
			StartsAt  int64  `json:"startsAt"`
			EndsAt    int64  `json:"endsAt"`
			UpdatedAt int64  `json:"updatedAt"`
		}

		err := dec.Decode(&raw)
		if err != nil {
			return nil, fmt.Errorf("decoding a chunk: %w", err)
		}

		changes, err := decodeStatChanges(raw.Body)
		if err != nil {
			return nil, fmt.Errorf("decoding the body of the chunk at %d: %w", raw.StartsAt, err)
		}

		chunks = append(chunks, StatChunk{
			Changes:   changes,
			StartsAt:  raw.StartsAt,
			EndsAt:    raw.EndsAt,
			UpdatedAt: raw.UpdatedAt,
		})
	}

	err = expectDelim(dec, ']')
	if err != nil {
		return nil, err
	}

	return chunks, nil
}

// decodeStatChanges decodes a chunk body, which holds a JSON array of
// change records.
func decodeStatChanges(body string) ([]StatChange, error) {
	var changes []StatChange

	err := json.Unmarshal([]byte(body), &changes)
	if err != nil {
		return nil, fmt.Errorf("decoding the changes: %w", err)
	}

	// A JSON null decodes to a nil slice, and an empty array does not.
	if changes == nil {
		return nil, fmt.Errorf("the body %q is not a JSON array", body)
	}

	for i, change := range changes {
		if change.Kind == "" {
			return nil, fmt.Errorf("change %d of the body has no kind", i)
		}
	}

	return changes, nil
}

// expectDelim reads the next token of dec and returns an error when the
// token differs from want.
func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("reading %s: %w", want, err)
	}

	if tok != want {
		return fmt.Errorf("found %v where %s belongs", tok, want)
	}

	return nil
}

// call performs one JSON request. It sends a non-empty session as a bearer
// token. A nil dst discards the response body.
func (s *Client) call(ctx context.Context, method, path, session string, body, dst any) error {
	return s.stream(ctx, method, path, session, body, func(r io.Reader) error {
		raw, err := io.ReadAll(io.LimitReader(r, maxResponseBytes))
		if err != nil {
			return fmt.Errorf("reading %s: %w", s.baseURL+path, err)
		}

		if dst == nil {
			return nil
		}

		err = json.Unmarshal(raw, dst)
		if err != nil {
			return fmt.Errorf("%w: decoding %s: %w", ErrService, path, err)
		}

		return nil
	})
}

// stream performs one request and passes the body of a 2xx response to
// read. It sends a non-empty session as a bearer token and a non-nil body
// as JSON.
func (s *Client) stream(
	ctx context.Context,
	method, path, session string,
	body any,
	read func(io.Reader) error,
) error {
	u := s.baseURL + path

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

	req.Header.Set("User-Agent", s.userAgent)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if session != "" {
		req.Header.Set("Authorization", "Bearer "+session)
	}

	//nolint:bodyclose // The deferred httpbody.Close closes the body.
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}

	defer httpbody.Close(ctx, resp.Body, u)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return read(resp.Body)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("reading %s: %w", u, err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: %s %s: %s", ErrUnauthorized, method, path, serviceMessage(raw))
	}

	return fmt.Errorf(
		"%w: %s %s returned status %d: %s",
		ErrService, method, path, resp.StatusCode, serviceMessage(raw),
	)
}

// serviceMessage extracts the "error" field of a service error body and
// falls back to the start of the raw body.
func serviceMessage(raw []byte) string {
	var body struct {
		Error string `json:"error"`
	}

	err := json.Unmarshal(raw, &body)
	if err == nil && body.Error != "" {
		return body.Error
	}

	const limit = 200

	text := strings.TrimSpace(string(raw))
	if len(text) > limit {
		text = text[:limit]
	}

	return text
}
