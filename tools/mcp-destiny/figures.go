package main

import (
	"fmt"
	"slices"
	"strings"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/damage"
	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/inventory"
)

// allFiguresUnknown opens the line of a weapon copy without a time to
// kill and without a damage per second. The reason follows it.
const allFiguresUnknown = "Time to kill and damage per second: unknown."

// Labels of the two ways in which a copy hits its target.
const (
	withPrecision = "with precision hits"
	withBody      = "with body shots"
)

// reportOf returns the [damage.Report] of a weapon copy. The report
// reads the type, the stats, and the reload time of the copy. It also
// reads the name and the type of the plug of each perk socket.
func reportOf(item *inventory.Item) damage.Report {
	weapon := damage.Weapon{
		Type:   item.TypeName,
		Stats:  statsOf(item),
		Reload: func(bonus int) curves.Measure { return reloadOf(item, bonus) },
	}

	for _, perk := range item.Perks {
		weapon.Plugs = append(weapon.Plugs, damage.Plug{Name: perk.Name, Type: perk.Type})
	}

	return damage.Of(weapon)
}

// reloadOf returns the time of a full reload of a weapon copy with its
// Reload Speed stat raised by bonus. The curves cap the raised stat, as
// the game caps it. A copy without the stat keeps the reason of its
// reload time.
func reloadOf(item *inventory.Item, bonus int) curves.Measure {
	weapon := curvesWeaponOf(item)

	stat, ok := weapon.Stats[curves.StatReload]
	if ok {
		weapon.Stats[curves.StatReload] = stat + bonus
	}

	return curves.Of(weapon).Reload
}

// writeFigures writes the time to kill and the damage per second of a
// weapon copy as a block of the get_item output. The block opens with
// the health of the Guardian and the game update that the numbers match.
// It holds the base numbers and then the numbers of each state of each
// perk that the damage data models. Its last line names the active perks
// that the numbers leave out. One line replaces the block when the copy
// has no numbers, and the line gives the reason.
func writeFigures(b *strings.Builder, item *inventory.Item) {
	report := reportOf(item)
	if report.Reason != "" {
		fmt.Fprintf(b, "\n%s %s\n", allFiguresUnknown, report.Reason)

		return
	}

	fmt.Fprintf(
		b,
		"\nTime to kill a Guardian with %.0f health, and damage per second while the copy fires one magazine "+
			"and reloads, from community measurements and Bungie's patch notes at Update %s:\n",
		report.Health, report.Patch,
	)

	writeFigureLines(b, "Base", &report.Base)

	for i := range report.Perks {
		figures := &report.Perks[i]
		writeFigureLines(b, "With "+strings.TrimSpace(figures.Perk+" "+figures.State), figures)
	}

	if len(report.Unmodeled) > 0 {
		fmt.Fprintf(b, "- Perks that the numbers leave out: %s\n", strings.Join(report.Unmodeled, ", "))
	}
}

// writeFigureLines writes the numbers of one state under its label. One
// line gives the known times to kill, and one gives the known damage per
// second. A line for each reason then names the values that are unknown
// for the reason.
func writeFigureLines(b *strings.Builder, label string, figures *damage.Figures) {
	values := []figureValue{
		{
			metric: metricKill,
			way:    withPrecision,
			text:   killText(figures.OptimalKill),
			reason: figures.OptimalKill.Reason,
		},
		{metric: metricKill, way: withBody, text: killText(figures.BodyKill), reason: figures.BodyKill.Reason},
		{
			metric: metricRate, way: withPrecision,
			text: rateText(figures.PrecisionRate, withPrecision), reason: figures.PrecisionRate.Reason,
		},
		{
			metric: metricRate,
			way:    withBody,
			text:   rateText(figures.BodyRate, withBody),
			reason: figures.BodyRate.Reason,
		},
	}

	fmt.Fprintf(b, "- %s:\n", label)

	for _, metric := range figureMetrics {
		var known []string

		for _, value := range values {
			if value.metric == metric && value.text != "" {
				known = append(known, value.text)
			}
		}

		if len(known) > 0 {
			fmt.Fprintf(b, "  %s: %s\n", metric, strings.Join(known, ", "))
		}
	}

	var reasons []string

	for _, value := range values {
		if value.text == "" && !slices.Contains(reasons, value.reason) {
			reasons = append(reasons, value.reason)
		}
	}

	for _, reason := range reasons {
		fmt.Fprintf(b, "  %s: unknown. %s\n", unknownLabel(values, reason), reason)
	}
}

// Names of the two numbers of a state.
const (
	metricKill = "Time to kill"
	metricRate = "Damage per second"
)

// figureMetrics lists the numbers of a state in the order in which the
// tools print them.
var figureMetrics = [...]string{metricKill, metricRate}

// figureValue is one value of a state. Metric names its number, and way
// names how the copy hits, such as "with body shots". Text is the value,
// and it is empty for an unknown value. Reason says why the value is
// unknown.
type figureValue struct {
	metric string
	way    string
	text   string
	reason string
}

// unknownLabel names the values that are unknown for one reason. A
// number whose two values are unknown goes by its name alone, such as
// "Damage per second". Otherwise the way follows the name. When each
// number lacks the same one value, the way follows the two names one
// time, as in "Time to kill and damage per second with precision hits".
func unknownLabel(values []figureValue, reason string) string {
	ways := map[string][]string{}

	for _, value := range values {
		if value.text == "" && value.reason == reason {
			ways[value.metric] = append(ways[value.metric], value.way)
		}
	}

	kill, rate := ways[metricKill], ways[metricRate]
	if len(kill) == 1 && len(rate) == 1 && kill[0] == rate[0] {
		return metricKill + " and " + strings.ToLower(metricRate) + " " + kill[0]
	}

	var names []string

	for _, metric := range figureMetrics {
		switch len(ways[metric]) {
		case 0:
			continue
		case 1:
			names = append(names, metric+" "+ways[metric][0])
		default:
			names = append(names, metric)
		}
	}

	if len(names) == 2 {
		names[1] = strings.ToLower(names[1])
	}

	return strings.Join(names, " and ")
}

// killText returns a kill as its time and its hits, such as "0.87 s with
// 2 precision hits and 1 body shot". It returns an empty string for an
// unknown kill.
func killText(kill damage.Kill) string {
	if !kill.Known() {
		return ""
	}

	var hits []string

	if kill.Precision > 0 {
		hits = append(hits, countText(kill.Precision, "precision hit"))
	}

	if kill.Body > 0 {
		hits = append(hits, countText(kill.Body, "body shot"))
	}

	return fmt.Sprintf("%.2f s with %s", kill.Seconds, strings.Join(hits, " and "))
}

// rateText returns a damage per second with the way in which the copy
// hits, such as "133 with precision hits". It returns an empty string
// for an unknown rate.
func rateText(rate damage.Rate, when string) string {
	if !rate.Known() {
		return ""
	}

	return fmt.Sprintf("%.0f %s", rate.Value, when)
}

// countText writes n with a noun, such as "1 body shot" or "3 body
// shots".
func countText(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}
