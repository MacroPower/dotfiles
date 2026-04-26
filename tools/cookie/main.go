package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// shortLen is the upper byte-length bound for entries considered by
// the -s flag.
const shortLen = 300

// separator delimits entries in a fortune file: a line containing
// exactly '%'. Files end with the separator, so splitting yields a
// trailing empty entry that must be dropped.
const separator = "\n%\n"

// defaultWidth is the wrap width used by [bubble] when -w is not set.
// Matches cowsay's default.
const defaultWidth = 40

// randomCow is the reserved -cow value that [pickCow] interprets as
// "pick uniformly from the embedded cows".
const randomCow = "random"

var (
	//go:embed fortunes/*
	fortunes embed.FS

	//go:embed cows/*.cow
	cows embed.FS

	// ErrUnknownCow is returned by [pickCow] when -cow names a file that
	// is not in the embedded cows directory.
	ErrUnknownCow = errors.New("unknown cow")
)

// loadAll reads every embedded fortune file and returns the merged,
// trimmed, non-empty entries.
func loadAll() ([]string, error) {
	var entries []string

	walkErr := fs.WalkDir(fortunes, "fortunes", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		data, readErr := fortunes.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}

		entries = append(entries, parse(string(data))...)

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking fortunes: %w", walkErr)
	}

	return entries, nil
}

// parse splits a fortune file body on [separator], trims trailing
// whitespace from each entry, and drops empties.
func parse(body string) []string {
	parts := strings.Split(body, separator)
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		trimmed := strings.TrimRight(p, " \t\n\r")
		if trimmed == "" {
			continue
		}

		out = append(out, trimmed)
	}

	return out
}

// filterShort returns only entries whose byte length is at most
// maxLen. Empty entries are skipped unconditionally, mirroring the
// upstream patch's `!q.is_empty()` guard so the random pick can never
// land on a blank fortune.
func filterShort(entries []string, maxLen int) []string {
	out := make([]string, 0, len(entries))

	for _, e := range entries {
		if e == "" {
			continue
		}

		if len(e) <= maxLen {
			out = append(out, e)
		}
	}

	return out
}

// pickRandom returns one entry chosen uniformly at random from
// entries using rng. Callers must ensure entries is non-empty.
func pickRandom(rng *rand.Rand, entries []string) string {
	return entries[rng.IntN(len(entries))]
}

// wrap formats text into a flat slice of output lines.
//
// Newline handling:
//   - A single '\n' followed by non-whitespace is a soft break: the
//     surrounding words flow together with a single space.
//   - A run of two or more '\n' emits one paragraph break.
//   - A '\n' followed by ' ' or '\t' emits one break; the triggering
//     whitespace is stripped from the next line.
//
// Each resulting logical line is greedy-wrapped; tokens longer than
// width are hard-broken on rune boundaries (matching cowsay). Empty
// input yields one empty line so the bubble still draws. Width must
// be positive; the caller is expected to validate.
func wrap(text string, width int) []string {
	if width <= 0 {
		panic("wrap: width must be positive")
	}

	if text == "" {
		return []string{""}
	}

	lines := splitLogicalLines(text)
	if len(lines) == 0 {
		return []string{""}
	}

	var out []string

	for _, line := range lines {
		out = append(out, wrapParagraph(line, width)...)
	}

	return out
}

// splitLogicalLines breaks text into the lines that [wrap] should
// process independently. See [wrap] for the newline rules.
func splitLogicalLines(text string) []string {
	var (
		out []string
		cur strings.Builder
	)

	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(text); i++ {
		c := text[i]
		if c != '\n' {
			cur.WriteByte(c)
			continue
		}

		if i+1 < len(text) && text[i+1] == '\n' {
			flush()

			for i+1 < len(text) && text[i+1] == '\n' {
				i++
			}

			continue
		}

		if i+1 < len(text) && (text[i+1] == ' ' || text[i+1] == '\t') {
			flush()
			continue
		}

		if i+1 == len(text) {
			continue
		}

		cur.WriteByte(' ')
	}

	flush()

	return out
}

