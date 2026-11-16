package app

import (
	"testing"

	"cosmossdk.io/core/header"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	"sparkdream/app/upgrades"
	blogkeeper "sparkdream/x/blog/keeper"
	blogtypes "sparkdream/x/blog/types"
	sessiontypes "sparkdream/x/session/types"
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

// pinned-index-rep-sessions on a chain still at x/blog v1 runs the v1 -> v2
// migration: a post pinned before the index existed becomes listed.
func TestBlogPinnedIndexUpgradeBackfills(t *testing.T) {
	app := testApp(t)
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 10}).
		WithHeaderInfo(header.Info{Height: 10})

	id := app.BlogKeeper.AppendPost(ctx, blogtypes.Post{
		Creator: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan", Title: "Pinned", Body: "body",
		Status:   blogtypes.PostStatus_POST_STATUS_ACTIVE,
		PinnedBy: "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan", PinnedAt: 100,
	})
	_, broken := blogkeeper.PinnedIndexInvariant(app.BlogKeeper)(ctx)
	require.True(t, broken)

	vm := app.ModuleManager.GetVersionMap()
	vm[blogtypes.ModuleName] = 1
	require.NoError(t, app.UpgradeKeeper.SetModuleVersionMap(ctx, vm))
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "pinned-index-rep-sessions", Height: 10}))

	msg, broken := blogkeeper.PinnedIndexInvariant(app.BlogKeeper)(ctx)
	require.False(t, broken, msg)
	resp, err := blogkeeper.NewQueryServerImpl(app.BlogKeeper).ListPinnedPosts(ctx, &blogtypes.QueryListPinnedPostsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Posts, 1)
	require.Equal(t, id, resp.Posts[0].Id)

	after, err := app.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), after[blogtypes.ModuleName])
}

// The upgrade on a chain still at x/session v1 runs the v1 -> v2 migration:
// the x/rep work and staking messages join the ceiling and the active list,
// and a list the network narrowed keeps its other removals.
func TestUpgradeAdmitsRepWorkSessionMsgs(t *testing.T) {
	app := testApp(t)
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 10}).
		WithHeaderInfo(header.Info{Height: 10})

	// The lists as a v1 chain holds them, with one entry narrowed away.
	const narrowed = "/sparkdream.blog.v1.MsgReact"
	isRepWork := map[string]bool{}
	for _, m := range sessiontypes.RepWorkMsgTypes {
		isRepWork[m] = true
	}
	params := sessiontypes.DefaultParams()
	var ceiling, active []string
	for _, m := range params.MaxAllowedMsgTypes {
		if !isRepWork[m] {
			ceiling = append(ceiling, m)
			if m != narrowed {
				active = append(active, m)
			}
		}
	}
	params.MaxAllowedMsgTypes, params.AllowedMsgTypes = ceiling, active
	require.NoError(t, params.Validate())
	require.NoError(t, app.SessionKeeper.Params.Set(ctx, params))

	vm := app.ModuleManager.GetVersionMap()
	vm[sessiontypes.ModuleName] = 1
	require.NoError(t, app.UpgradeKeeper.SetModuleVersionMap(ctx, vm))
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "pinned-index-rep-sessions", Height: 10}))

	after, err := app.SessionKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, sessiontypes.DefaultAllowedMsgTypes, after.MaxAllowedMsgTypes)
	require.Subset(t, after.AllowedMsgTypes, sessiontypes.RepWorkMsgTypes)
	require.NotContains(t, after.AllowedMsgTypes, narrowed)
	require.Len(t, after.AllowedMsgTypes, len(sessiontypes.DefaultAllowedMsgTypes)-1)

	versions, err := app.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(2), versions[sessiontypes.ModuleName])
}
