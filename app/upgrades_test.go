package app

import (
	"testing"

	"cosmossdk.io/core/header"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"sparkdream/app/upgrades"
)

// A plan whose name has no handler halts every node at its height with
// "UPGRADE NEEDED" and nothing to run, so each listed upgrade must reach the
// upgrade keeper.
func TestEveryUpgradeHasAHandler(t *testing.T) {
	app := testApp(t)
	for _, u := range upgrades.All {
		require.True(t, app.UpgradeKeeper.HasHandler(u.Name), u.Name)
	}
}

// Applying each plan on a chain whose module versions are current runs its
// handler to completion and records it as done at the plan height.
func TestUpgradesApply(t *testing.T) {
	for _, u := range upgrades.All {
		t.Run(u.Name, func(t *testing.T) {
			app := testApp(t)
			// x/upgrade records the done height from the header info
			ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 10}).
				WithHeaderInfo(header.Info{Height: 10})
			require.NoError(t, app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()))

			plan := upgradetypes.Plan{Name: u.Name, Height: 10}
			require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, plan))

			done, err := app.UpgradeKeeper.GetDoneHeight(ctx, u.Name)
			require.NoError(t, err)
			require.Equal(t, int64(10), done)
		})
	}
}

// The machinery works before any upgrade is listed: a plan whose handler is
// RunMigrations applies on a chain at current module versions and records
// its done height.
func TestRunMigrationsHandlerApplies(t *testing.T) {
	app := testApp(t)
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 10}).
		WithHeaderInfo(header.Info{Height: 10})
	require.NoError(t, app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap()))
	app.UpgradeKeeper.SetUpgradeHandler("phoenix-test", upgrades.RunMigrations(app.ModuleManager, app.Configurator()))

	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "phoenix-test", Height: 10}))
	done, err := app.UpgradeKeeper.GetDoneHeight(ctx, "phoenix-test")
	require.NoError(t, err)
	require.Equal(t, int64(10), done)
}
