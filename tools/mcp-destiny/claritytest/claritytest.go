package claritytest

import (
	"cmp"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
)

// Paths of the two files that the Clarity site serves.
const (
	// PathVersions is the path of the versions file.
	PathVersions = "/versions.json"
	// PathDescriptions is the path of the descriptions file.
	PathDescriptions = "/descriptions/clarity.json"
)

// FixtureVersion is the version of the descriptions file that a new
// [Server] names.
const FixtureVersion = "1.5"

// PlugExplosivePayload is the item hash of the plug that the fixture
// database describes. Package definitionstest names the plug Explosive
// Payload, and the vault hand cannon of the Bungie fixture profile holds
// it.
const PlugExplosivePayload uint32 = 900

// Perk is one entry of a descriptions file that [Descriptions] builds.
// Item is the name of the exotic item that owns the perk, and it is empty
// for a perk that many items can hold. Type is the kind of perk that
// Clarity gives the entry, such as Weapon Trait.
type Perk struct {
	Uploaded time.Time
	Name     string
	Item     string
	Type     string
	Lines    []string
	Hash     uint32
}

// Fixture returns the entries of the fixture database. It describes the
// plug [PlugExplosivePayload] in two lines. Clarity uploaded the entry
// after the last patch that changed a perk.
func Fixture() []Perk {
	return []Perk{{
		Hash:     PlugExplosivePayload,
		Name:     "Explosive Payload",
		Type:     "Weapon Trait",
		Uploaded: time.Date(2026, time.July, 9, 12, 0, 0, 0, time.UTC),
		Lines: []string{
			"Each bullet deals half of its damage as an explosion.",
			"\U00002022 The explosion ignores damage falloff.",
		},
	}}
}

// Descriptions returns the content of a descriptions file that holds
// perks. Each line of a perk becomes one line with one run of text.
func Descriptions(t *testing.T, perks ...Perk) string {
	t.Helper()

	type content struct {
		Text string `json:"text"`
	}

	type line struct {
		LinesContent []content `json:"linesContent"`
	}

	type entry struct {
		Descriptions map[string][]line `json:"descriptions"`
		Name         string            `json:"name"`
		ItemName     string            `json:"itemName,omitempty"`
		Type         string            `json:"type,omitempty"`
		LastUpload   int64             `json:"lastUpload"`
		Hash         uint32            `json:"hash"`
	}

	file := make(map[string]entry, len(perks))

	for _, perk := range perks {
		lines := make([]line, 0, len(perk.Lines))
		for _, text := range perk.Lines {
			lines = append(lines, line{LinesContent: []content{{Text: text}}})
		}

		file[strconv.FormatUint(uint64(perk.Hash), 10)] = entry{
			Hash:         perk.Hash,
			Name:         perk.Name,
			ItemName:     perk.Item,
			Type:         perk.Type,
			LastUpload:   perk.Uploaded.UnixMilli(),
			Descriptions: map[string][]line{"en": lines},
		}
	}

	raw, err := json.Marshal(file)
	require.NoError(t, err)

	return string(raw)
}

// Server is a fake of the site that publishes the Clarity database. It
// records each request in the embedded [httpfake.Recorder] before it
// answers. The server reads the fields that pick its answers on every
// request, so a test sets them before the call under test.
type Server struct {
	*httpfake.Recorder

	// URL is the base URL of the server.
	URL string
	// Version is the version of the descriptions file that the versions
	// file names. The server writes it as a JSON number.
	Version string
	// Descriptions is the content of the descriptions file.
	Descriptions string
	// Status is the status of every answer. Zero answers with 200.
	Status int
}

// NewServer starts a [Server] that names [FixtureVersion] and serves the
// [Fixture] database. It closes the server when the test ends.
func NewServer(t *testing.T) *Server {
	t.Helper()

	s := &Server{
		Recorder:     &httpfake.Recorder{},
		Version:      FixtureVersion,
		Descriptions: Descriptions(t, Fixture()...),
	}

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := s.Record(t, r)

		status := cmp.Or(s.Status, http.StatusOK)

		switch {
		case status != http.StatusOK:
			httpfake.WriteJSON(t, w, status, `{"error": "unavailable"}`)
		case req.Path == PathVersions:
			httpfake.WriteJSON(t, w, http.StatusOK, `{"descriptions": `+s.Version+`}`)
		case req.Path == PathDescriptions:
			httpfake.WriteJSON(t, w, http.StatusOK, s.Descriptions)
		default:
			httpfake.WriteJSON(t, w, http.StatusNotFound, `{"error": "Not found"}`)
		}
	}))
	t.Cleanup(site.Close)

	s.URL = site.URL

	return s
}

// Downloads counts the requests for the descriptions file that the
// server received.
func (s *Server) Downloads() int {
	return len(s.Calls(http.MethodGet, PathDescriptions))
}

// Checks counts the requests for the versions file that the server
// received.
func (s *Server) Checks() int {
	return len(s.Calls(http.MethodGet, PathVersions))
}
