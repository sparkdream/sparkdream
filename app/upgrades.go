package app

import (
	"fmt"

	upgradetypes "cosmossdk.io/x/upgrade/types"

	"sparkdream/app/upgrades"
)

// registerUpgrades registers a handler for every named upgrade, and, when the
// node is starting at the height of one that changes module stores, the store
// loader that applies those changes. Must run before app.Load.
func (app *App) registerUpgrades() {
	seen := make(map[string]bool, len(upgrades.All))
	for _, u := range upgrades.All {
		if seen[u.Name] {
			panic(fmt.Sprintf("upgrade %q is listed twice", u.Name))
		}
		seen[u.Name] = true
		app.UpgradeKeeper.SetUpgradeHandler(u.Name, u.Handler(app.ModuleManager, app.Configurator()))
	}

	// x/upgrade writes upgrade-info.json when it halts the chain for a plan;
	// the new binary reads it here to apply that plan's store changes before
	// the stores load.
	info, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("read upgrade info from disk: %v", err))
	}
	if info.Name == "" || app.UpgradeKeeper.IsSkipHeight(info.Height) {
		return
	}
	for _, u := range upgrades.All {
		if u.Name != info.Name {
			continue
		}
		if len(u.StoreUpgrades.Added)+len(u.StoreUpgrades.Renamed)+len(u.StoreUpgrades.Deleted) > 0 {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(info.Height, &u.StoreUpgrades))
		}
		return
	}
}
