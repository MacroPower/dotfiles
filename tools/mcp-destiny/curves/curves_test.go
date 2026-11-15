package curves_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/curves"
)

// Plug hashes of the frames that several weapon types share.
const (
	frameAdaptive    uint32 = 1294026524
	frameAggressive  uint32 = 3468089894
	frameHighImpact  uint32 = 1019291327
	frameLightweight uint32 = 1458010786
	framePrecision   uint32 = 1636108362
)

// Reasons that the curve data gives for a value that it omits.
const (
	noFalloff  = "The community data has no damage falloff start for this weapon."
	flatDamage = "The community data shows that the damage of this weapon does not fall off with distance."
	noReload   = "The community data has no reload time for this weapon."
	noAim      = "The community data has no aim-down-sights time for this weapon."
	noType     = "The community data has no curves for this weapon type."
	noFrame    = "The community data has no curves for the frame of this weapon."
	noPlugs    = "The profile lists no perks for this copy, so the server cannot tell its frame."

	glaiveReload = "Update 9.7.0 changed the reload timing of glaives, and no measurement is newer."
	glaiveScale  = "The community data has no aim-down-sights falloff for glaives."
	tombReload   = "Update 9.7.0 changed the reload timing of Delicate Tomb, and no measurement is newer."
	jotunnReload = "Update 9.5.0 changed the reload timing of Jötunn, and no measurement is newer."
	machineGuess = "The source marks its aim-down-sights falloff of machine guns as a guess."

	shotgunReload = "The source does not say whether its reload time of shotguns covers one shell or a full reload."

	sweetScale = "Update 9.7.0 left Sweet Business out of its change to the aim-down-sights falloff of " +
		"High-Impact auto rifles, and no source gives the value of Sweet Business."
	exoticScale = "Updates 9.1.0 and 9.5.0 raised the aim-down-sights falloff of Aggressive and Lightweight " +
		"submachine guns, and no source says which exotic submachine guns follow them."
	chaperoneScale = "Update 9.5.0 gave The Chaperone the profile of Rapid-Fire slug shotguns and raised the " +
		"aim-down-sights falloff of Pinpoint slug shotguns, and no source gives the value of The Chaperone."
	dualityScale = "Update 9.5.0 raised the aim-down-sights falloff of Pinpoint slug shotguns, and no source says " +
		"whether Duality follows them."
)

// stats returns the stats of a copy with the given Reload Speed, Handling,
// and Range.
func stats(reload, handling, distance int) map[uint32]int {
	return map[uint32]int{
		curves.StatReload:   reload,
		curves.StatHandling: handling,
		curves.StatRange:    distance,
	}
}

// known returns a measure with a value.
func known(value float64) curves.Measure {
	return curves.Measure{Value: value}
}

// none returns a measure without a value.
func none(reason string) curves.Measure {
	return curves.Measure{Reason: reason}
}

