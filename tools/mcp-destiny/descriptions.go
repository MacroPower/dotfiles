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
// perk. The item names the perk, its type, the perks that its socket can
// swap to, and the perks of the socket that Bungie reports as locked
// after [inventory.LockedLabel]. The placeholder of an empty socket has no
// type, so its item leaves out the type. The in-game text of Bungie
// follows on indented lines. Each aspect of a subclass, active or a swap,
// also gives the number of fragment sockets that it opens, as
// [fragmentSlots] words it.
//
// A perk that the database in tested describes also shows the
// description of Clarity. [writeClarity] writes that description under
// the text of Bungie, as the web client does. An enhanced perk without a
// description of its own shows the description of its base perk, as in
// the web client. [clarity.Database.Base] finds the base perk by name.
// Base finds no weapon frame, so an enhanced frame without a description
// of its own shows only the text of Bungie. An aspect without a
// description of its own, such as an aspect of a Prismatic subclass,
// shows the description of the aspect with its name.
// [clarity.Database.Aspect] finds that aspect.
//
// One line after the list names the source of the descriptions. When
// tested holds a database, the line is [clarityCredit], and a list
// without a description of Clarity has no such line. When tested holds
// an Err, the line says that the tested descriptions are unavailable and
// gives the reason.
func writePerks(b *strings.Builder, perks []inventory.Perk, tested clarity.State) {
	credited := false

	for _, perk := range perks {
		var details []string
		if perk.Type != "" {
			details = append(details, perk.Type)
		}

		if perk.FragmentSlots > 0 {
			details = append(details, fragmentSlots(perk.FragmentSlots))
		}

		b.WriteString("- " + perk.Name)

		if len(details) > 0 {
			fmt.Fprintf(b, " (%s)", strings.Join(details, ", "))
		}

		separator := ", "

		if len(perk.Options) > 0 {
			fmt.Fprintf(b, ", can swap to %s", optionLabels(perk.Options))

			separator = "; "
		}

		if len(perk.Locked) > 0 {
			b.WriteString(separator + inventory.LockedLabel + optionLabels(perk.Locked))
		}

		b.WriteString("\n")
		writeDescription(b, perk.Description)

		desc, ok := tested.Database.Description(perk.Hash)
		if !ok && perk.Enhanced() {
			desc, ok = tested.Database.Base(definitions.BasePerkName(perk.Name))
		}

		// Only the aspects open fragment slots.
		if !ok && perk.FragmentSlots > 0 {
			desc, ok = tested.Database.Aspect(perk.Name)
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

// optionLabels names the plugs that a socket can swap to, separated by
// commas. The label of an aspect also gives the number of fragment
// sockets that the aspect opens, such as "Winter's Shroud (2 fragment
// slots)".
func optionLabels(options []inventory.Plug) string {
	labels := make([]string, 0, len(options))

	for _, option := range options {
		if option.FragmentSlots == 0 {
			labels = append(labels, option.Name)
		} else {
			labels = append(labels, option.Name+" ("+fragmentSlots(option.FragmentSlots)+")")
		}
	}

	return strings.Join(labels, ", ")
}

// fragmentSlots words the number of fragment sockets that an aspect
// opens, such as "1 fragment slot" or "3 fragment slots".
func fragmentSlots(n int) string {
	if n == 1 {
		return "1 fragment slot"
	}

	return fmt.Sprintf("%d fragment slots", n)
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
