package dvmtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
)

// pathAutotagState is the service path of the autotag state, which takes
// the claims of new items.
const pathAutotagState = "/api/autotag/state"

// Server is a fake service of Agg's Destiny Vault Manager. It records each
// request in the embedded [httpfake.Recorder] before it answers. The server
// reads the fields that pick its answers on every request, so a test sets
// them before the call under test.
type Server struct {
	*httpfake.Recorder

	// Data holds the GET response body of each service path.
	Data map[string]string
	// ArtifactFiles holds the file list of each artifact that the index
	// of the service lists. The service serves a listed file only when
	// Data holds a body for its path under /artifacts/.
	ArtifactFiles map[string][]string
	// URL is the base URL of the server.
	URL string
	// StaleSession is a token that the fake service rejects.
	StaleSession string
	// Claim answers each claim POST to /api/autotag/state. A zero Claim
	// answers with {"applied": true}.
	Claim Answer
	// ClaimedAutotag answers each GET of /api/autotag after a claim
	// POST. A zero ClaimedAutotag keeps the body of Data.
	ClaimedAutotag Answer
	// TagWriteStatus is the status of the answer to each POST to
	// /api/tags. Zero answers with 200.
	TagWriteStatus int
}

// Answer is the status and the body of one answer of a fake server.
type Answer struct {
	// Body is the JSON body of the answer.
	Body string
	// Status is the HTTP status of the answer. A zero Status leaves the
	// field that holds the Answer unset.
	Status int
}

// NewServer starts a [Server] with empty Data and ArtifactFiles. It closes
// the server when the test ends.
func NewServer(t *testing.T) *Server {
	t.Helper()

	s := &Server{
		Recorder:      &httpfake.Recorder{},
		Data:          map[string]string{},
		ArtifactFiles: map[string][]string{},
	}

	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := s.Record(t, r)

		switch {
		case req.Path == "/api/config":
			httpfake.WriteJSON(t, w, http.StatusOK, configBody(t, s.ArtifactFiles))

		case req.Path == "/api/auth/refresh" || req.Path == "/api/auth/token":
			httpfake.WriteJSON(t, w, http.StatusOK, `{"access_token": "access-2", "expires_in": 3600,
				"refresh_token": "refresh-2", "refresh_expires_in": 7776000, "membership_id": "42", "session": "session-2"}`)

		case s.StaleSession != "" && req.Auth == "Bearer "+s.StaleSession:
			httpfake.WriteJSON(t, w, http.StatusUnauthorized, `{"error": "Sign in required"}`)
		case req.Path == pathAutotagState && req.Body["kind"] == "claim" && s.Claim.Status != 0:
			httpfake.WriteJSON(t, w, s.Claim.Status, s.Claim.Body)
		case req.Method == http.MethodGet && req.Path == "/api/autotag" && s.ClaimedAutotag.Status != 0 &&
			len(s.Claims()) > 0:
			httpfake.WriteJSON(t, w, s.ClaimedAutotag.Status, s.ClaimedAutotag.Body)
		case req.Method == http.MethodPost && req.Path == "/api/tags" && s.TagWriteStatus != 0:
			httpfake.WriteJSON(t, w, s.TagWriteStatus, `{"error": "Tag store unavailable"}`)
		case req.Method == http.MethodGet && s.Data[req.Path] != "":
			httpfake.WriteJSON(t, w, http.StatusOK, s.Data[req.Path])
		case req.Path == pathAutotagState:
			httpfake.WriteJSON(t, w, http.StatusOK, `{"applied": true}`)
		case req.Method == http.MethodPost:
			httpfake.WriteJSON(t, w, http.StatusOK, `{"now": 11}`)
		default:
			httpfake.WriteJSON(t, w, http.StatusNotFound, `{"error": "Not found"}`)
		}
	}))
	t.Cleanup(service.Close)

	s.URL = service.URL

	return s
}

// Claims returns the claim POSTs that the fake service received.
func (s *Server) Claims() []httpfake.Request {
	var out []httpfake.Request

	for _, req := range s.Calls(http.MethodPost, pathAutotagState) {
		if req.Body["kind"] == "claim" {
			out = append(out, req)
		}
	}

	return out
}

// configBody returns the /api/config response body whose artifact index
// lists files.
func configBody(t *testing.T, files map[string][]string) string {
	t.Helper()

	raw, err := json.Marshal(files)
	require.NoError(t, err)

	return `{"apiKey": "key", "clientId": "54520", "artifacts": {"manifestVersion": "v1", "files": ` +
		string(raw) + `}}`
}