func wrapParagraph(text string, width int) []string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return []string{""}
	}

	var (
		out     []string
		current strings.Builder
		curLen  int
	)

	flush := func() {
		out = append(out, current.String())
		current.Reset()

		curLen = 0
	}

	for _, word := range fields {
		wordLen := utf8.RuneCountInString(word)

		if wordLen > width {
			if curLen > 0 {
				flush()
			}

			runes := []rune(word)
			for i := 0; i < len(runes); i += width {
				end := min(i+width, len(runes))
				chunk := string(runes[i:end])
				chunkLen := end - i

				if chunkLen == width {
					out = append(out, chunk)
				} else {
					current.WriteString(chunk)

					curLen = chunkLen
				}
			}

			continue
		}

		switch {
		case curLen == 0:
			current.WriteString(word)

			curLen = wordLen

		case curLen+1+wordLen <= width:
			current.WriteByte(' ')
			current.WriteString(word)

			curLen += 1 + wordLen

		default:
			flush()
			current.WriteString(word)

			curLen = wordLen
		}
	}

	if curLen > 0 {
		flush()
	}

	return out
}

// bubble renders lines as a speech bubble using rounded box-drawing
// characters. A single-line input is sized to the text; multi-line
// input pads each row to width so the right edge stays aligned.
// Callers must pass a non-empty slice of lines (typically from
// [wrap]).
func bubble(lines []string, width int) string {
	body := width
	if len(lines) == 1 {
		body = utf8.RuneCountInString(lines[0])
	}

	rule := strings.Repeat("─", body+2)

	var sb strings.Builder

	sb.WriteString(" ╭")
	sb.WriteString(rule)
	sb.WriteString("╮\n")

	for _, line := range lines {
		pad := max(body-utf8.RuneCountInString(line), 0)

		sb.WriteString(" │ ")
		sb.WriteString(line)
		sb.WriteString(strings.Repeat(" ", pad))
		sb.WriteString(" │\n")
	}

	sb.WriteString(" ╰")
	sb.WriteString(rule)
	sb.WriteString("╯")

	return sb.String()
}

// pickCow reads a cow body from the embedded cows directory. The
// special name [randomCow] picks one entry uniformly at random using
// rng. Any other unknown name returns [ErrUnknownCow].
func pickCow(rng *rand.Rand, name string) (string, error) {
	if name == randomCow {
		entries, err := cows.ReadDir("cows")
		if err != nil {
			return "", fmt.Errorf("reading cows directory: %w", err)
		}

		if len(entries) == 0 {
			return "", errors.New("no cows embedded")
		}

		chosen := entries[rng.IntN(len(entries))]

		data, err := cows.ReadFile("cows/" + chosen.Name())
		if err != nil {
			return "", fmt.Errorf("reading cow %q: %w", chosen.Name(), err)
		}

		return string(data), nil
	}

	data, err := cows.ReadFile("cows/" + name + ".cow")
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrUnknownCow, name)
	}

	return string(data), nil
}

// render wraps fortune in a bubble of the given width and appends the
// chosen cow body verbatim.
func render(rng *rand.Rand, fortune, cowName string, width int) (string, error) {
	cowBody, err := pickCow(rng, cowName)
	if err != nil {
		return "", err
	}

	return bubble(wrap(fortune, width), width) + "\n" + cowBody, nil
}

// format returns the final stdout payload: the bare fortune when
// cowName is empty, otherwise the bubble-and-cow rendering.
func format(rng *rand.Rand, fortune, cowName string, width int) (string, error) {
	if cowName == "" {
		return fortune, nil
	}

	return render(rng, fortune, cowName, width)
}

func run() error {
	short := flag.Bool("s", false, "only consider short entries")
	cowName := flag.String("cow", "", "render in a speech bubble with the named character (use \"random\" for any)")
	width := flag.Int("w", defaultWidth, "wrap width for the bubble body")

	flag.Parse()

	if *width <= 0 {
		return fmt.Errorf("width must be positive, got %d", *width)
	}

	entries, err := loadAll()
	if err != nil {
		return err
	}

	if *short {
		entries = filterShort(entries, shortLen)
	}

	if len(entries) == 0 {
		return errors.New("no fortunes available")
	}

	seed := uint64(time.Now().UnixNano())
	//nolint:gosec // cookie picking is not security-sensitive
	rng := rand.New(rand.NewPCG(seed, seed))

	out, err := format(rng, pickRandom(rng, entries), *cowName, *width)
	if err != nil {
		return err
	}

	fmt.Println(out)

	return nil
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
