package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Sentinel errors for shell-out diagnostics. Wrapping these makes
// unit tests robust to platform-specific exec error text.
var (
	ErrHostExec    = errors.New("host subcommand")
	ErrHostExecRun = errors.New("invoke host subcommand")
)

// guestEnvVar is the environment variable workmux sets inside its
// sandbox guest. handler.isGuest reads this through envLookup.
const guestEnvVar = "WM_SANDBOX_GUEST"

// hostExecArgs returns the (command, argv) pair to run a `host *`
// subcommand. On the macOS host the binary is invoked at its own
// absolute path. Inside a Lima guest it goes through `workmux
// host-exec` as [hostCommand], which the workmux host_commands
// allowlist names. workmux finds [hostCommand] on the PATH of the
// host and passes it the subcommand and its flags. [dispatch] runs
// only the `host` subcommands under that name.
//
// Reads h.envLookup so tests can flip the guest bit without mutating
// process env. Only handler-bound code paths use this helper. The
// host * subcommand entry points have no handler instance, so they
// cannot recurse.
func (h *handler) hostExecArgs(sub string, args []string) (string, []string, error) {
	if h.isGuest() {
		argv := append([]string{"host-exec", hostCommand, sub}, args...)
		return "workmux", argv, nil
	}

	full := append([]string{subHost, sub}, args...)

	self, err := os.Executable()
	if err != nil {
		return "", nil, fmt.Errorf("resolve executable path: %w", err)
	}

	return self, full, nil
}

// isGuest reports whether the serve process is running inside a
// workmux sandbox guest. The decision uses the injected envLookup
// (assigned at every construction site: [main.runServe] uses
// [os.Getenv], tests use constLookup) so unit tests can drive both
// branches deterministically.
func (h *handler) isGuest() bool {
	return h.envLookup(guestEnvVar) == "1"
}

// runHostFunc is the contract used by handler.list and
// handler.selectCtx to shell out. It is a field on handler so
// tests can substitute a fake that records calls and returns
// canned stdout/stderr without touching the filesystem.
type runHostFunc func(ctx context.Context, sub string, args []string) (stdout []byte, err error)

// hostSubcommandKey is the attribute that [relayHostLog] adds to each
// record that it relays. Its value names the `host` subcommand that
// wrote the record.
const hostSubcommandKey = "subcommand"

// defaultRunHost forks the mcp-kubectx binary (or `workmux
// host-exec mcp-kubectx-host ...` when in a guest) through
// [runHostCommand]. runHostCommand adds the log records of the
// subcommand to the default logger of serve.
func (h *handler) defaultRunHost(ctx context.Context, sub string, args []string) ([]byte, error) {
	cmd, argv, err := h.hostExecArgs(sub, args)
	if err != nil {
		return nil, err
	}

	return runHostCommand(ctx, slog.Default(), sub, cmd, argv)
}

// runHostCommand runs cmd with argv as the `host` subcommand sub and
// returns its stdout.
//
// [dispatchHost] makes the `host` subcommands write their logs to
// stderr as JSON lines. runHostCommand passes each record on
// stderr to logger through [relayHostLog], whatever the exit status.
// So the log of serve holds each warning of a subcommand that exits
// 0, such as a failed delete of `host release`.
//
// Other text on stderr, such as the error that [run] prints before a
// non-zero exit, goes into the returned error. After exit 0,
// runHostCommand logs that text at warn level.
func runHostCommand(ctx context.Context, logger *slog.Logger, sub, cmd string, argv []string) ([]byte, error) {
	// cmd is either os.Executable() or the literal "workmux"; argv
	// is built from controlled inputs, not user-supplied strings.
	c := exec.CommandContext(ctx, cmd, argv...) //nolint:gosec // see comment

	var stdout, stderr bytes.Buffer

	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()

	text := relayHostLog(ctx, logger, sub, stderr.Bytes())

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), fmt.Errorf(
				"%w %q: exit %d: %s",
				ErrHostExec, sub, exitErr.ExitCode(), text,
			)
		}

		return stdout.Bytes(), fmt.Errorf("%w %q: %w", ErrHostExecRun, sub, err)
	}

	if text != "" {
		logger.WarnContext(ctx, "host subcommand wrote to stderr",
			slog.String(hostSubcommandKey, sub),
			slog.String("stderr", text),
		)
	}

	return stdout.Bytes(), nil
}

