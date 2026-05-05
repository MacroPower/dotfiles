package main

import (
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// connectInMemory wires an [*mcp.Server] (with the static registry-info
// resource registered) to a freshly initialized [*mcp.ClientSession] over
// [mcp.NewInMemoryTransports]. Both sessions are closed via t.Cleanup so the
// test body only deals with the live client session.
func connectInMemory(t *testing.T) *mcp.ClientSession {
	t.Helper()

	srv := mcp.NewServer(&mcp.Implementation{Name: "mcp-opentofu", Version: "test"}, nil)
	addRegistryInfoResource(srv)

	cli := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)

	st, ct := mcp.NewInMemoryTransports()

	ctx := t.Context()

	ss, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		closeErr := ss.Close()
		if closeErr != nil {
			slog.DebugContext(ctx, "closing server session",
				slog.Any("error", closeErr),
			)
		}
	})

	cs, err := cli.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		closeErr := cs.Close()
		if closeErr != nil {
			slog.DebugContext(ctx, "closing client session",
				slog.Any("error", closeErr),
			)
		}
	})

	return cs
}

func TestRegistryInfoResourceListed(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.ListResources(t.Context(), nil)
	require.NoError(t, err)
	require.Len(t, res.Resources, 1)

	got := res.Resources[0]
	assert.Equal(t, registryInfoURI, got.URI)
	assert.Equal(t, registryInfoMIMEType, got.MIMEType)
	assert.Equal(t, registryInfoName, got.Name)
	assert.Equal(t, registryInfoDescription, got.Description)
}

func TestRegistryInfoResourceRead(t *testing.T) {
	t.Parallel()

	cs := connectInMemory(t)

	res, err := cs.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: registryInfoURI})
	require.NoError(t, err)
	require.Len(t, res.Contents, 1)

	got := res.Contents[0]
	assert.Equal(t, registryInfoURI, got.URI)
	assert.Equal(t, registryInfoMIMEType, got.MIMEType)
	assert.Equal(t, registryInfoText, got.Text)
}
