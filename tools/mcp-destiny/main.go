package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "0.1.0"

// Subcommand names.
const (
	subServe  = "serve"
	subLogin  = "login"
	subLogout = "logout"
)

// loginPrompt is the text that the login subcommand prints before it reads
// the redirected URL. Its one verb takes the authorization URL.
const loginPrompt = `Open this URL and approve access:

  %s

Bungie redirects to Agg's Destiny Vault Manager, which shows
"OAuth state mismatch". That message is expected. Copy the full URL from the
address bar.

Redirected URL: `

// ErrUnknownSubcommand reports a first argument that names no subcommand.
var ErrUnknownSubcommand = errors.New("unknown subcommand")

func main() {
	os.Exit(run())
}

// run is the body of main. It returns the exit code so that the deferred
// cancel runs before the process exits.
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err := dispatch(ctx, os.Args[1:], os.Stdin, os.Stdout)

	// The flag package already printed the usage.
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)

		return 1
	}

	return 0
}

// dispatch routes the arguments to a subcommand. The MCP server runs when
// the first argument is "serve", a flag, or absent.
func dispatch(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || (args[0] != "" && args[0][0] == '-') {
		return runServe(ctx, args)
	}

	switch args[0] {
	case subServe:
		return runServe(ctx, args[1:])
	case subLogin:
		return runLogin(ctx, args[1:], stdin, stdout)
	case subLogout:
		return runLogout(args[1:], stdout)
	default:
		return fmt.Errorf("%w: %s (want serve, login, or logout)", ErrUnknownSubcommand, args[0])
	}
}

// options holds the flags that every subcommand shares.
type options struct {
	serviceURL string
	stateDir   string
	cacheDir   string
	logFile    string
}

func parseFlags(name string, args []string) (options, error) {
	var o options

	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&o.serviceURL, "service-url", defaultServiceURL, "base URL of Agg's Destiny Vault Manager")
	fs.StringVar(&o.stateDir, "state-dir", "", "directory of the token file (default $XDG_STATE_HOME/mcp-destiny)")
	fs.StringVar(
		&o.cacheDir,
		"cache-dir",
		"",
		"directory of the definition cache (default $XDG_CACHE_HOME/mcp-destiny)",
	)
	fs.StringVar(&o.logFile, "log-file", "", "path to JSON log file (append)")

	err := fs.Parse(args)
	if err != nil {
		return options{}, fmt.Errorf("parsing flags: %w", err)
	}

	if o.stateDir == "" {
		o.stateDir, err = xdgDir("XDG_STATE_HOME", ".local/state")
		if err != nil {
			return options{}, err
		}
	}

	if o.cacheDir == "" {
		o.cacheDir, err = xdgDir("XDG_CACHE_HOME", ".cache")
		if err != nil {
			return options{}, err
		}
	}

	return o, nil
}

// xdgDir returns the mcp-destiny directory inside the XDG base directory
// that the environment variable env names. An unset variable selects
// fallback, a path relative to the home directory. Every platform follows
// the XDG layout, so the state directory and the cache directory share one
// convention.
func xdgDir(env, fallback string) (string, error) {
	dir := os.Getenv(env)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locating the home directory: %w", err)
		}

		dir = filepath.Join(home, fallback)
	}

	return filepath.Join(dir, "mcp-destiny"), nil
}

func (o options) auth(service *Service) *Auth {
	return NewAuth(NewTokenStore(filepath.Join(o.stateDir, "tokens.json")), service)
}

