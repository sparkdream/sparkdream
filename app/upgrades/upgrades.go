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
var All = []Upgrade{
	// x/blog v2: builds the pinned post index behind Query/ListPinnedPosts.
	// x/session v2: admits the x/rep work and staking messages to the session
	// allowlist (sessiontypes.RepWorkMsgTypes), whose ceiling only an upgrade
	// can raise. Not yet applied by any network, so later consensus changes of
	// the same release fold in here.
	//
	// x/storage: a new module (its store is added here and its InitGenesis
	// runs with the default genesis: params and the arweave backend).
	// x/service v2 writes the storage-arweave service type, x/session v2
	// also admits the storage worker messages, and x/commons v3 gives the
	// Commons Council and its Operations Committee the storage council
	// messages (docs/x-storage-spec.md, "Genesis, module account and upgrade").
	//
	// x/commons v3 also gives the Technical and Ecosystem Councils (standard
	// and veto policies) the messages to hold their Commons Supervisory
	// Board seats, and the release carries the veto-seat and golden-share
	// recusal rules (x/commons/keeper/veto_seat.go; no stored state).
	//
	// x/rep: Params.Validate now requires council_migration_delay to be
	// positive and at least recovery_migration_delay. Validation only, no
	// migration: stored params are untouched, so a network keeps its
	// council delay until governance changes it (the new 7-day default, 2
	// days on testnet and 1 on devnet, reaches only a fresh genesis).
	{
		Name:          "storage-pinned-index-rep-sessions",
		Handler:       RunMigrations,
		StoreUpgrades: storetypes.StoreUpgrades{Added: []string{"storage"}},
	},
}
