package app

import (
	"strings"
	"testing"

	"cosmossdk.io/core/header"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/app/upgrades"
	blogkeeper "sparkdream/x/blog/keeper"
	blogtypes "sparkdream/x/blog/types"
	commonskeeper "sparkdream/x/commons/keeper"
	commonstypes "sparkdream/x/commons/types"
	servicetypes "sparkdream/x/service/types"
	sessiontypes "sparkdream/x/session/types"
	storagekeeper "sparkdream/x/storage/keeper"
	storagetypes "sparkdream/x/storage/types"
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

// storage-pinned-index-rep-sessions on a chain still at x/blog v1 runs the v1 -> v2
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
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "storage-pinned-index-rep-sessions", Height: 10}))

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
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "storage-pinned-index-rep-sessions", Height: 10}))

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

// The upgrade on a chain that predates x/storage brings the module up whole:
// its genesis runs (params and the arweave backend), x/service v2 writes the
// storage-arweave service type, x/session v2 admits the worker messages, and
// x/commons v3 gives the Commons Council and its Operations Committee the
// storage council messages. After it, x/storage's own cross-module check
// (the hold windows fit inside the operator unbonding period) holds.
func TestUpgradeBringsUpStorage(t *testing.T) {
	app := testApp(t)
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 10}).
		WithHeaderInfo(header.Info{Height: 10})

	// The state of a chain that predates the module (the test app starts
	// empty): no storage state, x/service without the storage-arweave type,
	// a session allowlist without the worker messages, and the two commons
	// bodies holding only what they held before.
	_, err := app.StorageKeeper.Params.Get(ctx)
	require.Error(t, err, "storage must have no state before the upgrade")
	require.NoError(t, app.ServiceKeeper.Params.Set(ctx, servicetypes.DefaultParams()))
	_, found := app.ServiceKeeper.GetServiceTypeConfig(ctx, servicetypes.StorageArweaveServiceType)
	require.False(t, found)

	isStorage := func(m string) bool { return strings.HasPrefix(m, "/sparkdream.storage.v1.") }
	sp := sessiontypes.DefaultParams()
	strip := func(in []string) (out []string) {
		for _, m := range in {
			if !isStorage(m) {
				out = append(out, m)
			}
		}
		return out
	}
	sp.MaxAllowedMsgTypes, sp.AllowedMsgTypes = strip(sp.MaxAllowedMsgTypes), strip(sp.AllowedMsgTypes)
	require.NoError(t, sp.Validate())
	require.NoError(t, app.SessionKeeper.Params.Set(ctx, sp))

	bodies := map[string]string{
		"Commons Council":              "sprkdrm1afyuna8gqe55t7jztxcg0aleg0k5txep72pfan",
		"Commons Operations Committee": "sprkdrm1g5ad4qmzqpfkfzgktx6za005qt2t0v56jy529y",
	}
	const held = "/sparkdream.commons.v1.MsgSpendFromCommons"
	for name, policy := range bodies {
		require.NoError(t, app.CommonsKeeper.Groups.Set(ctx, name, commonstypes.Group{PolicyAddress: policy}))
		require.NoError(t, app.CommonsKeeper.PolicyPermissions.Set(ctx, policy,
			commonstypes.PolicyPermissions{PolicyAddress: policy, AllowedMessages: []string{held}}))
	}

	vm := app.ModuleManager.GetVersionMap()
	delete(vm, storagetypes.ModuleName)
	vm[servicetypes.ModuleName] = 1
	vm[sessiontypes.ModuleName] = 1
	vm[commonstypes.ModuleName] = 2
	require.NoError(t, app.UpgradeKeeper.SetModuleVersionMap(ctx, vm))
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "storage-pinned-index-rep-sessions", Height: 10}))

	// x/storage: default params and the launch backend, nobody approved.
	params, err := app.StorageKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, storagetypes.DefaultParams(), params)
	backend, err := app.StorageKeeper.Backends.Get(ctx, storagetypes.ArweaveBackend)
	require.NoError(t, err)
	require.Equal(t, servicetypes.StorageArweaveServiceType, backend.ServiceType)
	exported, err := app.StorageKeeper.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Empty(t, exported.ApprovedOperators)
	require.Empty(t, exported.ApprovedScanners)

	// x/service: the operator type, with an unbonding period the storage
	// windows fit inside.
	period, ok := app.ServiceKeeper.UnbondingPeriodBlocks(ctx, servicetypes.StorageArweaveServiceType)
	require.True(t, ok)
	require.Greater(t, period, params.HoldSpan())

	// x/session: the five worker messages, and only those.
	sp, err = app.SessionKeeper.Params.Get(ctx)
	require.NoError(t, err)
	require.Subset(t, sp.MaxAllowedMsgTypes, sessiontypes.StorageWorkerMsgTypes)
	require.Subset(t, sp.AllowedMsgTypes, sessiontypes.StorageWorkerMsgTypes)
	require.ElementsMatch(t, sessiontypes.DefaultAllowedMsgTypes, sp.MaxAllowedMsgTypes)

	// x/commons: the council messages for both bodies, prices for the committee.
	for name, policy := range bodies {
		perms, err := app.CommonsKeeper.PolicyPermissions.Get(ctx, policy)
		require.NoError(t, err)
		require.Subset(t, perms.AllowedMessages, storagetypes.CouncilMsgTypeURLs(), name)
		require.Contains(t, perms.AllowedMessages, held, name)
	}
	ops, err := app.CommonsKeeper.PolicyPermissions.Get(ctx, bodies["Commons Operations Committee"])
	require.NoError(t, err)
	require.Contains(t, ops.AllowedMessages, "/sparkdream.storage.v1.MsgUpdateOperationalParams")
	council, err := app.CommonsKeeper.PolicyPermissions.Get(ctx, bodies["Commons Council"])
	require.NoError(t, err)
	require.NotContains(t, council.AllowedMessages, "/sparkdream.storage.v1.MsgUpdateOperationalParams")

	after, err := app.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), after[storagetypes.ModuleName])
	require.Equal(t, uint64(2), after[servicetypes.ModuleName])
	require.Equal(t, uint64(2), after[sessiontypes.ModuleName])
	// v2 -> v3 and v3 -> v4 (treasury staking) run in the same pass.
	require.Equal(t, uint64(4), after[commonstypes.ModuleName])

	// The index and flag invariants hold on the upgraded state. (The escrow
	// invariant reads the bond denom from x/identity, which this empty test
	// app has not initialised; TestInvariants covers it on real genesis files.)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, inv := range []sdk.Invariant{storagekeeper.IndexInvariant(app.StorageKeeper), storagekeeper.FlagInvariant(app.StorageKeeper)} {
		msg, broken := inv(sdkCtx)
		require.False(t, broken, msg)
	}

	// The entry adds the module's store at the upgrade height.
	for _, u := range upgrades.All {
		if u.Name == "storage-pinned-index-rep-sessions" {
			require.Contains(t, u.StoreUpgrades.Added, storagetypes.StoreKey)
		}
	}
}

