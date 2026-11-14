package bungietest

import (
	"context"
	"errors"
	"fmt"

	"go.jacobcolvin.com/dotfiles/tools/mcp-destiny/bungie"
)

// ErrStep is the error that a [StepMover] returns for the action that its
// Fail field names.
var ErrStep = errors.New("step refused")

// StepMover records the Bungie item actions and loadout actions of a test
// as short strings. It refuses the action that Fail names.
type StepMover struct {
	// Fail names the action that the mover refuses with [ErrStep]. The
	// mover leaves a refused action out of Steps.
	Fail string
	// Steps holds the name of each action that the mover accepted, in
	// order.
	Steps []string
}

// step records the action called name, or refuses it with [ErrStep] when
// Fail names it.
func (m *StepMover) step(name string) error {
	if name == m.Fail {
		return ErrStep
	}

	m.Steps = append(m.Steps, name)

	return nil
}

// Transfer records a transfer between a character and the vault as
// "to vault from <character>" or "from vault to <character>". The step of
// a stack ends in the stack size, as in "to vault from c1 x10".
func (m *StepMover) Transfer(_ context.Context, action bungie.ItemAction, toVault bool) error {
	name := "from vault to " + action.CharacterID
	if toVault {
		name = "to vault from " + action.CharacterID
	}

	if action.InstanceID == bungie.StackInstanceID {
		name += fmt.Sprintf(" x%d", action.StackSize)
	}

	return m.step(name)
}

// Equip records an equip as "equip <instance> on <character>".
func (m *StepMover) Equip(_ context.Context, action bungie.ItemAction) error {
	return m.step("equip " + action.InstanceID + " on " + action.CharacterID)
}

// PullFromPostmaster records a pull from the Postmaster as
// "pull to <character>".
func (m *StepMover) PullFromPostmaster(_ context.Context, action bungie.ItemAction) error {
	return m.step("pull to " + action.CharacterID)
}

// SetLockState records a lock change as "lock <instance> on <character>"
// or "unlock <instance> on <character>".
func (m *StepMover) SetLockState(_ context.Context, action bungie.ItemAction, locked bool) error {
	name := "unlock "
	if locked {
		name = "lock "
	}

	return m.step(name + action.InstanceID + " on " + action.CharacterID)
}

// InsertSocketPlugFree records a perk change as
// "insert <plug> into socket <socket> of <instance> on <character>".
func (m *StepMover) InsertSocketPlugFree(
	_ context.Context,
	action bungie.ItemAction,
	socket int,
	plugHash uint32,
) error {
	return m.step(
		fmt.Sprintf("insert %d into socket %d of %s on %s", plugHash, socket, action.InstanceID, action.CharacterID),
	)
}

// EquipLoadout records a loadout equip as
// "equip loadout <index> on <character>".
func (m *StepMover) EquipLoadout(_ context.Context, action bungie.LoadoutAction) error {
	return m.step(fmt.Sprintf("equip loadout %d on %s", action.Index, action.CharacterID))
}
