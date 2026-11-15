package clarity

import (
	"slices"
	"time"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/fold"
)

// Patch is one update of Destiny 2. Version is the version that the patch
// notes of Bungie carry, such as "9.7.0". Date is the day on which Bungie
// published the notes, at midnight UTC.
type Patch struct {
	Date    time.Time
	Version string
}

// Change is a change that one [Patch] made to how a perk plays. Name is
// the name under which the patch notes list the change. It is the name of
// the perk, or the name of the exotic item that holds the perk.
type Change struct {
	Name  string
	Patch Patch
}

// patchNotes lists what the notes of one patch change. Perks names the
// perks, the mods, the aspects, the fragments, and the class abilities.
// Items names the exotic weapons and the exotic armor. Each name has the
// spelling of the game.
type patchNotes struct {
	patch Patch
	perks []string
	items []string
}

// patchIndex holds the names of one [patchNotes] for a lookup. Each map
// takes a name that [fold.Name] folded and gives the spelling of the
// list.
type patchIndex struct {
	perks map[string]string
	items map[string]string
	patch Patch
}

var (
	// finalPatches lists the changes of Destiny 2 Update 9.7.0 and of its
	// hotfixes, in the order of their dates. Update 9.7.0 was the last
	// major update of the game, and its hotfixes made the last changes to
	// the perks. The list comes from the patch notes that Bungie published
	// at https://www.bungie.net/7/en/News/Article/destiny_update_9_7_0 and
	// at the same address with the suffix _1 or _2.
	//
	// The list names each perk and each exotic item whose play the notes
	// change. A bug fix counts when it changes what a perk does. The list
	// follows these rules:
	//
	//   - It leaves out a change that the notes call visual, such as a
	//     timer on the screen, and a change that only rewords a
	//     description.
	//   - It leaves out a change to a whole weapon type or to every
	//     weapon, because such a change names no perk. The damage of
	//     every rocket-assisted pulse rifle and the anti-Champion effect
	//     of each weapon frame are such changes.
	//   - It leaves out the supers, the grenades, the melee abilities, the
	//     armor set bonuses, and the artifact perks. Clarity describes
	//     none of them.
	//   - It leaves out a fix to the progress of a catalyst objective.
	//   - It names an exotic item when the notes change the item, and each
	//     perk of the item then counts as changed. Clarity files a few
	//     exotic perks without the name of their item, and the list names
	//     those perks.
	//
	// Bungie shipped four more hotfixes, and the last one, 9.7.0.6,
	// followed on 2026-10-20. The notes of 9.7.0.3, 9.7.0.5, and 9.7.0.6
	// change no perk that Clarity describes, and Bungie published no notes
	// for 9.7.0.4. The list therefore holds no entry for them.
	finalPatches = []patchNotes{
		{
			patch: Patch{Version: "9.7.0", Date: time.Date(2026, time.June, 9, 0, 0, 0, 0, time.UTC)},
			perks: []string{
				// Armor mods.
				"Better Already",
				"Elemental Charge",
				"Grenade Kickstart",
				"Harmonic Holster",
				"Melee Kickstart",
				"Recuperation",
				"Solar Holster",

				// Perks of the exotic class items.
				"Spirit of Galanor",
				"Spirit of the Star-Eater",
				"Spirit of the Wormhusk",

				// Weapon perks and origin traits.
				"Adagio",
				"Adrenaline Junkie",
				"Advanced Reflexes",
				"Aggregate Charge",
				"All-Star",
				"Attrition Orbs",
				"Cascade Point",
				"Collective Action",
				"Collective Pugilism",
				"Contending Cascade",
				"Cooling Baubles",
				"Desperado",
				"Envious Assassin",
				"Focused Fury",
				"Frenzy",
				"Full Choke",
				"Full Court",
				"Gun and Run",
				"Gutshot Straight",
				"Jolting Feedback",
				"Lasting Impression",
				"Meganeura",
				"Nanotech Tracer Missiles",
				"Paracausal Fluid",
				"Physic",
				"Reservoir Burst",
				"Sharp Harvest",
				"Withering Gaze",

				// Weapon frames.
				"Caster Frame",
				"Shot Package",
				"Vortex Frame",

				// Perks of Hard Light, Borealis, Dead Messenger, and Outbreak
				// Perfected that Clarity files without the name of the item.
				"Parasitism",
				"The Corruption Spreads",
				"The Fundamentals",
				"Trinary Vision",

				// Fragments.
				"Facet of Dominance",
				"Facet of Honor",
				"Spark of Shock",
				"Whisper of Rime",

				// Aspects.
				"Arc Soul",
				"Ascension",
				"Bastion",
				"Child of the Old Gods",
				"Crackshot",
				"Cryoclasm",
				"Diamond Lance",
				"Drengr's Lash",
				"Ensnaring Slam",
				"Frostpulse",
				"Glacial Harvest",
				"Grim Harvest",
				"Heat Rises",
				"Hellion",
				"Icarus Dash",
				"Ionic Sentry",
				"Knock 'Em Down",
				"Knockout",
				"Lethal Current",
				"Lightning Surge",
				"Mindspun Invocation",
				"Offensive Bulwark",
				"On Your Mark",
				"On the Prowl",
				"Shatterdive",
				"Shieldburst",
				"Soul Siphon",
				"Tectonic Harvest",
				"The Wanderer",
				"Threaded Specter",
				"Touch of Thunder",
				"Trapper's Ambush",
				"Unbreakable",
				"Weavewalk",
				"Widow's Silk",
				"Winter's Shroud",

				// Class abilities.
				"Acrobat's Dodge",
				"Thruster",
			},
			items: []string{
				// Armor.
				"ACD/0 Feedback Fence",
				"Astrocyte Verse",
				"Blastwave Striders",
				"Cadmus Ridge Lancecap",
				"Caliban's Hand",
				"Contraverse Hold",
				"Crown of Tempests",
				"Dawn Chorus",
				"Deimosuffusion",
				"Fr0st-EE5",
				"Hallowfire Heart",
				"Helm of Saint-14",
				"Lucky Pants",
				"Mantle of Battle Harmony",
				"Mataiodox\u00EDa",
				"Melas Panoplia",
				"Mothkeeper's Wraps",
				"Necrotic Grip",
				"Nezarec's Sin",
				"No Backup Plans",
				"Nothing Manacles",
				"Oathkeeper",
				"Peacekeepers",
				"Peregrine Greaves",
				"Point-Contact Cannon Brace",
				"Praxic Vestment",
				"Pyrogale Gauntlets",
				"Raiden Flux",
				"Sealed Ahamkara Grasps",
				"Secant Filaments",
				"Severance Enclosure",
				"Shards of Galanor",
				"Shinobu's Vow",
				"Skull of Dire Ahamkara",
				"Stronghold",
				"The Bombardiers",
				"Wormhusk Crown",

				// Weapons.
				"Anarchy",
				"Arbalest",
				"Borealis",
				"Buried Bloodline",
				"Centrifuse",
				"Choir of One",
				"Coldheart",
				"Crimson",
				"Dead Messenger",
				"Delicate Tomb",
				"Divinity",
				"Dragon's Breath",
				"Edge of Action",
				"Edge of Concurrence",
				"Edge of Intent",
				"Graviton Lance",
				"Hard Light",
				"Heirloom",
				"Hierarchy of Needs",
				"Ice Breaker",
				"MIDA Multi-Tool",
				"Merciless",
				"Monte Carlo",
				"Necrochasm",
				"New Land Beyond",
				"Osteo Striga",
				"Outbreak Perfected",
				"Praxic Blade",
				"Prometheus Lens",
				"Riskrunner",
				"SUROS Regime",
				"Skyburner's Oath",
				"Slayer's Fang",
				"Sleeper Simulant",
				"Sunshot",
				"Sweet Business",
				"Tarrabah",
				"The Chaperone",
				"The Jade Rabbit",
				"The Lament",
				"The Last Word",
				"The Prospector",
				"The Wardcliff Coil",
				"Thorn",
				"Thunderlord",
				"Touch of Malice",
				"Trinity Ghoul",
				"Truth",
				"Two-Tailed Fox",
				"Vex Mythoclast",
				"Whirling Ovation",
				"Winterbite",
				"Wolfsbane",
				"Xenophage",
			},
		},
		{
			patch: Patch{Version: "9.7.0.1", Date: time.Date(2026, time.June, 16, 0, 0, 0, 0, time.UTC)},
			perks: []string{
				"Heat Rises",
				"Howl of the Storm",
			},
			items: []string{
				"Celestial Nighthawk",
				"Hazardous Propulsion",
			},
		},
		{
			patch: Patch{Version: "9.7.0.2", Date: time.Date(2026, time.June, 23, 0, 0, 0, 0, time.UTC)},
			perks: []string{
				"Accelerated Assault",
				"Crackshot",
				"Discord",
				"Forge's Kin",
				"Lethal Current",
			},
			items: []string{
				"Ergo Sum",
				"Ex Diris",
				"Fr0st-EE5",
				"Hierarchy of Needs",
				"Mothkeeper's Wraps",
				"Rime-coat Raiment",
				"Shards of Galanor",
				"Shinobu's Vow",
				"Truth",
				"Turncoat",
				"Vesper of Radius",
			},
		},
	}

	// finalIndex holds the lookup of each entry of [finalPatches], in the
	// order of the list.
	finalIndex = indexPatches(finalPatches)
)

