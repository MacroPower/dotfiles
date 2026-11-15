package main

import (
	"fmt"
	"strings"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/clarity"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/definitions"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
)

// clarityLabel opens the first line of each description that comes from
// the Clarity database.
const clarityLabel = "Clarity: "

// clarityCredit is the line that follows a perk list with a description
// of the Clarity database. It names the source of each description. The
// server writes the line that [writeClarity] adds about the final patch,
// so the credit leaves it out.
const clarityCredit = `Under each perk, the lines before "Clarity:" are the in-game text of Bungie, ` +
	`and the lines from "Clarity:" on are the tested description of the Clarity database (d2clarity.com), ` +
	`except a line that starts with "Might predate the final patch", which the server adds.`

// writePerks writes the perk list of an item, one list item for each
// perk. The item names the perk, its type, and the perks that its socket
// can swap to. The in-game text of Bungie follows on indented lines.
//
// A perk that the database in tested describes also shows the
// description of Clarity. [writeClarity] writes that description under
// the text of Bungie, as the web client does. An enhanced perk without a
// description of its own shows the description of its base perk, as in
// the web client. [clarity.Database.Base] finds the base perk by name.
// Base finds no weapon frame, so an enhanced frame without a description
// of its own shows only the text of Bungie.
//
// One line after the list names the source of the descriptions. When
// tested holds a database, the line is [clarityCredit], and a list
// without a description of Clarity has no such line. When tested holds
// an Err, the line says that the tested descriptions are unavailable and
// gives the reason.
func writePerks(b *strings.Builder, perks []inventory.Perk, tested clarity.State) {
	credited := false

	for _, perk := range perks {
		fmt.Fprintf(b, "- %s (%s)", perk.Name, perk.Type)

		if len(perk.Options) > 0 {
			fmt.Fprintf(b, ", can swap to %s", strings.Join(perk.OptionNames(), ", "))
		}

		b.WriteString("\n")
		writeDescription(b, perk.Description)

		desc, ok := tested.Database.Description(perk.Hash)
		if !ok && perk.Enhanced() {
			desc, ok = tested.Database.Base(definitions.BasePerkName(perk.Name))
		}

		if ok {
			writeClarity(b, desc)

			credited = true
		}
	}

	switch {
	case tested.Err != nil:
		fmt.Fprintf(b, "Clarity %s. The perks show only the in-game text of Bungie.\n", tested.Err)
	case credited:
		b.WriteString(clarityCredit + "\n")
	}
}

// writeClarity writes the lines of a Clarity description indented under
// the list item of its perk. The first line carries [clarityLabel].
//
// One more line follows when a patch changed the perk after Clarity last
// updated the text, as [clarity.Description.Outdated] reports. The line
// says that the text might predate the final patch. It names the patch,
// the day of the patch notes, and the perk or the exotic item that the
// patch changed.
func writeClarity(b *strings.Builder, desc clarity.Description) {
	label := clarityLabel

	for _, line := range desc.Lines {
		fmt.Fprintf(b, "  %s%s\n", label, line)

		label = ""
	}

	change, ok := desc.Outdated()
	if ok {
		fmt.Fprintf(
			b,
			"  Might predate the final patch: Destiny 2 Update %s (%s) changed %s after Clarity last updated this text.\n",
			change.Patch.Version,
			change.Patch.Date.Format(time.DateOnly),
			change.Name,
		)
	}
}
