package keeper

import "sparkdream/x/futarchy/types"

// SetHooks sets the hooks for the futarchy module.
// Note: It must be a pointer receiver to update the struct.
func (k *Keeper) SetHooks(hooks types.FutarchyHooks) {
	if k.Hooks != nil {
		panic("FutarchyHooks already set")
	}
	k.Hooks = hooks
}