// runLogin signs the user in from a terminal. It prints the Bungie
// authorization URL and reads the redirect URL from stdin.
func runLogin(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	o, err := parseFlags(subLogin, args)
	if err != nil {
		return err
	}

	service := NewService(WithServiceURL(o.serviceURL))
	auth := o.auth(service)

	u, err := auth.LoginURL(ctx)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, loginPrompt, u)
	if err != nil {
		return fmt.Errorf("writing the prompt: %w", err)
	}

	line, err := readLine(ctx, stdin)
	if err != nil {
		return fmt.Errorf("reading the redirected URL: %w", err)
	}

	t, err := auth.CompleteLogin(ctx, line)
	if err != nil {
		return err
	}

	done := "\nSigned in as Bungie.net membership " + t.BungieID + ".\n"
	if !t.RefreshExpires.IsZero() {
		done += "The sign-in lasts until " + t.RefreshExpires.Format(timeLayout) + ".\n"
	}

	_, err = io.WriteString(stdout, done)
	if err != nil {
		return fmt.Errorf("writing the result: %w", err)
	}

	return nil
}

// readLine reads one line from r, and the end of the input also ends the
// line. It returns the error of ctx when ctx ends first, because a read
// from a terminal does not stop on an interrupt.
func readLine(ctx context.Context, r io.Reader) (string, error) {
	type result struct {
		err  error
		line string
	}

	done := make(chan result, 1)

	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		done <- result{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for input: %w", ctx.Err())

	case res := <-done:
		if res.err != nil && !errors.Is(res.err, io.EOF) {
			return "", res.err
		}

		return res.line, nil
	}
}

// runLogout deletes the saved tokens.
func runLogout(args []string, stdout io.Writer) error {
	o, err := parseFlags(subLogout, args)
	if err != nil {
		return err
	}

	err = o.auth(NewService()).Logout()
	if err != nil {
		return err
	}

	_, err = io.WriteString(stdout, "Signed out.\n")
	if err != nil {
		return fmt.Errorf("writing the result: %w", err)
	}

	return nil
}

// runServe runs the MCP server on stdio until ctx ends.
func runServe(ctx context.Context, args []string) error {
	o, err := parseFlags(subServe, args)
	if err != nil {
		return err
	}

	logger, closeLog, err := openLogger(o.logFile)
	if err != nil {
		return err
	}
	defer closeLog()

	service := NewService(WithServiceURL(o.serviceURL))

	h := &handler{
		service: service,
		bungie:  NewBungie(),
		auth:    o.auth(service),
		defs:    NewDefsLoader(service, o.cacheDir, WithDefsLogger(logger)),
		log:     logger,
		now:     time.Now,
	}

	err = newServer(h).Run(ctx, &mcp.StdioTransport{})
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	return nil
}