// indexPatches returns the [patchIndex] of each entry of notes, in the
// order of notes.
func indexPatches(notes []patchNotes) []patchIndex {
	folded := func(names []string) map[string]string {
		out := make(map[string]string, len(names))
		for _, name := range names {
			out[fold.Name(name)] = name
		}

		return out
	}

	out := make([]patchIndex, 0, len(notes))
	for _, n := range notes {
		out = append(out, patchIndex{patch: n.patch, perks: folded(n.perks), items: folded(n.items)})
	}

	return out
}

// Outdated returns the last [Change] that a patch made to the perk of d
// after Clarity last updated the text. The text of such a perk might
// describe how the perk played before the patch. Outdated reports false
// when no patch changed the perk after the upload.
//
// The patches are Destiny 2 Update 9.7.0 and its hotfixes, the last
// updates that changed how the perks play. A patch counts as later than
// the upload when the upload came before the day of the patch notes, at
// midnight UTC. A patch changed the perk when its notes name the perk or
// the exotic item that holds the perk. Outdated compares the names after
// [fold.Name] folds them.
func (d Description) Outdated() (Change, bool) {
	perk, item := fold.Name(d.Name), fold.Name(d.Item)

	for _, notes := range slices.Backward(finalIndex) {
		// The list is in the order of the dates, so no earlier patch
		// came after the upload either.
		if !d.Uploaded.Before(notes.patch.Date) {
			break
		}

		if name, ok := notes.perks[perk]; ok {
			return Change{Name: name, Patch: notes.patch}, true
		}

		if name, ok := notes.items[item]; ok {
			return Change{Name: name, Patch: notes.patch}, true
		}
	}

	return Change{}, false
}
