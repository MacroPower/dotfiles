package kube_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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

	client, err := kube.NewClientset(writeKubeconfig(t, server.URL), "test")
	require.NoError(t, err)

	err = client.CreateServiceAccount(t.Context(), "ns", "claude-sa-0123abcd", nil)
	require.Error(t, err)

	assert.Equal(t, int32(2), posts.Load(), "client-go must send the create again after the ServerTimeout")
	assert.True(t, apierrors.IsAlreadyExists(err), "the error must hold the answer to the last attempt")
	assert.False(t, kube.IsRejected(err), "the first attempt stored the object, so the call is not a refusal")
}

// TestClientsetListCarriesCreationTime pins that each list call of
// [kube.Clientset] returns the creationTimestamp of each object. The
// orphan sweep keeps each resource that the apiserver created after
// the live set, so a list call that dropped the time would make the
// sweep keep every resource.
func TestClientsetListCarriesCreationTime(t *testing.T) {
	t.Parallel()

	const selector = "app.kubernetes.io/managed-by=mcp-kubectx"

	created := time.Date(2026, time.March, 4, 12, 30, 5, 0, time.UTC)

	tests := map[string]struct {
		list       func(*kube.Clientset) func(context.Context, string) ([]kube.ResourceRef, error)
		path       string
		apiVersion string
		kind       string
		namespace  string
	}{
		"service accounts": {
			list: func(c *kube.Clientset) func(context.Context, string) ([]kube.ResourceRef, error) {
				return c.ListServiceAccounts
			},
			path:       "/api/v1/serviceaccounts",
			apiVersion: "v1",
			kind:       "ServiceAccountList",
			namespace:  "ns",
		},
		"role bindings": {
			list: func(c *kube.Clientset) func(context.Context, string) ([]kube.ResourceRef, error) {
				return c.ListRoleBindings
			},
			path:       "/apis/rbac.authorization.k8s.io/v1/rolebindings",
			apiVersion: "rbac.authorization.k8s.io/v1",
			kind:       "RoleBindingList",
			namespace:  "ns",
		},
		"cluster role bindings": {
			list: func(c *kube.Clientset) func(context.Context, string) ([]kube.ResourceRef, error) {
				return c.ListClusterRoleBindings
			},
			path:       "/apis/rbac.authorization.k8s.io/v1/clusterrolebindings",
			apiVersion: "rbac.authorization.k8s.io/v1",
			kind:       "ClusterRoleBindingList",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.path ||
					r.URL.Query().Get("labelSelector") != selector {
					http.NotFound(w, r)

					return
				}

				namespace := ""
				if tc.namespace != "" {
					namespace = fmt.Sprintf(`"namespace":%q,`, tc.namespace)
				}

				writeStatus(t, w, http.StatusOK, fmt.Sprintf(
					`{"kind":%q,"apiVersion":%q,"metadata":{},"items":[{"metadata":`+
						`{"name":"claude-sa-0123abcd",%s"creationTimestamp":%q,"labels":{"a":"b"}}}]}`,
					tc.kind, tc.apiVersion, namespace, created.Format(time.RFC3339)))
			}))
			t.Cleanup(server.Close)

			client, err := kube.NewClientset(writeKubeconfig(t, server.URL), "test")
			require.NoError(t, err)

			got, err := tc.list(client)(t.Context(), selector)
			require.NoError(t, err)

			require.Len(t, got, 1)
			assert.True(t, created.Equal(got[0].Created), "got creation time %v", got[0].Created)
			assert.Equal(t, "claude-sa-0123abcd", got[0].Name)
			assert.Equal(t, tc.namespace, got[0].Namespace)
			assert.Equal(t, map[string]string{"a": "b"}, got[0].Labels)
		})
	}
}

// writeKubeconfig writes a kubeconfig whose context "test" reaches
// the apiserver at serverURL, and returns its path.
func writeKubeconfig(t *testing.T, serverURL string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config")
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
`, serverURL)
	require.NoError(t, os.WriteFile(path, []byte(kubeconfig), 0o600))

	return path
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
