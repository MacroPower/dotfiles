package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHeaders(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in      []string
		want    http.Header
		wantErr bool
	}{
		"single pair": {
			in:   []string{"Authorization=Bearer xyz"},
			want: http.Header{"Authorization": []string{"Bearer xyz"}},
		},
		"multiple pairs": {
			in: []string{"X-A=1", "X-B=2"},
			want: http.Header{
				"X-A": []string{"1"},
				"X-B": []string{"2"},
			},
		},
		"empty value is allowed": {
			in:   []string{"X-Empty="},
			want: http.Header{"X-Empty": []string{""}},
		},
		"missing equals rejected": {
			in:      []string{"NoEquals"},
			wantErr: true,
		},
		"empty key rejected": {
			in:      []string{"=value"},
			wantErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseHeaders(tc.in)
			if tc.wantErr {
				require.ErrorIs(t, err, errBadHeader)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestParseHeadersEnvExpansion verifies that header values expand $VAR via
// os.ExpandEnv, so secrets can be passed through environment.
func TestParseHeadersEnvExpansion(t *testing.T) {
	t.Setenv("PROXY_TEST_TOKEN", "sekret")

	got, err := parseHeaders([]string{"Authorization=Bearer $PROXY_TEST_TOKEN"})
	require.NoError(t, err)
	assert.Equal(t, http.Header{"Authorization": []string{"Bearer sekret"}}, got)
}

// TestProxyRoundTrip spins up an upstream MCP server behind StreamableHTTPHandler,
// runs the proxy against it, then drives a local mcp.Client over an in-memory
// transport to verify tools/list and tools/call round trip correctly.
func TestProxyRoundTrip(t *testing.T) {
	t.Parallel()

	var (
		initializePostSeen   atomic.Bool
		postAfterInitialize  atomic.Int32
		protoVersionAfterInit atomic.Value // string
	)

	upstream := mcp.NewServer(
		&mcp.Implementation{Name: "upstream", Version: "v0.0.1"},
		nil,
	)
	upstream.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "echo back the input",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "echo:" + args.Msg}},
		}, nil
	})

	httpHandler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return upstream },
		nil,
	)
	inspect := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
			isInit := bytes.Contains(body, []byte(`"method":"initialize"`))
			if isInit {
				initializePostSeen.Store(true)
			} else if initializePostSeen.Load() {
				postAfterInitialize.Add(1)
				if v := r.Header.Get("Mcp-Protocol-Version"); v != "" {
					protoVersionAfterInit.Store(v)
				}
			}
		}
		httpHandler.ServeHTTP(w, r)
	})
	ts := httptest.NewServer(inspect)
	t.Cleanup(ts.Close)

	localCli, localSrv := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	var (
		proxyErr error
		wg       sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		proxyErr = run(ctx, []string{"--url", ts.URL}, localSrv)
	}()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "v0.0.1"},
		&mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}},
	)
	cs, err := client.Connect(ctx, localCli, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	listRes, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Len(t, listRes.Tools, 1)
	assert.Equal(t, "echo", listRes.Tools[0].Name)

	callRes, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "echo",
		Arguments: map[string]any{"msg": "hi"},
	})
	require.NoError(t, err)
	require.Len(t, callRes.Content, 1)
	text, ok := callRes.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Equal(t, "echo:hi", text.Text)

	// Every POST after initialize must carry the negotiated protocol version
	// header. The SDK only stamps Mcp-Protocol-Version via the mcp.Client's
	// sessionUpdated hook, so a proxy that forwards at the raw transport
	// layer would silently drop it on subsequent POSTs.
	require.True(t, initializePostSeen.Load(), "initialize POST not observed")
	require.Greater(t, postAfterInitialize.Load(), int32(0), "no post-initialize POSTs observed")
	v, _ := protoVersionAfterInit.Load().(string)
	assert.NotEmpty(t, v, "Mcp-Protocol-Version header should be set on post-initialize POSTs")
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, v, "expected dated protocol version")

	_ = cs.Close()
	cancel()
	wg.Wait()

	if proxyErr != nil {
		assert.ErrorIs(t, proxyErr, context.Canceled)
	}
}

// TestLogFile verifies that --log-file produces newline-delimited JSON with a
// "method" field for each forwarded call.
func TestLogFile(t *testing.T) {
	t.Parallel()

	upstream := mcp.NewServer(
		&mcp.Implementation{Name: "upstream", Version: "v0.0.1"},
		nil,
	)
	upstream.AddTool(&mcp.Tool{
		Name:        "noop",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}, nil
	})

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return upstream },
		nil,
	)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	logPath := filepath.Join(t.TempDir(), "proxy.log")
	localCli, localSrv := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = run(ctx, []string{"--url", ts.URL, "--log-file", logPath}, localSrv)
	}()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "test-client", Version: "v0.0.1"},
		&mcp.ClientOptions{Capabilities: &mcp.ClientCapabilities{}},
	)
	cs, err := client.Connect(ctx, localCli, nil)
	require.NoError(t, err)

	_, err = cs.ListTools(ctx, nil)
	require.NoError(t, err)

	_ = cs.Close()
	cancel()
	wg.Wait()

	f, err := os.Open(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	var entries []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &entry))
		entries = append(entries, entry)
	}
	require.NoError(t, sc.Err())

	var toolsListEntry map[string]any
	for _, e := range entries {
		if m, _ := e["method"].(string); m == "tools/list" {
			toolsListEntry = e
			break
		}
	}
	require.NotNil(t, toolsListEntry, "expected a tools/list log entry")
	assert.Equal(t, "forward", toolsListEntry["msg"])
	assert.Contains(t, toolsListEntry, "duration_ms")
	assert.NotContains(t, toolsListEntry, "error", "successful call should not log error")
}

// TestRunRequiresURL ensures the CLI fails cleanly when --url is absent.
func TestRunRequiresURL(t *testing.T) {
	t.Parallel()

	_, localSrv := mcp.NewInMemoryTransports()
	err := run(context.Background(), nil, localSrv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--url")
}
