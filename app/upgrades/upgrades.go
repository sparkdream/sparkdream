// Package upgrades lists the chain's named software upgrades. Each entry is
// what an x/upgrade plan of the same name runs when the chain reaches the
// plan's height on the new binary (docs/chain-upgrades.md).
//
// A release adds at most one entry: x/upgrade panics on a binary that already
// holds the handler of a plan still pending, so every consensus change in a
// release folds into that release's one upgrade until a network applies it.
// Entries stay listed after they are applied, so a node replaying history
// through the upgrade height on this binary still finds its handler.
package upgrades

import (
	"context"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// Upgrade is one named upgrade: the handler x/upgrade runs at the plan
// height, and the stores the upgrade adds, renames or deletes.
type Upgrade struct {
	Name string
	// StoreUpgrades: stores added, renamed or deleted at the upgrade height;
	// empty when no module store changes.
	StoreUpgrades storetypes.StoreUpgrades
	// Handler builds the upgrade handler from the app's module manager.
	Handler func(mm *module.Manager, cfg module.Configurator) upgradetypes.UpgradeHandler
}

// RunMigrations is the handler for an upgrade whose state changes, if any,
// are module migrations registered under a bumped ConsensusVersion.
func RunMigrations(mm *module.Manager, cfg module.Configurator) upgradetypes.UpgradeHandler {
	return func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, cfg, fromVM)
	}
}

// All is every upgrade this binary can apply, oldest first.
//
// Empty while the networks are still reset for consensus-breaking releases
// (docs/chain-upgrades.md); the first entry lands with the first release a
// running network upgrades to instead.
var All = []Upgrade{}
