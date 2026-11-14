package bungietest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
)

// Server is a fake Bungie platform API for the fixture account. It also
// serves the content files of the manifest. It records each request in the
// embedded [httpfake.Recorder] before it answers. The server reads the
// fields that pick its answers on every request, so a test sets them before
// the call under test.
type Server struct {
	*httpfake.Recorder

	// Data holds the GET response body of each Bungie path. The server
	// serves the platform and the content files of the manifest, so Data
	// also holds the body of each content file.
	Data map[string]string
	// ActionErrorIf limits ActionError to the actions whose request body
	// it selects, such as the transfers of one item. A nil ActionErrorIf
	// selects every action.
	ActionErrorIf func(body map[string]any) bool
	// OnAction runs for each Bungie action after the server records it
	// and before the server answers it. A nil OnAction does nothing.
	OnAction func()
	// URL is the base URL of the server.
	URL string
	// ActivitiesBody is the response body of the profile request that
	// asks only for the activities. When it is empty, that request gets
	// the body of the snapshot request: ProfileBody when it is set, and
	// [ProfileJSON] otherwise.
	ActivitiesBody string
	// ProfileBody is the response body of every other profile request.
	// When it is empty, those requests get [ProfileJSON].
	ProfileBody string
	// StaleAccess is an access token that the server rejects.
	StaleAccess string
	// ActionError is the body that the Bungie actions return. A set
	// ActionErrorPath limits it to the action at that path.
	ActionError     string
	ActionErrorPath string
}

// NewServer starts a [Server] whose Data holds the manifest, the loadout
// tables, the activity histories, and the vendors of the fixture account.
// It closes the server when the test ends.
func NewServer(t *testing.T) *Server {
	t.Helper()

	s := &Server{
		Recorder: &httpfake.Recorder{},
		Data: map[string]string{
			PathManifest:      ManifestJSON,
			PathLoadoutNames:  LoadoutNamesJSON,
			PathLoadoutIcons:  LoadoutIconsJSON,
			PathLoadoutColors: LoadoutColorsJSON,
			PathHunterHistory: HunterHistoryJSON,
			PathTitanHistory:  TitanHistoryJSON,
			PathHunterVendors: HunterVendorsJSON,
			PathTitanVendors:  TitanVendorsJSON,
		},
	}

	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := s.Record(t, r)

		if req.Method == http.MethodPost && s.OnAction != nil {
			s.OnAction()
		}

		switch {
		case s.StaleAccess != "" && req.Auth == "Bearer "+s.StaleAccess:
			httpfake.WriteJSON(t, w, http.StatusUnauthorized, "")
		case req.Path == "/User/GetMembershipsForCurrentUser/":
			httpfake.WriteJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": {"primaryMembershipId": "4611",
				"destinyMemberships": [{"membershipType": 2, "membershipId": "1"}, {"membershipType": 3, "membershipId": "4611"}]}}`)

		case req.Path == PathProfile && s.ActivitiesBody != "" &&
			strings.HasPrefix(req.Query, "components=204&"):
			httpfake.WriteJSON(t, w, http.StatusOK, s.ActivitiesBody)
		case req.Path == PathProfile && s.ProfileBody != "":
			httpfake.WriteJSON(t, w, http.StatusOK, s.ProfileBody)
		case req.Path == PathProfile:
			httpfake.WriteJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": `+ProfileJSON+`}`)
		case req.Method == http.MethodGet && s.Data[req.Path] != "":
			httpfake.WriteJSON(t, w, http.StatusOK, s.Data[req.Path])
		case req.Method == http.MethodPost && s.ActionError != "" &&
			(s.ActionErrorPath == "" || req.Path == s.ActionErrorPath) &&
			(s.ActionErrorIf == nil || s.ActionErrorIf(req.Body)):
			httpfake.WriteJSON(t, w, http.StatusOK, s.ActionError)
		case req.Method == http.MethodPost:
			httpfake.WriteJSON(t, w, http.StatusOK, `{"ErrorCode": 1, "Response": 0}`)
		default:
			httpfake.WriteJSON(
				t,
				w,
				http.StatusNotFound,
				`{"ErrorCode": 7, "ErrorStatus": "ParameterParseFailure", "Message": "no route"}`,
			)
		}
	}))
	t.Cleanup(platform.Close)

	s.URL = platform.URL

	return s
}