// relayHostLog passes each log record in stderr, the output of the
// `host` subcommand sub, to logger. Each relayed record keeps the
// time, the level, the message, and the attributes that the
// subcommand gave it, and it gains [hostSubcommandKey]. logger drops
// each record below its level.
//
// relayHostLog returns the lines of stderr that hold no record,
// joined and trimmed. [parseHostLogRecord] decides which lines hold
// a record.
func relayHostLog(ctx context.Context, logger *slog.Logger, sub string, stderr []byte) string {
	handler := logger.Handler()

	var text []string

	for line := range bytes.Lines(stderr) {
		r, ok := parseHostLogRecord(line, sub)
		if !ok {
			text = append(text, string(line))
			continue
		}

		if !handler.Enabled(ctx, r.Level) {
			continue
		}

		// A record that the log cannot take has no other place to
		// go. [slog.Logger] drops the error of its handler too.
		_ = handler.Handle(ctx, r) //nolint:errcheck // see comment
	}

	return strings.TrimSpace(strings.Join(text, ""))
}

// parseHostLogRecord reads line as one record that
// [slog.JSONHandler] wrote in the `host` subcommand sub. The line
// holds a record when it is one JSON object with a string level that
// [slog.Level.UnmarshalText] reads and a string message.
//
// The record takes its time from the time key of the object. When
// that key is missing or does not parse, it takes the time of the
// call. Its first attribute is [hostSubcommandKey] with the value
// sub. Every other key of the object becomes an attribute after it,
// in the order of the object.
func parseHostLogRecord(line []byte, sub string) (slog.Record, bool) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return slog.Record{}, false
	}

	var (
		when      time.Time
		level     slog.Level
		msg       string
		haveLevel bool
		haveMsg   bool
	)

	attrs := []slog.Attr{slog.String(hostSubcommandKey, sub)}

	for dec.More() {
		tok, err = dec.Token()
		if err != nil {
			return slog.Record{}, false
		}

		key, ok := tok.(string)
		if !ok {
			return slog.Record{}, false
		}

		var val any

		err = dec.Decode(&val)
		if err != nil {
			return slog.Record{}, false
		}

		switch key {
		case slog.TimeKey:
			s, isString := val.(string)
			if isString {
				t, parseErr := time.Parse(time.RFC3339Nano, s)
				if parseErr == nil {
					when = t
				}
			}

		case slog.LevelKey:
			s, isString := val.(string)
			haveLevel = isString && level.UnmarshalText([]byte(s)) == nil

		case slog.MessageKey:
			msg, haveMsg = val.(string)

		default:
			attrs = append(attrs, hostLogAttr(key, val))
		}
	}

	// The closing brace, then nothing but the end of the line.
	_, err = dec.Token()
	if err != nil {
		return slog.Record{}, false
	}

	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		return slog.Record{}, false
	}

	if !haveLevel || !haveMsg {
		return slog.Record{}, false
	}

	if when.IsZero() {
		when = time.Now()
	}

	r := slog.NewRecord(when, level, msg, 0)
	r.AddAttrs(attrs...)

	return r, true
}

// hostLogAttr builds the attribute of one key of a JSON log record.
// It keeps strings, booleans, and numbers as values of their own
// kind, so that every [slog.Handler] prints them as the subcommand
// logged them.
func hostLogAttr(key string, val any) slog.Attr {
	switch v := val.(type) {
	case string:
		return slog.String(key, v)
	case bool:
		return slog.Bool(key, v)
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return slog.Int64(key, i)
		}

		f, err := v.Float64()
		if err == nil {
			return slog.Float64(key, f)
		}
	}

	return slog.Any(key, val)
}
