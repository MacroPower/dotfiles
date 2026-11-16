package kube_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"go.jacobcolvin.com/dotfiles/tools/mcp-kubectx/kube"
)

// TestIsNotFound pins which errors count as an object that does not
// exist. The wrapped case matters because every method of
// [kube.Clientset] wraps the error of client-go before it returns.
func TestIsNotFound(t *testing.T) {
	t.Parallel()

	serviceAccounts := schema.GroupResource{Resource: "serviceaccounts"}

	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil": {},
		"not found": {
			err:  apierrors.NewNotFound(serviceAccounts, "claude-sa-1"),
			want: true,
		},
		"wrapped not found": {
			err:  fmt.Errorf("delete service account: %w", apierrors.NewNotFound(serviceAccounts, "claude-sa-1")),
			want: true,
		},
		"forbidden": {
			err: apierrors.NewForbidden(serviceAccounts, "claude-sa-1", errors.New("denied")),
		},
		"plain error with the same text": {
			err: errors.New("not found"),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, kube.IsNotFound(tc.err))
		})
	}
}

// TestIsRejected pins which errors prove that a create call stored
// nothing. Only an answer of the apiserver that refuses the call
// counts. Every error after which the apiserver might have stored
// the object counts as not rejected. That includes AlreadyExists
// and TooManyRequests. After some answers client-go sends a create
// again, and the error holds only the answer to the last attempt.
func TestIsRejected(t *testing.T) {
	t.Parallel()

	serviceAccounts := schema.GroupResource{Resource: "serviceaccounts"}

	tests := map[string]struct {
		err  error
		want bool
	}{
		"nil": {},
		"forbidden": {
			err:  apierrors.NewForbidden(serviceAccounts, "claude-sa-1", errors.New("denied")),
			want: true,
		},
		"invalid": {
			err:  apierrors.NewInvalid(schema.GroupKind{Kind: "ServiceAccount"}, "claude-sa-1", nil),
			want: true,
		},
		"unauthorized": {
			err:  apierrors.NewUnauthorized("expired token"),
			want: true,
		},
		"wrapped forbidden": {
			err: fmt.Errorf("create service account: %w",
				apierrors.NewForbidden(serviceAccounts, "claude-sa-1", errors.New("denied"))),
			want: true,
		},
		"already exists after a retry": {
			err: apierrors.NewAlreadyExists(serviceAccounts, "claude-sa-1"),
		},
		"wrapped already exists": {
			err: fmt.Errorf("create service account: %w", apierrors.NewAlreadyExists(serviceAccounts, "claude-sa-1")),
		},
		"too many requests after the last retry": {
			err: apierrors.NewTooManyRequests("throttled", 1),
		},
		"timeout": {
			err: apierrors.NewTimeoutError("request did not complete", 0),
		},
		"server timeout": {
			err: apierrors.NewServerTimeout(serviceAccounts, "create", 1),
		},
		"internal error": {
			err: apierrors.NewInternalError(errors.New("etcd leader changed")),
		},
		"service unavailable": {
			err: apierrors.NewServiceUnavailable("shutting down"),
		},
		"request timeout from a proxy": {
			err: apierrors.NewGenericServerResponse(
				http.StatusRequestTimeout, "POST", serviceAccounts, "claude-sa-1", "", 0, true,
			),
		},
		"transport error": {
			err: fmt.Errorf("create service account: %w", errors.New("read tcp: connection reset by peer")),
		},
		"canceled context": {
			err: fmt.Errorf("create service account: %w", context.Canceled),
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, kube.IsRejected(tc.err))
		})
	}
}