// newServer registers every tool of h on an MCP server.
func newServer(h *handler) *mcp.Server {
	srv := mcp.NewServer(
		&mcp.Implementation{Name: "mcp-destiny", Version: version},
		&mcp.ServerOptions{
			Instructions: "Reads and manages a Destiny 2 inventory through Agg's Destiny Vault Manager. " +
				"Every tool except status and login needs a sign-in. When a tool reports that the user is " +
				"not signed in, call login and relay its steps. Find instance ids with list_items before " +
				"calling a tool that takes them.",
		},
	)

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	additive := &mcp.ToolAnnotations{DestructiveHint: new(bool), IdempotentHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolStatus,
		Description: "Report whether the user is signed in to Bungie and when the sign-in expires.",
		Annotations: readOnly,
	}, h.handleStatus)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolLogin,
		Description: "Start a Bungie sign-in. Returns a URL for the user to open and the steps to relay.",
		Annotations: readOnly,
	}, h.handleLogin)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolCompleteLogin,
		Description: "Finish the sign-in that login started, using the URL that Bungie redirected the user to.",
		Annotations: additive,
	}, h.handleCompleteLogin)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolListCharacters,
		Description: "List the Destiny 2 characters with their ids, classes, power, and item counts, plus the vault count.",
		Annotations: readOnly,
	}, h.handleListCharacters)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolListItems,
		Description: "List inventory items across the vault and every character, with filters and paging. " +
			"Returns weapons and armor unless kind says otherwise. Each row carries the instance id that the other tools take.",
		Annotations: readOnly,
	}, h.handleListItems)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolGetItem,
		Description: "Show one item instance in full: perks with their descriptions and the alternatives each socket can swap to, " +
			"stats, tag, note, and location.",
		Annotations: readOnly,
	}, h.handleGetItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolCompareDuplicates,
		Description: "Compare the copies of one weapon side by side: perks with the alternatives each socket can swap to, " +
			"power, location, tag, and flags such as the favorite mark. Pass a weapon name or the instance id of one copy. " +
			"Without either, it compares every weapon that has more than one copy, a page at a time. " +
			"The copies of a reissue and of its Adept, Timelost, or Harrowed version count as copies of one weapon.",
		Annotations: readOnly,
	}, h.handleCompareDuplicates)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolMoveItems,
		Description: "Move item instances to the vault or to a character in the live game, and optionally equip them. " +
			"Pulls items from the Postmaster first. Before it transfers an equipped item, it equips another item " +
			"that the character holds for that slot. " +
			"Bungie allows equipping only while the character is offline, in orbit, or in a social space.",
		Annotations: additive,
	}, h.handleMoveItems)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolMoveStack,
		Description: "Move units of a stackable item, such as a material or a consumable, between the vault " +
			"and the inventory in the live game. list_items with kind=other shows each stack with its item hash.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(bool)},
	}, h.handleMoveStack)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolSetLock,
		Description: "Lock or unlock item instances in the live game. The game does not let the player " +
			"dismantle a locked item. list_items and get_item show the lock as the locked flag.",
		Annotations: additive,
	}, h.handleSetLock)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolListTags,
		Description: "List the tag ids of Agg's Destiny Vault Manager with the number of held items that carry each one.",
		Annotations: readOnly,
	}, h.handleListTags)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolSetTag,
		Description: "Set or clear the tag of item instances. An item carries one tag, so a new tag replaces the previous one.",
		Annotations: additive,
	}, h.handleSetTag)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolSetFavorite,
		Description: "Mark item instances as favorites or clear the mark. The favorite mark is independent of the tag.",
		Annotations: additive,
	}, h.handleSetFavorite)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolListNotes,
		Description: "List the notes saved on item instances, Collections entries, and Collections armor sets.",
		Annotations: readOnly,
	}, h.handleListNotes)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolSetNote,
		Description: "Save, replace, or remove the note on an item instance, a Collections entry, or a Collections armor set.",
		Annotations: additive,
	}, h.handleSetNote)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolListPerkPreferences,
		Description: "List the perks the user marked as liked (up) or disliked (down) for PvE and PvP.",
		Annotations: readOnly,
	}, h.handleListPerkPreferences)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolSetPerkPreferences,
		Description: "Mark weapon traits as liked or disliked for PvE or PvP, or clear a preference. " +
			"The roll ratings of the web client count a liked trait as wanted and drop a disliked one.",
		Annotations: additive,
	}, h.handleSetPerkPreferences)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolListAutotagRules,
		Description: "List the autotag rules, which tag or vault newly acquired items that match a search, and whether autotagging is on.",
		Annotations: readOnly,
	}, h.handleListAutotagRules)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolSaveAutotagRule,
		Description: "Create an autotag rule, or update one by id. The web client runs the rules. " +
			"It skips a rule whose query does not parse, and this tool cannot check the query syntax.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(bool)},
	}, h.handleSaveAutotagRule)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolRemoveAutotagRule,
		Description: "Remove an autotag rule by id.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, h.handleRemoveAutotagRule)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolSetAutotagEnabled,
		Description: "Turn autotagging on or off. Turning it on skips every item held at that moment, " +
			"so the rules apply only to items acquired afterward.",
		Annotations: additive,
	}, h.handleSetAutotagEnabled)

	return srv
}

// openLogger creates a JSON [*slog.Logger] that appends to the named file.
// An empty path returns a logger that discards every entry.
func openLogger(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}

	// The log file can share the directory of the token file, so only the
	// owner can enter a directory that this call creates.
	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		return nil, nil, fmt.Errorf("creating log directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening %s: %w", path, err)
	}

	logger := slog.New(slog.NewJSONHandler(f, nil))

	return logger, func() {
		err := f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "closing log file: %v\n", err)
		}
	}, nil
}
