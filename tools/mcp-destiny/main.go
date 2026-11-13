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
		Name: toolListCharacters,
		Description: "List the Destiny 2 characters with their ids, classes, power, item counts, " +
			"and the used and total slots of each bucket, such as kinetic weapons and the Postmaster. " +
			"The Postmaster slots count only the lost items and leave out the messages. " +
			"The result also gives the used and total slots of the vault and the account-wide buckets, " +
			"and the balance of each currency, such as glimmer.",
		Annotations: readOnly,
	}, h.handleListCharacters)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolListItems,
		Description: "List inventory items across the vault and every character, with filters and paging. " +
			"Returns weapons and armor unless kind says otherwise. Each row carries the instance id that the other tools take, " +
			"and each armor row carries the sum of its stats. The min_total filter selects armor with at least that stat total, " +
			"and sort=total puts the highest total first, for example to find the helmets with the highest stat total. " +
			"The stat input names one stat of armor or weapons, such as Health or Range, and adds a column with its value. " +
			"The min_stat filter selects items with at least that value of the stat, and sort=stat puts the highest value first.",
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
		Name: toolListActivityHistory,
		Description: "List the activities that the user played most recently, newest first, such as raids, strikes, " +
			"and Crucible matches. Each row gives the start time, the character, the activity name, the duration, " +
			"whether the run was completed, the kills, the deaths, and the instance id of the run. " +
			"Without character, it merges the runs of every character.",
		Annotations: readOnly,
	}, h.handleListActivityHistory)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolListVendors,
		Description: "List the vendors of one character, such as Banshee-44 or Xûr, and what each one sells " +
			"at the moment of the call. Bungie lists the vendors whose stock rotates and leaves out preview vendors " +
			"and kiosks. The stock and whether the character can buy each item differ per character. " +
			"Without character, it reads the most recently played character. " +
			"Without vendor, it lists one row per vendor with its name, subtitle, hash, number of sales, " +
			"and next refresh time. A disabled flag marks a vendor that Bungie reports as not accessible. " +
			"With vendor, it lists the sales of that vendor under their category names. " +
			"Each sale gives the item, the quantity when it is more than one, and the costs. " +
			"It also says whether the character can buy the item, with the reasons from the game when it cannot. " +
			"A sale that shows no cost is often a rank reward or a screen that the vendor opens, not a free item.",
		Annotations: readOnly,
	}, h.handleListVendors)

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
		Name: toolSetPerk,
		Description: "Make a perk active on an item instance in the live game, such as a weapon trait or a barrel. " +
			"The perk must be one that get_item lists as a swap for a socket of the item. " +
			"Bungie refuses a plug that costs materials to insert. It also allows the change only while the character " +
			"that holds the item is offline, in orbit, or in a social space. For an item in the vault, " +
			"that character is the most recently played one. " +
			"get_item and list_items show the stats of the item from before the change until the next inventory fetch, " +
			"up to 30 seconds later.",
		Annotations: additive,
	}, h.handleSetPerk)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolListLoadouts,
		Description: "List the loadouts that each character saved in the game, with the instance id, name, slot, " +
			"and location of each item. The server cannot read the name, icon, or color that the game shows " +
			"for a loadout, so the character and the loadout number identify it.",
		Annotations: readOnly,
	}, h.handleListLoadouts)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolEquipLoadout,
		Description: "Equip a loadout that a character saved in the game, by its character and its number " +
			"from list_loadouts. The game does not pull loadout items from another character, so the tool " +
			"first moves each loadout item that the character does not hold to that character. " +
			"It also pulls loadout items from the Postmaster. Before it moves an item that another character " +
			"has equipped, it equips a replacement that the other character holds for that slot, so the gear " +
			"of the other character changes. The tool changes nothing when a loadout item that another " +
			"character holds is not transferable, or when another character has it equipped and does not " +
			"hold a replacement for its slot. " +
			"A failed move stops the tool before it equips the loadout. " +
			"Bungie allows equipping only while the character is offline, in orbit, or in a social space.",
		Annotations: additive,
	}, h.handleEquipLoadout)

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

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolGetStats,
		Description: "Report how much of each currency and material the user gained and spent over a period, " +
			"and the kills of each weapon, split into player versus environment (PvE) and player versus player (PvP). " +
			"Gambit kills count as PvE. The numbers come from the profile changes that the web client of " +
			"Agg's Destiny Vault Manager records only while it is open in a browser. The web client posts the changes " +
			"of each 5-minute bucket after the bucket closes, so the latest minutes are missing. " +
			"list_characters reports the current currency balances, " +
			"and list_items with kind=other shows the current material stacks.",
		Annotations: readOnly,
	}, h.handleGetStats)

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
