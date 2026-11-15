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
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/auth"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/clarity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/damage"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/dvm"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/rolls"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/search"
)

const (
	version = "0.1.0"

	// userAgent is the User-Agent header of every request that the server
	// sends.
	userAgent = "MCP-Destiny/" + version
)

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
	fs.StringVar(&o.serviceURL, "service-url", dvm.DefaultURL, "base URL of Agg's Destiny Vault Manager")
	fs.StringVar(&o.stateDir, "state-dir", "", "directory of the token file (default $XDG_STATE_HOME/mcp-destiny)")
	fs.StringVar(
		&o.cacheDir,
		"cache-dir",
		"",
		"directory of the definition cache, the item name cache, and the Clarity cache "+
			"(default $XDG_CACHE_HOME/mcp-destiny)",
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

func (o options) auth(service *dvm.Client) *auth.Auth {
	return auth.New(auth.NewTokenStore(filepath.Join(o.stateDir, "tokens.json")), service)
}

// runLogin signs the user in from a terminal. It prints the Bungie
// authorization URL and reads the redirect URL from stdin.
func runLogin(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	o, err := parseFlags(subLogin, args)
	if err != nil {
		return err
	}

	service := dvm.New(dvm.WithURL(o.serviceURL), dvm.WithUserAgent(userAgent))
	signin := o.auth(service)

	u, err := signin.LoginURL(ctx)
	if err != nil {
		return err //nolint:wrapcheck // The sign-in error reaches the terminal unchanged.
	}

	_, err = fmt.Fprintf(stdout, loginPrompt, u)
	if err != nil {
		return fmt.Errorf("writing the prompt: %w", err)
	}

	line, err := readLine(ctx, stdin)
	if err != nil {
		return fmt.Errorf("reading the redirected URL: %w", err)
	}

	t, err := signin.CompleteLogin(ctx, line)
	if err != nil {
		return err //nolint:wrapcheck // The sign-in error reaches the terminal unchanged.
	}

	//nolint:gosmopolitan // The sign-in end is in the local time zone, as in the tools.
	done := "\n" + strings.Join(signedInSentences(t, time.Local), "\n") + "\n"

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

	err = o.auth(dvm.New(dvm.WithUserAgent(userAgent))).Logout()
	if err != nil {
		return err //nolint:wrapcheck // The sign-out error reaches the terminal unchanged.
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

	service := dvm.New(dvm.WithURL(o.serviceURL), dvm.WithUserAgent(userAgent))

	h := &handler{
		service: service,
		bungie:  bungie.New(bungie.WithUserAgent(userAgent)),
		auth:    o.auth(service),
		defs:    definitions.NewLoader(service, o.cacheDir, definitions.WithLogger(logger)),
		sheets:  rolls.NewSheetStore(service),
		log:     logger,
		now:     time.Now,
		zone:    time.Local, //nolint:gosmopolitan // The tools print times in the local time zone.
	}
	h.loadoutTables = bungie.NewLoadoutTableStore(bungieManifest{h: h})
	h.names = definitions.NewNameStore(bungieItems{h: h}, o.cacheDir, definitions.WithNameLogger(logger))
	h.clarity = clarity.NewStore(
		clarity.NewClient(clarity.WithUserAgent(userAgent)),
		o.cacheDir,
		clarity.WithLogger(logger),
	)

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

	// A loadout save replaces the loadout in its slot, and a clear deletes
	// it.
	destroys := true
	replacing := &mcp.ToolAnnotations{DestructiveHint: &destroys, IdempotentHint: true}

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
			"and each armor row carries the sum of its stats. " +
			"The power column is empty for an item without power, such as a vehicle or a subclass. " +
			"The min_total filter selects armor with at least that stat total, " +
			"and sort=total puts the highest total first, for example to find the helmets with the highest stat total. " +
			"The stat input names one stat of armor or weapons, such as Health or Range, and adds a column with its value. " +
			"The min_stat filter selects items with at least that value of the stat, and sort=stat puts the highest value first. " +
			"The include_ratings input adds the roll rating of each weapon copy for PvE, from the Aegis sheet, " +
			"and for PvP, from the PvP sheet. " +
			"Each rating cell gives the tier of the roll, from S (best) to F. " +
			"When the roll lost tiers, the cell also gives the tier of the weapon on the sheet in parentheses, such as 'A (sheet S)'. " +
			"A cell reads 'no tier' for a weapon that the sheet lists without a tier, " +
			"and 'roll unknown' with the sheet tier for a copy whose perks the profile does not list. " +
			"It reads 'not listed' for a weapon that the sheet does not list, " +
			"and 'unavailable' when the server has no sheet for the game mode, " +
			"or cannot read the perk preferences or the Destiny definitions. " +
			"A line before the table gives the reason for each unavailable game mode. " +
			"The rating cells of every item that is not a weapon are empty. " +
			"The min_pve_rating input takes a tier from S to F and keeps only the weapon copies " +
			"whose roll rates that tier or better for PvE, such as min_pve_rating=A for every roll at S or A. " +
			"The min_pvp_rating input does the same for PvP. " +
			"A copy must pass each minimum that the call sets, and a minimum adds no column. " +
			"A weapon that the sheet does not list, a weapon that the sheet lists without a tier, " +
			"and a copy whose rating reads 'roll unknown' never match a minimum. " +
			"Each minimum needs the ratings of its game mode. " +
			"When the mode has none, the call returns the reason as an error result, never an empty list. " +
			"get_item explains a rating column by column.",
		Annotations: readOnly,
	}, h.handleListItems)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolGetItem,
		Description: "Show one item instance in full: perks with their descriptions and the alternatives each socket can swap to, " +
			"stats, tag, note, and location. " +
			"Under each perk, the in-game text of Bungie comes first. " +
			"A line that starts with 'Clarity:' then opens the tested description of the Clarity database, " +
			"for each perk that Clarity covers. " +
			"That description gives the numbers that the in-game text leaves out, " +
			"such as the size and the duration of a damage bonus. " +
			"In it, a value in square brackets after 'PvP' applies only against players, " +
			"and '\u2191' precedes each value that the enhanced perk improves. " +
			"An enhanced trait, origin trait, or other weapon perk without a description of its own " +
			"shows the description of its base perk. " +
			"An enhanced weapon frame without one shows only the in-game text. " +
			"A line that starts with 'Might predate the final patch' names an update of Destiny 2 " +
			"that changed the perk after Clarity last updated the description, so its numbers might be out of date. " +
			"When the server cannot load the Clarity database, one line after the perk list gives the reason, " +
			"and the perks show only the in-game text. " +
			"A weapon also shows how the Aegis sheet (PvE) and the PvP sheet rate the roll of this copy. " +
			"Each sheet gives a weapon a tier from S (best) to F and picks the perks that it wants in each perk column. " +
			"The line of each sheet names the category that lists the weapon on the sheet, such as 'in HCs'. " +
			"When the sheet ranks the weapon, the line also gives the rank in that category, " +
			"such as 'rank 3 of 62 in HCs' for the third of 62 weapons. " +
			"The tier of the copy starts at the tier of the weapon on the sheet. " +
			"It moves down one step for each perk column that the copy misses, and it stops at F. " +
			"The copy hits a column with each pick of the column that it has. " +
			"The rating counts the perk preferences of the user for the game mode of the sheet. " +
			"A disliked perk never counts, also when the sheet picks it. " +
			"A liked perk counts as a pick for the perk column of its socket, " +
			"also when the copy rolled no pick of that column. " +
			"One line for each column names the perks that count, with '(liked)' after each liked perk, " +
			"or says that the column missed. " +
			"After the words 'The sheet picks', the line also names the perks that the sheet picks for the column. " +
			"The sheet lists its picks across every version of the weapon, " +
			"so a pick can be a perk that this version cannot roll. " +
			"The list leaves out the perks that the user dislikes for the game mode. " +
			"The note that the sheet gives the weapon follows the column lines, after the label 'Sheet note'. " +
			"The rating reads the perks that this tool lists, " +
			"and a pick that sits beside the active perk in a socket counts. " +
			"The web client reads another socket list, so its rating of a copy can differ. " +
			"A copy for which the profile lists no perks reads 'roll unknown'. " +
			"When the server has no sheet for a game mode, or cannot read the perk preferences or the Destiny definitions, " +
			"the line of that mode names the reason. " +
			"A weapon also shows its times and its damage falloff in real units, at the stats of this copy. " +
			"The block gives the reload time, the ready time, the stow time, and the time to aim down sights in seconds. " +
			"It also gives the distance in meters at which the damage falloff starts, " +
			"from the hip and while aiming down sights. " +
			"Bungie publishes each stat only as a number from 0 to 100. " +
			"The server turns the stats into seconds and meters with curves that players measured in the game " +
			"for each weapon type and frame, as D2Foundry's Oracle Engine collects them. " +
			"The values are therefore community measurements and not numbers from Bungie. " +
			"They are the base values of the weapon. " +
			"The Weapons stat of a character, armor mods, and perks with a timed bonus change them in the game. " +
			"Perks that scale a time or a distance without a stat bonus, such as Snapshot Sights, change them as well. " +
			"A value that the measurements do not give reads 'unknown' with the reason, " +
			"such as a frame without curves or a game update that changed the value after the last measurement. " +
			"A weapon also shows its time to kill a Guardian and its damage per second. " +
			"The numbers come from community measurements, " +
			"as D2Foundry's Oracle Engine and the Clarity database collect them, " +
			"and from the damage values that Bungie's patch notes state. " +
			"The Bungie API publishes none of them. " +
			"They match Update " + damage.Patch() + " of the game, the last update that changed one of them. " +
			"The time to kill runs from the first hit to the last one against a Guardian with 230 health. " +
			"One time counts precision hits and names the hits that can be body shots, " +
			"and one time counts body shots alone. " +
			"The damage per second is the damage of one magazine divided by the time of the magazine and one reload, " +
			"with precision hits and with body shots. " +
			"Every round hits, and the target stands closer than the damage falloff. " +
			"The numbers leave out the Weapons stat of a character, and they hold against a Guardian. " +
			"A combatant takes other damage, which the server does not model. " +
			"The first lines give the base numbers, without a timed perk. " +
			"The next lines give the numbers while one active perk is as strong as its line says, " +
			"such as 'With Rampage at 3 stacks', for each perk whose effect the server has. " +
			"A last line names each active perk whose effect the numbers leave out, " +
			"so a number never ignores a perk without that line. " +
			"The server has the damage of most legendary frames of auto rifles, hand cannons, pulse rifles, " +
			"scout rifles, sidearms, and submachine guns. " +
			"It does not have the damage of the Support Frame of auto rifles, the Spread Shot frame of hand cannons, " +
			"the Aggressive Burst frame of submachine guns, the Balanced Heat Weapon and Dynamic Heat Weapon frames, " +
			"or the Micro-Missile Frame of pulse rifles and sidearms. " +
			"For a weapon without these numbers, the block is one line that reads 'unknown' with the reason.",
		Annotations: readOnly,
	}, h.handleGetItem)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolCompareDuplicates,
		Description: "Compare the copies of one weapon side by side: perks with the alternatives each socket can swap to, " +
			"roll ratings, power, location, tag, and flags such as the favorite mark. " +
			"Pass a weapon name or the instance id of one copy. " +
			"Without either, it compares every weapon that has more than one copy, a page at a time. " +
			"Each row gives the roll rating of the copy for PvE and for PvP, in the cell texts that list_items describes. " +
			"The PvE rating comes from the Aegis sheet, and the PvP rating comes from the PvP sheet. " +
			"The result gives the reason for each game mode whose cells read 'unavailable', in one line for each mode. " +
			"Each group holds every copy of one weapon name, " +
			"and the copies of a reissue and of its Adept, Timelost, or Harrowed version count as copies of one weapon. " +
			"The server rates each copy by its own item hash. " +
			"Adept copies therefore read 'not listed' beside rated copies of the base weapon " +
			"when the sheet has no row for the Adept version. " +
			"Each row also gives the times and the damage falloff of the copy in real units, at the stats of the copy, " +
			"so two rolls compare in seconds and meters. " +
			"The reload, ready, stow, and aim columns hold the reload time, the ready time, the stow time, " +
			"and the time to aim down sights in seconds. " +
			"The hip falloff and aim falloff columns hold the distance in meters at which the damage falloff starts, " +
			"from the hip and while aiming down sights. " +
			"The values are the community measurements that get_item describes, and not numbers from Bungie. " +
			"They omit the effect of perks that scale a time or a distance without a stat bonus, such as Snapshot Sights. " +
			"A cell is empty when the measurements do not give the value. " +
			"A line after the title of the group then gives the reason. " +
			"The line names the columns that the reason empties, " +
			"or it opens with 'Times and falloff' when the reason empties all of them.",
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
		Description: "List the vendors of one character, such as Banshee-44 or X\u00fbr, and what each one sells " +
			"at the moment of the call. The stock and whether the character can buy each item differ per character. " +
			"Without character, it reads the most recently played character. " +
			"Without vendor, it lists one row per vendor with its name, subtitle, hash, number of sales, " +
			"and next refresh time. The list holds the vendors that Bungie groups for its Companion app and " +
			"that have a sale. No group lists the sub-vendors without a name that hold part of the Eververse " +
			"stock, so the list leaves them out, and the sales of Tess Everis do not include their stock. " +
			"With all, the list holds every vendor that Bungie returns, such as subclass screens, archives, " +
			"and vendors without a name, the Eververse sub-vendors among them. " +
			"A disabled flag marks a vendor that Bungie reports as not accessible. " +
			"With vendor, it lists the sales of that vendor under their category names, " +
			"and every vendor that Bungie returns can match. " +
			"Each sale gives the item, the quantity when it is more than one, and the costs. " +
			"It also says whether the character can buy the item, with the reasons from the game when it cannot. " +
			"A sale that shows no cost is often a rank reward or a screen that the vendor opens, not a free item.",
		Annotations: readOnly,
	}, h.handleListVendors)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolListActivities,
		Description: "List the activities that one character can play, such as raids, dungeons, and Crucible playlists, " +
			"with the rewards that each one still offers this week. The game drops a reward from the list after " +
			"the character claims it. Without character, it reads the most recently played character. " +
			"Each row gives the activity name, the activity type, the location, the recommended power, " +
			"the number of bonus drops, the focused reward, the bonus loot, a featured flag for an activity in the " +
			"featured carousel of the Portal, and the activity hash. The rows follow the reward ranking of the " +
			"Activities page of the web client, without its sections, its rule that puts Quickplay rows first, or its " +
			"power key: the most bonus drops first, then the activities with a focused reward, then those with " +
			"bonus loot, then the featured ones, then by name. Pass rewards=true to list only the activities that still " +
			"offer a reward. The list leaves out the activities that the game hides and the Story and Social activities, " +
			"and it lists each difficulty of an activity on its own row.",
		Annotations: readOnly,
	}, h.handleListActivities)

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
		Name: toolStageJunk,
		Description: "Stage items tagged junk in one weapon or armor slot of a character, so that the user " +
			"can dismantle them in the game. The Bungie API has no request that dismantles an item. " +
			"The tool fills the free places of the slot with junk from the vault and the other characters " +
			"in the live game, and it lists the unequipped junk that the slot then holds. " +
			"Call it again after the user dismantled them, until it reports that no junk is left. " +
			"It moves nothing when the slot holds an unequipped item that is not tagged junk and that " +
			"move_items can move, and it names those items so that move_items can move them to the vault first. " +
			"It stages junk beside an item that move_items cannot move, such as a stack without an instance id " +
			"or an item that is not transferable, and it names such items as items that the user keeps. " +
			"It moves nothing when such items leave no free place in a slot that holds no junk. " +
			"It skips junk that is equipped, at the Postmaster, not transferable, or for another class. " +
			"Without character, it stages on the most recently played character.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(bool)},
	}, h.handleStageJunk)

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
		Description: "List the loadouts that each character saved in the game. Each loadout carries the name " +
			"that the game shows for it, and each item shows its instance id, name, slot, and location. " +
			"The server reads the loadout names from the Bungie manifest. When it cannot read them, " +
			"the result gives the reason and lists the loadouts by number. " +
			"The server cannot show the icon or the color of a loadout, because the game defines both " +
			"only as images. Several loadouts can share a name, so the character and the loadout number " +
			"identify a loadout.",
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
		Name: toolSaveLoadout,
		Description: "Save the gear that a character has equipped into one of its loadout slots in the game, " +
			"by its character and the slot number, counted from 1 as in list_loadouts. " +
			"The Bungie API has no request that builds a loadout from a list of items, so creating a loadout " +
			"takes two steps. First equip the gear with move_items and equip=true, then call this tool. " +
			"The game saves the mods and the perks of the equipped gear with it. " +
			"A save into a slot that holds a loadout replaces that loadout. " +
			"name picks a name from the list that the game offers, in any letter case and with or without accents. " +
			"A digit also matches the letter that it stands for, such as 0 for o. " +
			"Without name, a saved loadout keeps its name, and an empty slot takes the first name of the list. " +
			"A saved loadout also keeps its icon and its color, and an empty slot takes the first icon and " +
			"the first color. The tool cannot pick an icon or a color, because the game defines both only as images. " +
			"The result lists the equipped items that the loadout holds and the name that it carries. " +
			"A slot number outside the slots of the character fails with the number of slots, and a name " +
			"that the game does not offer fails with the list of names. " +
			"Bungie can refuse the save, such as for a character in an activity, and the result then gives " +
			"the Bungie message.",
		Annotations: replacing,
	}, h.handleSaveLoadout)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolClearLoadout,
		Description: "Delete a loadout that a character saved in the game, by its character and its number " +
			"from list_loadouts. The slot of the loadout becomes empty, and save_loadout can fill it again. " +
			"The tool refuses a slot that holds no loadout. " +
			"Bungie can refuse the change, such as for a character in an activity, and the result then gives " +
			"the Bungie message.",
		Annotations: replacing,
	}, h.handleClearLoadout)

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
			"The roll ratings of the web client and of get_item count a liked trait as a pick of the roll sheet, " +
			"and they never count a disliked one.",
		Annotations: additive,
	}, h.handleSetPerkPreferences)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolListAutotagRules,
		Description: "List the autotag rules, which tag or vault newly acquired items that match a search, " +
			"and whether autotagging is on. " +
			"With matches=new, it reads a fresh inventory and shows which rule would take each item " +
			"that the next claim offers to the rules, without claiming. " +
			"With matches=owned, it counts the owned items that each rule matches " +
			"after the enabled rules before it took theirs. " +
			"The rule editor of the web client shows the same counts. " +
			"It names each rule whose answer the server cannot know.",
		Annotations: readOnly,
	}, h.handleListAutotagRules)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolSaveAutotagRule,
		Description: "Create an autotag rule, or update one by id. " +
			"A rule needs a tag_id or vault=true, and the tool refuses a rule that has neither. " +
			"The web client runs the rules, and it skips a rule whose query does not parse. " +
			"This tool refuses a query that the search parser of the web client rejects. " +
			"It also refuses a query that the web client runs differently from its text, " +
			"such as one with a ')' that closes no '(' or with an unquoted or, and, or not. " +
			"It also refuses a filter value that the web client rejects, such as count:=<2, " +
			"for the filters that preview_autotag_rule evaluates. " +
			"It saves a query with other filters and names them, because the web client might accept them. " +
			"When the call passes a query, the result counts the owned items that the query matches " +
			"at the moment of the call, as if no other rule existed. " +
			"It counts apart from the matches the items whose outcome the server cannot decide. " +
			"The count leaves out each item whose item set the EquipableItemSet definitions lack, " +
			"as preview_autotag_rule does, and a line after the count gives the number of such items. " +
			"For a query that reads a game mode without ratings, the result gives the reason of that mode " +
			"in place of the counts.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(bool)},
	}, h.handleSaveAutotagRule)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolPreviewAutotagRule,
		Description: "List the owned items that an autotag rule query matches, " +
			"so that the user can test a rule before saving it with save_autotag_rule. " +
			"The query uses the search syntax of the web client. " +
			"The tool evaluates it over the items that the rule editor of the web client previews, " +
			"which are the instanced weapons, armor, and other equipment in every location, the vault included, " +
			"except emotes and finishers. " +
			"The tool also leaves out each item whose item set the EquipableItemSet definitions lack, " +
			"because the web client builds no such item, and the last line of the result counts those items. " +
			"Each row has the columns of list_items. " +
			"The tool evaluates a closed set of filters, which are " + listWithAnd(search.SupportedKeywords()) + ". " +
			"Each is: word also works as not:<word>. " +
			"name:<text> matches an item with a word in its name that starts with the text, " +
			"and exactname:<text> matches an item whose whole name is the text. " +
			"Both ignore letter case and accents, and neither reads a digit as a letter, " +
			"so name:stomp does not match St0mp-EE5. " +
			"power:<range> and light:<range> compare the power that the web client counts with a number, " +
			"such as power:>=1900. The tool refuses a season word in the range, such as pinnaclecap. " +
			"rate:<range> tests the tier of the roll of a copy on the Aegis sheet, " +
			"and rate:>=b means B or better. " +
			"ratebase:<range> tests the tier of the row of the item on the sheet, whatever the copy rolled, " +
			"and misses:<range> tests the number of perk columns that the roll missed. " +
			"setbonus:<tier> matches armor with a set bonus that the sheet rates at that tier, such as setbonus:a, " +
			"and setbonus:2pc:<tier> and setbonus:4pc:<tier> also test the number of pieces of the bonus. " +
			"rate: and ratebase: also test the tier of each set bonus of the item. " +
			"verdict:<value> compares the perk columns that a copy hits with those of the other copies " +
			"on its sheet row, the vault included, and the value is better, same, worse, only, or mixed. " +
			"rating: and ratingbase: are other names of rate: and ratebase:, " +
			"and the forms with the prefix pvp read the PvP sheet. " +
			"The tool lists in a section of its own each item whose outcome the server cannot decide, " +
			"with the reason, and never counts such an item as a match. " +
			"For example, the profile lists no perks for some copies, " +
			"so the server leaves such a copy on a sheet row with perk columns undecided at rate: and misses:. " +
			"It also leaves a copy undecided at verdict: when the profile lists no perks for the copy " +
			"or for another copy on its sheet row. " +
			"A query that reads a game mode without ratings returns an error with the reason of that mode. " +
			"The tool refuses a query with any other term, such as a bare word or is:masterwork, " +
			"and the refusal names each such term together with the filters that the tool evaluates. " +
			"A term whose value the web client rejects, such as count:=<2, returns an error that names the term " +
			"and says that the web client skips a rule with the query. " +
			"The query can combine the filters with and, or, -, and parentheses. " +
			"The preview ignores rule order. An earlier rule can take a matching item first, " +
			"so a match does not mean that a saved rule takes the item.",
		Annotations: readOnly,
	}, h.handlePreviewAutotagRule)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        toolRemoveAutotagRule,
		Description: "Remove an autotag rule by id.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, h.handleRemoveAutotagRule)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolSetAutotagEnabled,
		Description: "Turn autotagging on or off. Turning it on skips every item held at that moment, " +
			"so the rules apply only to items acquired afterward. " +
			"run_autotag_rules runs the rules when a caller invokes it.",
		Annotations: additive,
	}, h.handleSetAutotagEnabled)

	// A claim cannot be undone, so DestructiveHint keeps its default of
	// true.
	mcp.AddTool(srv, &mcp.Tool{
		Name: toolRunAutotagRules,
		Description: "Run the autotag rules on the items acquired since the last claim, " +
			"as the web client does while it is open. " +
			"The tool first claims those items through the service, " +
			"so no other runner, such as the web client, applies the rules to them again. " +
			"A claim happens at most once. " +
			"For each rule with vault=true, the tool then moves each item that the web client moves to the vault. " +
			"It names each item that it leaves in place with the reason. " +
			"The tool claims nothing when autotagging is off " +
			"or when the server cannot decide which rule takes a new item. " +
			"It also claims nothing when an enabled rule is invalid, " +
			"uses a term that the server does not evaluate, or reads a game mode without ratings. " +
			"list_autotag_rules with matches=new shows what the tool would do.",
		Annotations: &mcp.ToolAnnotations{},
	}, h.handleRunAutotagRules)

	mcp.AddTool(srv, &mcp.Tool{
		Name: toolGetStats,
		Description: "Report how much of each currency and material the user gained and spent over a period, " +
			"and the kills of each weapon, split into player versus environment (PvE) and player versus player (PvP). " +
			"Gambit kills count as PvE. The numbers come from the profile changes that the web client of " +
			"Agg's Destiny Vault Manager records only while it is open in a browser. The web client posts the changes " +
			"of each 5-minute bucket after the bucket closes, so the latest minutes are missing. " +
			"A row whose item the server cannot name, from the definitions or from a lookup " +
			"in the live manifest of Bungie, shows the item hash, and a line after the tables gives the reason, " +
			"such as an item that Bungie classifies or a lookup that failed. " +
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
