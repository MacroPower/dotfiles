package clarity

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

const (
	// language is the key of the English lines in a description of the
	// descriptions file.
	language = "en"

	// classPvP is the class name that Clarity gives a value that applies
	// only against players.
	classPvP = "pvp"

	// pvpOpen opens a value that applies only against players in the
	// text of a [Description].
	pvpOpen = "[PvP "

	// EnhancedMark precedes each value that an enhanced perk improves in
	// the text of a [Description].
	EnhancedMark = "\u2191"

	// enhancedSuffix ends the type that Clarity gives the entry of an
	// enhanced perk, such as Weapon Trait Enhanced.
	enhancedSuffix = " Enhanced"

	// aspectType is the type that Clarity gives the entry of an aspect of
	// a subclass.
	aspectType = "Subclass Aspect"
)

var (
	// errNoDescriptions reports a descriptions file without a description.
	errNoDescriptions = errors.New("the descriptions file holds no description")

	// baseTypes lists the types of the entries that [Database.Base]
	// finds.
	baseTypes = []string{"Weapon Trait", "Weapon Trait Origin", "Weapon Perk"}

	// symbols rewrites the characters of the Clarity text that few fonts
	// draw, and it removes the characters without a width. The up arrow is
	// [EnhancedMark]. Clarity writes a quote between two block characters,
	// and the pairs with a space come first, so the space leaves with the
	// block character.
	symbols = strings.NewReplacer(
		"\U0001F845", EnhancedMark,
		"\U0001F81A", "\u2192",
		"\u21D2", "\u2192",
		"\u259B ", `"`,
		"\u259B", `"`,
		" \u259F", `"`,
		"\u259F", `"`,
		"\u2060", "",
		"\uFEFF", "",
	)
)

// Description is the text that Clarity publishes for one perk. Name is
// the name that Clarity gives the perk. Item is the name of the exotic
// item that owns the perk, and it is empty for a perk that many items
// can hold. Type is the kind of perk that Clarity gives the entry, such
// as Weapon Trait Origin. Lines holds the text, one entry for each line,
// and it never holds a blank line. Uploaded is the time of the last
// change that Clarity made to the text. For an enhanced perk, [Decode]
// sets it to the later upload of the perk and of its base perk.
//
// A line can start with a bullet. A value in square brackets after the
// word PvP applies only against players, and the value before it applies
// against combatants. [EnhancedMark] precedes each value that the
// enhanced perk improves.
type Description struct {
	Uploaded time.Time
	Name     string
	Item     string
	Type     string
	Lines    []string
}

// Database is one version of the Clarity database. The item hash of the
// plug of each perk keys Descriptions. Version is the version that the
// site gave the descriptions file.
//
// The first call of [Database.Base] or [Database.Aspect] indexes
// Descriptions, and the later calls of both read that index. A caller
// therefore must not change Descriptions after that call.
type Database struct {
	Descriptions map[uint32]Description

	// bases maps a name that [fold.Name] folded to the hash of the entry
	// that [Database.Base] returns for it, and aspects does the same for
	// [Database.Aspect]. The first call of Base or Aspect builds both
	// from Descriptions.
	bases   map[string]uint32
	aspects map[string]uint32

	Version string

	once sync.Once
}

// Description returns the description of the perk whose plug has the
// item hash. It reports false when the database holds none. A nil
// database holds no description.
func (d *Database) Description(hash uint32) (Description, bool) {
	if d == nil {
		return Description{}, false
	}

	desc, ok := d.Descriptions[hash]

	return desc, ok
}

// Base returns the description of the base perk with the name, for an
// enhanced perk without an entry of its own. It compares the names after
// [fold.Name] folds both, and it reports false when the database holds
// no such perk. A nil database holds no description.
//
// Base finds only the kinds of perks that Destiny Item Manager maps to
// an enhanced perk. They are the weapon traits, the origin traits, and
// the perks of the other weapon columns, such as barrels and magazines.
// Base skips the entries that Clarity types as exotic or as enhanced. It
// also skips the weapon frames, because Bungie gives one frame name to
// the frames of several weapon types that play differently. Precision
// Frame is one such name.
//
// Clarity can hold several entries with one name, such as the origin
// trait of each weapon that carries it. Base then returns the entry with
// the lowest hash, so each call returns the same entry.
func (d *Database) Base(name string) (Description, bool) {
	if d == nil {
		return Description{}, false
	}

	d.once.Do(d.index)

	hash, ok := d.bases[fold.Name(name)]
	if !ok {
		return Description{}, false
	}

	return d.Descriptions[hash], true
}

// Aspect returns the description of the aspect of a subclass with the
// name, for an aspect without an entry of its own. It compares the names
// after [fold.Name] folds both, and it reports false when the database
// does not hold such an aspect. A nil database does not hold any
// description.
//
// The Prismatic subclasses give each of their aspects a plug of its own.
// Clarity keys the entry of an aspect by the plug of the aspect on its
// elemental subclass. The text of the entry covers both plugs, so a
// caller finds the description of a Prismatic aspect by its name.
// Aspect reads only the entries that Clarity types as aspects. Of
// several such entries with one name, it returns the entry with the
// lowest hash, so each call returns the same entry.
func (d *Database) Aspect(name string) (Description, bool) {
	if d == nil {
		return Description{}, false
	}

	d.once.Do(d.index)

	hash, ok := d.aspects[fold.Name(name)]
	if !ok {
		return Description{}, false
	}

	return d.Descriptions[hash], true
}