// TestOf checks the measures against points of the source data. Each
// time and each distance from the hip is the formula of
// build_resources/weapon_formulas.json in Oracle Engine at the stats of
// the case. Each distance while aiming is the distance from the hip times
// the scale of src/weapons/stat_calc.rs, or times the scale that a later
// patch note of Bungie states.
func TestOf(t *testing.T) {
	t.Parallel()

	// Shared stat lines and their measures, by weapon type.
	var (
		fusion  = stats(45, 55, 62)
		cannon  = stats(51, 46, 44)
		pulse   = stats(49, 53, 68)
		shotgun = stats(42, 63, 70)
		smg     = stats(37, 70, 48)
	)

	// with returns base with the two falloff distances replaced.
	with := func(base curves.Measures, hip, aim curves.Measure) curves.Measures {
		base.HipFalloff = hip
		base.AimFalloff = aim

		return base
	}

	fusionBase := curves.Measures{
		Reload: known(2.061921802), Ready: known(0.419417443), Stow: known(0.287077896), Aim: known(0.318077230),
		HipFalloff: known(13.2348), AimFalloff: known(17.20524),
	}
	cannonBase := curves.Measures{
		Reload: known(2.543064760), Ready: known(0.342885714), Stow: known(0.377571429), Aim: known(0.255),
		HipFalloff: known(19.96), AimFalloff: known(29.94),
	}
	pulseBase := curves.Measures{
		Reload: known(1.889957907), Ready: known(0.378304270), Stow: known(0.361669950), Aim: known(0.353478460),
		HipFalloff: known(21.1), AimFalloff: known(35.87),
	}
	// The source gives every shotgun one reload time. It does not say
	// whether the time covers one shell or a full reload.
	shotgunBase := curves.Measures{
		Reload: none(shotgunReload), Ready: known(0.332785425), Stow: known(0.357647773), Aim: known(0.303896761),
		HipFalloff: known(5.828), AimFalloff: known(5.828),
	}
	smgBase := curves.Measures{
		Reload: known(2.003036590), Ready: known(0.304629883), Stow: known(0.269771076), Aim: known(0.227033585),
		HipFalloff: known(13.42), AimFalloff: known(18.788),
	}
	adaptiveAuto := curves.Measures{
		Reload: known(2.028396191), Ready: known(0.324317210), Stow: known(0.296243020), Aim: known(0.2585),
		HipFalloff: known(16.75), AimFalloff: known(26.8),
	}

	tests := map[string]struct {
		weapon curves.Weapon
		want   curves.Measures
	}{
		"auto rifle": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{frameAdaptive}, Stats: stats(37, 70, 48)},
			want:   adaptiveAuto,
		},
		// Update 9.7.0 lowered the scale of High-Impact auto rifles from
		// 1.7 to 1.6.
		"auto rifle, High-Impact Frame after Update 9.7.0": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{frameHighImpact}, Stats: stats(0, 0, 0)},
			want: curves.Measures{
				Reload: known(2.806730067), Ready: known(0.51985381), Stow: known(0.48414822), Aim: known(0.38975),
				HipFalloff: known(10.75), AimFalloff: known(17.2),
			},
		},
		// Update 9.7.0 left Sweet Business out of that change.
		"auto rifle, Sweet Business after Update 9.7.0": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{3755070117}, Stats: stats(50, 50, 50)},
			want: curves.Measures{
				Reload: known(1.810547317), Ready: known(0.38018481), Stow: known(0.34993022), Aim: known(0.296),
				HipFalloff: known(17), AimFalloff: none(sweetScale),
			},
		},
		"auto rifle, Cerberus+1 has no falloff data": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{3208839961}, Stats: stats(50, 50, 50)},
			want: curves.Measures{
				Reload: known(1.810547317), Ready: known(0.38018481), Stow: known(0.34993022), Aim: known(0.296),
				HipFalloff: none(noFalloff), AimFalloff: none(noFalloff),
			},
		},
		"a stat outside 0 to 100 reads as the nearest end": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{frameAdaptive}, Stats: stats(140, -5, 300)},
			want: curves.Measures{
				Reload: known(1.242209067), Ready: known(0.51985381), Stow: known(0.48414822), Aim: known(0.38975),
				HipFalloff: known(23.25), AimFalloff: known(37.2),
			},
		},
		// The source reads the reload stat of a bow only up to 85.
		"combat bow, reload stat past the cap": {
			weapon: curves.Weapon{Type: "Combat Bow", Plugs: []uint32{715195141}, Stats: stats(100, 60, 0)},
			want: curves.Measures{
				Reload: known(0.617304250), Ready: known(0.561859122), Stow: known(0.432048499),
				Aim: known(0.418038337), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"combat bow": {
			weapon: curves.Weapon{Type: "Combat Bow", Plugs: []uint32{2108556049}, Stats: stats(40, 60, 0)},
			want: curves.Measures{
				Reload: known(0.776945855), Ready: known(0.561859122), Stow: known(0.432048499),
				Aim: known(0.418038337), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"fusion rifle": {
			weapon: curves.Weapon{Type: "Fusion Rifle", Plugs: []uint32{frameAdaptive}, Stats: fusion},
			want:   fusionBase,
		},
		// Update 9.7.0 lowered the scale of Vex Mythoclast with the
		// High-Impact auto rifles.
		"fusion rifle, Vex Mythoclast after Update 9.7.0": {
			weapon: curves.Weapon{Type: "Fusion Rifle", Plugs: []uint32{3610750208}, Stats: fusion},
			want: curves.Measures{
				Reload: known(1.890912589), Ready: known(0.366217910), Stow: known(0.336508420), Aim: known(0.286625),
				HipFalloff: known(17.7113), AimFalloff: known(28.33808),
			},
		},
		"fusion rifle, Delicate Tomb after Update 9.7.0": {
			weapon: curves.Weapon{Type: "Fusion Rifle", Plugs: []uint32{2585427437}, Stats: fusion},
			want: curves.Measures{
				Reload: none(tombReload), Ready: fusionBase.Ready, Stow: fusionBase.Stow, Aim: fusionBase.Aim,
				HipFalloff: fusionBase.HipFalloff, AimFalloff: fusionBase.AimFalloff,
			},
		},
		"fusion rifle, Jötunn after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Fusion Rifle", Plugs: []uint32{1656957541}, Stats: fusion},
			want: curves.Measures{
				Reload: none(jotunnReload), Ready: fusionBase.Ready, Stow: fusionBase.Stow, Aim: fusionBase.Aim,
				HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"fusion rifle, Telesto": {
			weapon: curves.Weapon{Type: "Fusion Rifle", Plugs: []uint32{1927916065}, Stats: fusion},
			want:   with(fusionBase, none(flatDamage), none(flatDamage)),
		},
		"fusion rifle, One Thousand Voices has no reload time": {
			weapon: curves.Weapon{Type: "Fusion Rifle", Plugs: []uint32{1657056865}, Stats: fusion},
			want: curves.Measures{
				Reload: none(noReload), Ready: fusionBase.Ready, Stow: fusionBase.Stow, Aim: fusionBase.Aim,
				HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		// Update 9.7.0 changed the reload timing of every glaive.
		"glaive after Update 9.7.0": {
			weapon: curves.Weapon{Type: "Glaive", Plugs: []uint32{1316753551}, Stats: stats(50, 64, 40)},
			want: curves.Measures{
				Reload: none(glaiveReload), Ready: known(0.286779984), Stow: known(0.333728864), Aim: none(noAim),
				HipFalloff: known(17.184), AimFalloff: none(glaiveScale),
			},
		},
		"grenade launcher with special ammo": {
			weapon: curves.Weapon{Type: "Grenade Launcher", Plugs: []uint32{frameLightweight}, Stats: stats(66, 72, 0)},
			want: curves.Measures{
				Reload: known(2.128055951), Ready: known(0.354927320), Stow: known(0.319661730),
				Aim: known(0.365499440), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"grenade launcher with heavy ammo": {
			weapon: curves.Weapon{Type: "Grenade Launcher", Plugs: []uint32{frameAdaptive}, Stats: stats(66, 72, 0)},
			want: curves.Measures{
				Reload: known(2.814730828), Ready: known(0.354927320), Stow: known(0.319661730),
				Aim: known(0.365499440), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"hand cannon": {
			weapon: curves.Weapon{Type: "Hand Cannon", Plugs: []uint32{frameAdaptive}, Stats: cannon},
			want:   cannonBase,
		},
		"hand cannon, Aggressive Frame": {
			weapon: curves.Weapon{Type: "Hand Cannon", Plugs: []uint32{frameAggressive}, Stats: cannon},
			want:   with(cannonBase, known(21.96), known(32.94)),
		},
		"hand cannon, Heavy Burst": {
			weapon: curves.Weapon{Type: "Hand Cannon", Plugs: []uint32{3923638944}, Stats: cannon},
			want:   with(cannonBase, known(19.96), known(31.936)),
		},
		"hand cannon, The Last Word": {
			weapon: curves.Weapon{Type: "Hand Cannon", Plugs: []uint32{2770223582}, Stats: cannon},
			want:   with(cannonBase, known(19.3), known(21.23)),
		},
		"hand cannon, Eriana's Vow": {
			weapon: curves.Weapon{Type: "Hand Cannon", Plugs: []uint32{3174300811}, Stats: cannon},
			want:   with(cannonBase, known(38), known(91.2)),
		},
		"hand cannon, Crimson": {
			weapon: curves.Weapon{Type: "Hand Cannon", Plugs: []uint32{1030990989}, Stats: cannon},
			want:   with(cannonBase, known(19.96), known(25.948)),
		},
		"linear fusion rifle": {
			weapon: curves.Weapon{Type: "Linear Fusion Rifle", Plugs: []uint32{2057203855}, Stats: stats(30, 35, 40)},
			want: curves.Measures{
				Reload: known(2.325374213), Ready: known(0.448378828), Stow: known(0.344348202),
				Aim: known(0.351952064), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		// The source marks its scale of machine guns as a guess.
		"machine gun": {
			weapon: curves.Weapon{Type: "Machine Gun", Plugs: []uint32{frameAdaptive}, Stats: stats(58, 41, 55)},
			want: curves.Measures{
				Reload: known(4.652800376), Ready: known(0.396989399), Stow: known(0.371053004),
				Aim: known(0.406855124), HipFalloff: known(29.7265), AimFalloff: none(machineGuess),
			},
		},
		"machine gun, Xenophage": {
			weapon: curves.Weapon{Type: "Machine Gun", Plugs: []uint32{2261491232}, Stats: stats(58, 41, 55)},
			want: curves.Measures{
				Reload: known(4.652800376), Ready: known(0.396989399), Stow: known(0.371053004),
				Aim: known(0.406855124), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"machine gun, Grand Overture": {
			weapon: curves.Weapon{Type: "Machine Gun", Plugs: []uint32{2940035732}, Stats: stats(58, 41, 55)},
			want: curves.Measures{
				Reload: known(4.652800376), Ready: known(0.396989399), Stow: known(0.371053004),
				Aim: known(0.407855124), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"pulse rifle": {
			weapon: curves.Weapon{Type: "Pulse Rifle", Plugs: []uint32{frameAdaptive}, Stats: pulse},
			want:   pulseBase,
		},
		"pulse rifle, High-Impact Frame": {
			weapon: curves.Weapon{Type: "Pulse Rifle", Plugs: []uint32{frameHighImpact}, Stats: pulse},
			want:   with(pulseBase, known(21.1), known(33.76)),
		},
		"pulse rifle, Aggressive Burst": {
			weapon: curves.Weapon{Type: "Pulse Rifle", Plugs: []uint32{2874284214}, Stats: pulse},
			want:   with(pulseBase, known(21.1), known(37.98)),
		},
		// Update 9.7.0 raised the scale of the Legacy PR-55 Frame from
		// 1.6 back to 1.7.
		"pulse rifle, Legacy PR-55 Frame after Update 9.7.0": {
			weapon: curves.Weapon{Type: "Pulse Rifle", Plugs: []uint32{4172222323}, Stats: pulse},
			want:   with(pulseBase, known(21.1), known(35.87)),
		},
		"rocket launcher": {
			weapon: curves.Weapon{Type: "Rocket Launcher", Plugs: []uint32{3419274965}, Stats: stats(62, 34, 0)},
			want: curves.Measures{
				Reload: known(2.928612729), Ready: known(0.527637280), Stow: known(0.434251889),
				Aim: known(0.456249370), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"scout rifle": {
			weapon: curves.Weapon{Type: "Scout Rifle", Plugs: []uint32{framePrecision}, Stats: stats(44, 38, 57)},
			want: curves.Measures{
				Reload: known(2.098905407), Ready: known(0.432133862), Stow: known(0.415451563),
				Aim: known(0.347069573), HipFalloff: known(39.1876), AimFalloff: known(78.3752),
			},
		},
		"shotgun": {
			weapon: curves.Weapon{Type: "Shotgun", Plugs: []uint32{framePrecision}, Stats: shotgun},
			want:   shotgunBase,
		},
		// Update 9.5.0 raised the scale of Pinpoint slug shotguns from
		// 1.2 to 1.25.
		"shotgun, Pinpoint Slug Frame after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Shotgun", Plugs: []uint32{918679156}, Stats: shotgun},
			want:   with(shotgunBase, known(7.835), known(9.79375)),
		},
		// Update 9.5.0 raised the scale of Heavy Burst slug shotguns from
		// 1.2 to 1.3.
		"shotgun, Heavy Burst after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Shotgun", Plugs: []uint32{3923638944}, Stats: shotgun},
			want:   with(shotgunBase, known(7.835), known(10.1855)),
		},
		"shotgun, The Chaperone after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Shotgun", Plugs: []uint32{1394384862}, Stats: shotgun},
			want:   with(shotgunBase, known(7.835), none(chaperoneScale)),
		},
		"shotgun, Duality after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Shotgun", Plugs: []uint32{536517534}, Stats: shotgun},
			want:   with(shotgunBase, known(7.835), none(dualityScale)),
		},
		"shotgun, Lord of Wolves has no falloff data": {
			weapon: curves.Weapon{Type: "Shotgun", Plugs: []uint32{481338655}, Stats: shotgun},
			want:   with(shotgunBase, none(noFalloff), none(noFalloff)),
		},
		"sidearm": {
			weapon: curves.Weapon{Type: "Sidearm", Plugs: []uint32{frameAdaptive}, Stats: stats(24, 77, 33)},
			want: curves.Measures{
				Reload: known(1.861471964), Ready: known(0.219969780), Stow: known(0.277799450),
				Aim: known(0.171916209), HipFalloff: known(12.32), AimFalloff: known(14.784),
			},
		},
		"sidearm, Forerunner": {
			weapon: curves.Weapon{Type: "Sidearm", Plugs: []uint32{2984682260}, Stats: stats(24, 77, 33)},
			want: curves.Measures{
				Reload: known(2.361471964), Ready: known(0.219969780), Stow: known(0.277799450),
				Aim: known(0.171916209), HipFalloff: known(28.3), AimFalloff: known(56.6),
			},
		},
		"sniper rifle": {
			weapon: curves.Weapon{Type: "Sniper Rifle", Plugs: []uint32{frameAdaptive}, Stats: stats(39, 52, 61)},
			want: curves.Measures{
				Reload: known(3.037977346), Ready: known(0.371501407), Stow: known(0.330888090),
				Aim: known(0.400733354), HipFalloff: none(flatDamage), AimFalloff: none(flatDamage),
			},
		},
		"submachine gun, Precision Frame": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{framePrecision}, Stats: smg},
			want:   smgBase,
		},
		"submachine gun, Adaptive Frame": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{frameAdaptive}, Stats: smg},
			want:   with(smgBase, known(13.42), known(20.13)),
		},
		// Update 9.1.0 raised the scale of Aggressive submachine guns to
		// 1.5.
		"submachine gun, Aggressive Frame after Update 9.1.0": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{1525239159}, Stats: smg},
			want:   with(smgBase, known(13.42), known(20.13)),
		},
		// Update 9.5.0 raised the scale of Lightweight submachine guns
		// from 1.4 to 1.45.
		"submachine gun, Lightweight Frame after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{frameLightweight}, Stats: smg},
			want:   with(smgBase, known(13.42), known(19.459)),
		},
		"submachine gun, Riskrunner after Update 9.5.0": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{2516532331}, Stats: smg},
			want:   with(smgBase, known(13.42), none(exoticScale)),
		},
		"submachine gun, Osteo Striga": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{2965975126}, Stats: smg},
			want:   with(smgBase, known(15.0691), none(exoticScale)),
		},
		"trace rifle": {
			weapon: curves.Weapon{Type: "Trace Rifle", Plugs: []uint32{frameAdaptive}, Stats: stats(60, 45, 72)},
			want: curves.Measures{
				Reload: known(1.9), Ready: known(0.394151710), Stow: known(0.363352020), Aim: known(0.305375),
				HipFalloff: known(22.0784), AimFalloff: known(35.32544),
			},
		},
		// The source keys Luna's Howl by its item hash.
		"item hash of a weapon": {
			weapon: curves.Weapon{Type: "Hand Cannon", Hash: 3757612024, Plugs: []uint32{900}, Stats: cannon},
			want:   cannonBase,
		},
		"item hash of a weapon without plugs": {
			weapon: curves.Weapon{Type: "Hand Cannon", Hash: 3757612024, Stats: cannon},
			want:   cannonBase,
		},
		// The item hash 900 keys no curves, and the copy has no plug
		// that names its frame.
		"copy without plugs": {
			weapon: curves.Weapon{Type: "Auto Rifle", Hash: 900, Stats: stats(37, 70, 48)},
			want:   unknown(noPlugs),
		},
		"weapon type without curves and a copy without plugs": {
			weapon: curves.Weapon{Type: "Sword", Hash: 900, Stats: stats(37, 70, 48)},
			want:   unknown(noType),
		},
		"frame after another plug": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{900, frameAdaptive}, Stats: stats(37, 70, 48)},
			want:   adaptiveAuto,
		},
		// The plug 141515518 is an Adaptive Frame that no weapon type
		// lists, as the frame of a crafted weapon is.
		"frame of a crafted weapon": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{141515518}, Stats: stats(37, 70, 48)},
			want:   adaptiveAuto,
		},
		// The plug 210578077 is a Wave Frame or a Compressed Wave Frame.
		// The two frames share a list, and they reload at different
		// speeds.
		"frame whose hashes differ in the weapon type": {
			weapon: curves.Weapon{Type: "Grenade Launcher", Plugs: []uint32{210578077}, Stats: stats(66, 72, 0)},
			want:   unknown(noFrame),
		},
		// The plug 2874284214 is the Aggressive Burst of the pulse
		// rifles. It shares a list with the Aggressive Frame, whose scale
		// a submachine gun with the plug does not take.
		"frame that another weapon type lists": {
			weapon: curves.Weapon{Type: "Submachine Gun", Plugs: []uint32{2874284214}, Stats: smg},
			want:   unknown(noFrame),
		},
		"frame without curves": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{900}, Stats: stats(37, 70, 48)},
			want:   unknown(noFrame),
		},
		"weapon type without curves": {
			weapon: curves.Weapon{Type: "Sword", Plugs: []uint32{frameAdaptive}, Stats: stats(37, 70, 48)},
			want:   unknown(noType),
		},
		"copy without stats": {
			weapon: curves.Weapon{Type: "Auto Rifle", Plugs: []uint32{frameAdaptive}},
			want: curves.Measures{
				Reload:     none("This copy has no Reload Speed stat."),
				Ready:      none("This copy has no Handling stat."),
				Stow:       none("This copy has no Handling stat."),
				Aim:        none("This copy has no Handling stat."),
				HipFalloff: none("This copy has no Range stat."),
				AimFalloff: none("This copy has no Range stat."),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := curves.Of(tc.weapon)

			for label, pair := range map[string][2]curves.Measure{
				"reload":      {tc.want.Reload, got.Reload},
				"ready":       {tc.want.Ready, got.Ready},
				"stow":        {tc.want.Stow, got.Stow},
				"aim":         {tc.want.Aim, got.Aim},
				"hip falloff": {tc.want.HipFalloff, got.HipFalloff},
				"aim falloff": {tc.want.AimFalloff, got.AimFalloff},
			} {
				want, got := pair[0], pair[1]

				assert.Equal(t, want.Reason, got.Reason, label)
				assert.Equal(t, want.Known(), got.Known(), label)
				assert.InDelta(t, want.Value, got.Value, 1e-8, label)
			}
		})
	}
}

// unknown returns measures that all carry one reason.
func unknown(reason string) curves.Measures {
	m := none(reason)

	return curves.Measures{Reload: m, Ready: m, Stow: m, Aim: m, HipFalloff: m, AimFalloff: m}
}
