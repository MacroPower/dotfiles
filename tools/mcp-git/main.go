package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

// stringSlice implements [flag.Value] for repeatable string
// flags.
type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprintf("%v", *s) }

func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	var allowDirs stringSlice

	flag.Var(
		&allowDirs, "allow-dir",
		"allowed destination directory (repeatable)",
	)

	allowInsecure := flag.Bool(
		"allow-insecure", false,
		"permit unencrypted URL schemes (http, git)",
	)

	flag.Parse()

	h := &cloneHandler{
		allowDirs:     allowDirs,
		allowInsecure: *allowInsecure,
	}

	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-git", Version: version},
		nil,
	)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "git_clone",
		Description: "Clone a git repository. Idempotent: clones if the destination does not exist, pulls (fast-forward only) if it does.",
	}, h.handle)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)

	err := srv.Run(ctx, &mcp.StdioTransport{})

	cancel()

	if err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