// index builds the indexes of [Database.Base] and [Database.Aspect] from
// Descriptions.
func (d *Database) index() {
	d.bases = map[string]uint32{}
	d.aspects = map[string]uint32{}

	for hash, desc := range d.Descriptions {
		var names map[string]uint32

		switch {
		case slices.Contains(baseTypes, desc.Type):
			names = d.bases
		case desc.Type == aspectType:
			names = d.aspects
		default:
			continue
		}

		key := fold.Name(desc.Name)

		kept, ok := names[key]
		if !ok || hash < kept {
			names[key] = hash
		}
	}
}

// rawPerk is the subset of one entry of the descriptions file that
// [Decode] reads. LastUpload is a time in Unix milliseconds.
type rawPerk struct {
	Descriptions map[string][]rawLine `json:"descriptions"`
	Name         string               `json:"name"`
	ItemName     string               `json:"itemName"`
	Type         string               `json:"type"`
	LastUpload   int64                `json:"lastUpload"`
}

// rawLine is one line of a description. A line without content parts a
// description into paragraphs.
type rawLine struct {
	LinesContent []rawContent `json:"linesContent"`
}

// rawContent is one run of text in a line. The class names say how
// Clarity displays the run. A run without text stands for an icon.
type rawContent struct {
	Text       string   `json:"text"`
	ClassNames []string `json:"classNames"`
}

// Decode reads the content of a descriptions file, which the site
// published as version. The file is a JSON object that maps the item hash
// of each plug to its entry.
//
// Decode keeps each entry with at least one line of English text. It
// skips an entry whose key is not an item hash. It also skips an entry
// whose fields do not decode, so one malformed entry costs only its own
// description. A database without a description describes no perk, so
// Decode rejects a file that leaves none.
//
// Clarity stores each line as runs of text with display classes. Decode
// joins the runs of a line into plain text by these rules:
//
//   - A run without text stands for an icon, such as the icon of a damage
//     type before the name of a keyword of that type. The icon leaves no
//     text.
//   - A run of the class pvp holds a value in square brackets. The text
//     writes the word PvP inside the opening bracket.
//   - The arrow that Clarity puts before an enhanced value becomes
//     [EnhancedMark].
//   - The other arrows become a right arrow, and the block characters
//     around a quote become quotation marks.
//   - Each run of white space becomes one space.
//
// A line that holds no text after these rules leaves no line.
//
// Clarity copies the text of a base perk into the entry of each of its
// enhanced perks. It updates the copy when it updates the base perk, and
// the entry of the enhanced perk keeps the time of its own upload. Decode
// therefore dates each entry whose type ends in Enhanced at the later
// upload of the entry and of the base perk that [Database.Base] finds for
// its name. An enhanced weapon frame keeps the time of its own upload,
// because Base finds no weapon frame.
func Decode(version string, raw []byte) (*Database, error) {
	var entries map[string]json.RawMessage

	err := json.Unmarshal(raw, &entries)
	if err != nil {
		return nil, fmt.Errorf("decoding the descriptions file: %w", err)
	}

	db := &Database{Version: version, Descriptions: map[uint32]Description{}}

	for key, entry := range entries {
		hash, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			continue
		}

		var perk rawPerk

		err = json.Unmarshal(entry, &perk)
		if err != nil {
			continue
		}

		lines := textOf(perk.Descriptions[language])
		if len(lines) == 0 {
			continue
		}

		db.Descriptions[uint32(hash)] = Description{
			Name:     perk.Name,
			Item:     perk.ItemName,
			Type:     perk.Type,
			Lines:    lines,
			Uploaded: time.UnixMilli(perk.LastUpload).UTC(),
		}
	}

	if len(db.Descriptions) == 0 {
		return nil, errNoDescriptions
	}

	db.dateEnhanced()

	return db, nil
}

// dateEnhanced dates the entry of each enhanced perk at the later upload
// of the entry and of its base perk, by the rule of [Decode]. The index of
// [Database.Base] holds no enhanced entry, so the new times leave it
// valid.
func (d *Database) dateEnhanced() {
	for hash, desc := range d.Descriptions {
		if !strings.HasSuffix(desc.Type, enhancedSuffix) {
			continue
		}

		base, ok := d.Base(desc.Name)
		if ok && base.Uploaded.After(desc.Uploaded) {
			desc.Uploaded = base.Uploaded
			d.Descriptions[hash] = desc
		}
	}
}

// textOf returns the plain text of each line of a description that holds
// text, by the rules of [Decode].
func textOf(lines []rawLine) []string {
	var out []string

	for _, line := range lines {
		var b strings.Builder

		for _, content := range line.LinesContent {
			text := content.Text
			if slices.Contains(content.ClassNames, classPvP) && strings.HasPrefix(text, "[") {
				text = pvpOpen + text[1:]
			}

			b.WriteString(text)
		}

		text := strings.Join(strings.Fields(symbols.Replace(b.String())), " ")
		if text != "" {
			out = append(out, text)
		}
	}

	return out
}