// The upgrade on a chain at x/commons v3 runs the v3 -> v4
// migration: every genesis body with a treasury gains the treasury staking
// messages, and a body without one does not.
func TestUpgradeGrantsTreasuryStakingMsgs(t *testing.T) {
	app := testApp(t)
	ctx := app.NewUncachedContext(false, cmtproto.Header{Height: 10}).
		WithHeaderInfo(header.Info{Height: 10})

	// The bodies as a v3 chain holds them: no treasury staking messages.
	const held = "/sparkdream.commons.v1.MsgSpendFromCommons"
	policyOf := func(name string) string { return authtypes.NewModuleAddress(name).String() }
	const hr = "Commons Governance Committee"
	for _, name := range append([]string{hr}, commonskeeper.TreasuryStakingGroups...) {
		policy := policyOf(name)
		require.NoError(t, app.CommonsKeeper.Groups.Set(ctx, name, commonstypes.Group{PolicyAddress: policy}))
		require.NoError(t, app.CommonsKeeper.PolicyPermissions.Set(ctx, policy,
			commonstypes.PolicyPermissions{PolicyAddress: policy, AllowedMessages: []string{held}}))
	}

	// One body was granted the raw staking message before it was forbidden.
	const raw = "/cosmos.staking.v1beta1.MsgDelegate"
	rawHolder := policyOf(commonskeeper.TreasuryStakingGroups[0])
	require.NoError(t, app.CommonsKeeper.PolicyPermissions.Set(ctx, rawHolder,
		commonstypes.PolicyPermissions{PolicyAddress: rawHolder, AllowedMessages: []string{held, raw}}))

	vm := app.ModuleManager.GetVersionMap()
	vm[commonstypes.ModuleName] = 3
	require.NoError(t, app.UpgradeKeeper.SetModuleVersionMap(ctx, vm))
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: "storage-pinned-index-rep-sessions", Height: 10}))

	for _, name := range commonskeeper.TreasuryStakingGroups {
		perms, err := app.CommonsKeeper.PolicyPermissions.Get(ctx, policyOf(name))
		require.NoError(t, err, name)
		require.Contains(t, perms.AllowedMessages, held, name)
		require.Subset(t, perms.AllowedMessages, commonskeeper.TreasuryStakingMsgs, name)
		require.NotContains(t, perms.AllowedMessages, raw, name)
	}
	perms, err := app.CommonsKeeper.PolicyPermissions.Get(ctx, policyOf(hr))
	require.NoError(t, err)
	require.Equal(t, []string{held}, perms.AllowedMessages)

	after, err := app.UpgradeKeeper.GetModuleVersionMap(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(4), after[commonstypes.ModuleName])
}
