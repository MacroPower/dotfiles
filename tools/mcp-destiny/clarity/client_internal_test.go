package clarity

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/httpfake"
)

const testUserAgent = "MCP-Destiny/test"

func TestClientVersion(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		body string
		want string
		// errMsg is the text of an error without a sentinel. "{url}"
		// stands for the base URL of the site.
		errMsg string
		status int
	}{
		"a version": {
			status: http.StatusOK,
			body:   `{"descriptions": 2.0627}`,
			want:   "2.0627",
		},
		"a version keeps its digits": {
			status: http.StatusOK,
			body:   `{"descriptions": 2.10, "other": 7}`,
			want:   "2.10",
		},
		"a file without the descriptions version": {
			status: http.StatusOK,
			body:   `{"other": 7}`,
			err:    errNoVersion,
		},
		"a failure status": {
			status: http.StatusServiceUnavailable,
			body:   `{"error": "unavailable"}`,
			errMsg: "{url}/versions.json returned status 503",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			u, rec := httpfake.Serve(t, testUserAgent, tc.status, tc.body)
			c := NewClient(WithURL(u+"/"), WithUserAgent(testUserAgent))

			got, err := c.Version(t.Context())

			switch {
			case tc.err != nil:
				require.ErrorIs(t, err, tc.err)
			case tc.errMsg != "":
				require.EqualError(t, err, strings.ReplaceAll(tc.errMsg, "{url}", u))
			default:
				require.NoError(t, err)
			}

			assert.Equal(t, tc.want, got)
			assert.Len(t, rec.Calls(http.MethodGet, pathVersions), 1)
		})
	}
}

func TestClientVersionRejectsOtherContent(t *testing.T) {
	t.Parallel()

	u, _ := httpfake.Serve(t, testUserAgent, http.StatusOK, `<html>Not found</html>`)

	_, err := NewClient(WithURL(u), WithUserAgent(testUserAgent)).Version(t.Context())

	var syntax *json.SyntaxError

	require.ErrorAs(t, err, &syntax)
	require.ErrorContains(t, err, "decoding "+u+"/versions.json")
}

func TestClientDescriptions(t *testing.T) {
	t.Parallel()

	const body = `{"900": {"name": "Kill Clip"}}`

	u, rec := httpfake.Serve(t, testUserAgent, http.StatusOK, body)

	got, err := NewClient(WithURL(u), WithUserAgent(testUserAgent)).Descriptions(t.Context())
	require.NoError(t, err)
	assert.JSONEq(t, body, string(got))
	assert.Len(t, rec.Calls(http.MethodGet, pathDescriptions), 1)

	u, _ = httpfake.Serve(t, testUserAgent, http.StatusNotFound, `{}`)

	_, err = NewClient(WithURL(u), WithUserAgent(testUserAgent)).Descriptions(t.Context())
	require.EqualError(t, err, u+"/descriptions/clarity.json returned status 404")
}

func TestClientRequestFailure(t *testing.T) {
	t.Parallel()

	u, _ := httpfake.Serve(t, testUserAgent, http.StatusOK, `{}`)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := NewClient(WithURL(u), WithUserAgent(testUserAgent)).Version(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "performing request")
}

// TestClientCapsBody pins that the client rejects a file that is larger
// than its cap and still closes the body.
func TestClientCapsBody(t *testing.T) {
	t.Parallel()

	raw := &httpfake.EndlessBody{}

	c := NewClient(WithURL("http://clarity.invalid"))
	c.http = &http.Client{Transport: httpfake.BodyTransport{Body: raw}}

	_, err := c.Version(t.Context())
	require.ErrorIs(t, err, errTooLarge)
	assert.True(t, raw.Closed)
}
