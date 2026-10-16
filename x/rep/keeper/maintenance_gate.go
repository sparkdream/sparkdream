package keeper

import (
	"context"

	"sparkdream/x/rep/types"
)

// assertNotInMaintenance refuses reputation-moving messages while x/season is
// archiving or resetting reputation and XP (x-season-spec "Maintenance
// Mode"): stakes, initiative creation/completion and challenges all read or
// write per-tag reputation that the transition is rewriting. Tips, gifts,
// governance and queries stay open. With no season keeper wired (standalone
// tests) nothing is ever in maintenance.
func (k Keeper) assertNotInMaintenance(ctx context.Context) error {
	if k.InMaintenanceMode(ctx) {
		return types.ErrMaintenanceMode
	}
	return nil
}

// InMaintenanceMode reports whether x/season is in a blocking transition
// phase. Exported so simulation ops can precheck the gate instead of
// delivering a message the handler will refuse.
func (k Keeper) InMaintenanceMode(ctx context.Context) bool {
	return k.late.seasonKeeper != nil && k.late.seasonKeeper.IsInMaintenanceMode(ctx)
}
