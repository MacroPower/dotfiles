package kube_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
)

// TestClientsetCreateRetriedAfterStoredAttempt pins why
// [kube.IsRejected] counts AlreadyExists as not rejected. The fake
// apiserver stores the ServiceAccount of the first POST and answers
// it with the ServerTimeout and Retry-After header that the
// apiserver sends when its storage times out on a write. client-go
// then sends the POST again, and the second answer is AlreadyExists
// for the object that the first attempt stored.
func TestClientsetCreateRetriedAfterStoredAttempt(t *testing.T) {
	t.Parallel()

	var posts atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/namespaces/ns/serviceaccounts" {
			http.NotFound(w, r)

			return
		}

		if posts.Add(1) == 1 {
			// Retry-After 0 keeps the retry of client-go from waiting.
			w.Header().Set("Retry-After", "0")
			writeStatus(t, w, http.StatusInternalServerError,
				`{"kind":"Status","apiVersion":"v1","status":"Failure",`+
					`"reason":"ServerTimeout","code":500,"message":"try again later"}`)

			return
		}

		writeStatus(t, w, http.StatusConflict,
			`{"kind":"Status","apiVersion":"v1","status":"Failure",`+
				`"reason":"AlreadyExists","code":409,"message":"already exists",`+
				`"details":{"name":"claude-sa-0123abcd","kind":"serviceaccounts"}}`)
	}))
	t.Cleanup(server.Close)

	kubeconfigPath := filepath.Join(t.TempDir(), "config")
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: test
clusters:
- name: test
  cluster: {server: %q}
contexts:
- name: test
  context: {cluster: test, user: test}
users:
- name: test
  user: {token: test}
`, server.URL)
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600))

	client, err := kube.NewClientset(kubeconfigPath, "test")
	require.NoError(t, err)

	err = client.CreateServiceAccount(t.Context(), "ns", "claude-sa-0123abcd", nil)
	require.Error(t, err)

	assert.Equal(t, int32(2), posts.Load(), "client-go must send the create again after the ServerTimeout")
	assert.True(t, apierrors.IsAlreadyExists(err), "the error must hold the answer to the last attempt")
	assert.False(t, kube.IsRejected(err), "the first attempt stored the object, so the call is not a refusal")
}

// writeStatus answers a request of the fake apiserver with the JSON
// Status object body and the HTTP status code.
func writeStatus(t *testing.T, w http.ResponseWriter, code int, body string) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)

	_, err := io.WriteString(w, body)
	assert.NoError(t, err)
}
