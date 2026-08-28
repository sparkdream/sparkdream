package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// The federation verifier was the one bonded role paid in DREAM alone, while
// its work costs SPARK gas to perform. These tests pin the SPARK pay that
// closed that gap -- and in particular that it pays a FLAT base for showing up
// accurately rather than a per-verification rate, because verification is
// mechanical and a per-verification rate funds rubber-stamping.

const vRole = types.RoleType_ROLE_TYPE_FEDERATION_VERIFIER

func bondVerifier(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress, bond int64, status types.BondedRoleStatus) {
	t.Helper()
	require.NoError(t, k.BondedRoles.Set(ctx,
		collections.Join(int32(vRole), addr.String()),
		types.BondedRole{
			Address:            addr.String(),
			RoleType:           vRole,
			BondStatus:         status,
			CurrentBond:        math.NewInt(bond).String(),
			TotalCommittedBond: math.ZeroInt().String(),
		}))
}

// mkVerifier bonds a NORMAL verifier and reports `verifications` verifications
// plus a window of upheld/overturned challenge outcomes, the way x/federation
// reports them.
func mkVerifier(t *testing.T, f *fixture, addr sdk.AccAddress, verifications, upheld, overturned int) {
	t.Helper()
	bondVerifier(t, f.keeper, f.ctx, addr, 500_000_000, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL)
	for i := 0; i < verifications; i++ {
		require.NoError(t, f.keeper.RecordRoleAction(f.ctx, vRole, addr.String(), types.ActionKindFederationVerify))
	}
	for i := 0; i < upheld; i++ {
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, vRole, addr.String(), types.ActionKindFederationVerify, true))
	}
	for i := 0; i < overturned; i++ {
		require.NoError(t, f.keeper.RecordRoleOutcome(f.ctx, vRole, addr.String(), types.ActionKindFederationVerify, false))
	}
}

func fundVerifierPool(f *fixture, ledger map[string]math.Int, amount math.Int) {
	ledger[keeper.VerifierRewardPoolAddress().String()] = amount
}

func verifierEpochCtx(t *testing.T, f *fixture) sdk.Context {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.VerifierRewardEpochBlocks))
	require.True(t, f.keeper.IsVerifierRewardEpoch(ctx))
	return ctx
}

// pinVerifierPayParams fixes the eligibility bars these tests assert against.
// The defaults are build-tag dependent (testparams relaxes them so E2E runs
// can clear the floor with one verification), so a test that hardcodes a bar
// has to set it rather than inherit it.
func pinVerifierPayParams(t *testing.T, f *fixture, minVerifications uint32, minAccuracy string) {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MinEpochVerifications = minVerifications
	params.MinVerifierAccuracy = math.LegacyMustNewDecFromStr(minAccuracy)
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
}

func TestVerifierPoolPaysSparkNotJustDream(t *testing.T) {
	// The whole point of the change: a verifier who does the job receives
	// SPARK, the token their gas is actually denominated in.
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-paid--------"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	mkVerifier(t, f, v, 5, 0, 0)

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, v, k.BondDenom(ctx)).Amount.IsPositive(),
		"a bonded, active, accurate verifier must earn SPARK")
}

func TestVerifierPayIsFlatNotPerVerification(t *testing.T) {
	// A verifier doing ten times the work earns the SAME base as one doing the
	// minimum. Deliberate: verification is mechanical hash-matching, so paying
	// per verification pays most for whoever submits fastest without checking.
	// Volume enters only as the min_epoch_verifications floor.
	f := initFixture(t)
	k := f.keeper

	busy := sdk.AccAddress([]byte("ver-busy--------"))
	minimal := sdk.AccAddress([]byte("ver-minimal-----"))
	mkReviewMember(t, k, f.ctx, busy, "500.0")
	mkReviewMember(t, k, f.ctx, minimal, "500.0")
	mkVerifier(t, f, busy, 30, 0, 0)
	mkVerifier(t, f, minimal, 3, 0, 0)

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))

	denom := k.BondDenom(ctx)
	busyPay := f.bankKeeper.GetBalance(ctx, busy, denom).Amount
	minimalPay := f.bankKeeper.GetBalance(ctx, minimal, denom).Amount
	require.True(t, minimalPay.IsPositive(), "both must actually be paid, or this proves nothing")
	require.Equal(t, minimalPay.String(), busyPay.String(), "volume must not buy a larger share")
}

func TestVerifierBelowWorkFloorEarnsNothing(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	idle := sdk.AccAddress([]byte("ver-idle--------"))
	mkReviewMember(t, k, f.ctx, idle, "500.0")
	pinVerifierPayParams(t, f, 3, "0.80")
	mkVerifier(t, f, idle, 1, 0, 0) // below min_epoch_verifications (3)

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, idle, k.BondDenom(ctx)).Amount.IsZero())
	require.Equal(t, math.NewInt(1_000_000), k.GetVerifierRewardPool(ctx),
		"with nobody eligible the pool is left intact")
}

func TestVerifierBelowAccuracyBarEarnsNothing(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	accurate := sdk.AccAddress([]byte("ver-accurate----"))
	sloppy := sdk.AccAddress([]byte("ver-sloppy------"))
	mkReviewMember(t, k, f.ctx, accurate, "500.0")
	mkReviewMember(t, k, f.ctx, sloppy, "500.0")
	pinVerifierPayParams(t, f, 3, "0.80")
	mkVerifier(t, f, accurate, 5, 9, 1) // 0.90, above the 0.80 bar
	mkVerifier(t, f, sloppy, 5, 1, 9)   // 0.10

	// Getting accuracy down to 0.10 takes nine overturns, which is three times
	// the demotion streak -- so sloppy leaves mkVerifier DEMOTED and the bond
	// gate would reject them before the accuracy gate is ever consulted. That
	// would make this test pass no matter what min_verifier_accuracy did. Put
	// the bond status back to NORMAL so accuracy is the only thing left that
	// can disqualify them.
	bondVerifier(t, k, f.ctx, sloppy, 500_000_000, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL)

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))

	denom := k.BondDenom(ctx)
	require.True(t, f.bankKeeper.GetBalance(ctx, accurate, denom).Amount.IsPositive())
	require.True(t, f.bankKeeper.GetBalance(ctx, sloppy, denom).Amount.IsZero(),
		"a verifier below min_verifier_accuracy earns nothing")
}

func TestContestedAccuracyEarnsABonus(t *testing.T) {
	// The flat base is equal, but a verifier whose calls were CHALLENGED and
	// UPHELD scores higher -- that is judgment somebody actually tested.
	f := initFixture(t)
	k := f.keeper

	tested := sdk.AccAddress([]byte("ver-tested------"))
	untested := sdk.AccAddress([]byte("ver-untested----"))
	mkReviewMember(t, k, f.ctx, tested, "500.0")
	mkReviewMember(t, k, f.ctx, untested, "500.0")
	mkVerifier(t, f, tested, 5, 4, 0)
	mkVerifier(t, f, untested, 5, 0, 0)

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))

	denom := k.BondDenom(ctx)
	require.True(t,
		f.bankKeeper.GetBalance(ctx, tested, denom).Amount.GT(
			f.bankKeeper.GetBalance(ctx, untested, denom).Amount),
		"upheld-under-challenge must outscore never-challenged")
	require.True(t, f.bankKeeper.GetBalance(ctx, untested, denom).Amount.IsPositive(),
		"but never-challenged still collects the flat base -- it covers gas")
}

func TestVerifierSlashedThisEpochIsNotPaid(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-slashed-----"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	mkVerifier(t, f, v, 5, 0, 0)

	ctx := verifierEpochCtx(t, f)

	// Stamp a slash in the current reward epoch, the way SlashBond does.
	ra, err := k.GetRoleActivity(ctx, vRole, v.String())
	require.NoError(t, err)
	ra.LastSlashEpoch = int64(k.CurrentVerifierRewardEpoch(ctx))
	require.NoError(t, k.RoleActivities.Set(ctx, collections.Join(int32(vRole), v.String()), ra))

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))

	require.NoError(t, k.DistributeVerifierRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, v, k.BondDenom(ctx)).Amount.IsZero(),
		"no pay in an epoch you were slashed in")
}

func TestSlashBondStampsTheSlashEpoch(t *testing.T) {
	// The stamp is what the gate above reads, and SlashBond is the only writer.
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-stamp-------"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	bondVerifier(t, k, f.ctx, v, 500_000_000, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL)
	// The bond is locked DREAM on the member, so the slash has something to
	// unlock and burn.
	require.NoError(t, k.MintDREAM(f.ctx, v, math.NewInt(500_000_000)))
	require.NoError(t, k.LockDREAM(f.ctx, v, math.NewInt(500_000_000)))

	ctx := verifierEpochCtx(t, f)
	require.NoError(t, k.SlashBond(ctx, vRole, v.String(), math.NewInt(50_000_000), "test"))

	ra, err := k.GetRoleActivity(ctx, vRole, v.String())
	require.NoError(t, err)
	require.Equal(t, int64(k.CurrentVerifierRewardEpoch(ctx)), ra.LastSlashEpoch)
}

func TestVerifierDreamStipendAutoBondsInRecovery(t *testing.T) {
	// A slashed verifier rebuilds their bond by working rather than by
	// fronting DREAM -- the reason the stipend exists alongside the SPARK.
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-recovery----"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	bondVerifier(t, k, f.ctx, v, 1_000_000, types.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY)
	for i := 0; i < 5; i++ {
		require.NoError(t, k.RecordRoleAction(f.ctx, vRole, v.String(), types.ActionKindFederationVerify))
	}
	// min_bond for the role comes from the config x/federation writes through.
	require.NoError(t, k.SetBondedRoleConfig(f.ctx, types.BondedRoleConfig{
		RoleType: vRole,
		MinBond:  math.NewInt(500_000_000).String(),
	}))

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))

	br, err := k.GetBondedRole(ctx, vRole, v.String())
	require.NoError(t, err)
	bond, _ := math.NewIntFromString(br.CurrentBond)
	require.True(t, bond.GT(math.NewInt(1_000_000)),
		"the stipend must be re-locked into a RECOVERY bond")

	// SPARK, unlike DREAM, is paid straight out even in RECOVERY: it
	// reimburses gas already spent.
	require.True(t, f.bankKeeper.GetBalance(ctx, v, k.BondDenom(ctx)).Amount.IsPositive())
}

func TestDemotedVerifierEarnsNothing(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-demoted-----"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	bondVerifier(t, k, f.ctx, v, 500_000_000, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED)
	for i := 0; i < 5; i++ {
		require.NoError(t, k.RecordRoleAction(f.ctx, vRole, v.String(), types.ActionKindFederationVerify))
	}

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))
	ctx := verifierEpochCtx(t, f)

	require.NoError(t, k.DistributeVerifierRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, v, k.BondDenom(ctx)).Amount.IsZero())
}

func TestVerifierPoolIsSeparateFromTheOtherRolePools(t *testing.T) {
	// Four separate pools so no role can draw on another's funds or be
	// retuned by another's accuracy bar.
	f := initFixture(t)
	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(750_000))

	require.Equal(t, math.NewInt(750_000), f.keeper.GetVerifierRewardPool(f.ctx))
	require.True(t, f.keeper.GetSentinelRewardPool(f.ctx).IsZero())
	require.True(t, f.keeper.GetReviewerRewardPool(f.ctx).IsZero())
	require.True(t, f.keeper.GetCuratorRewardPool(f.ctx).IsZero())
}

func TestVerifierPoolOverflowIsBurned(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	poolCap := params.MaxVerifierRewardPool
	ledger := installLedger(f)
	fundVerifierPool(f, ledger, poolCap.MulRaw(2))

	require.NoError(t, k.BurnVerifierRewardPoolOverflow(f.ctx))

	expected := poolCap.MulRaw(2).Sub(poolCap.Quo(math.NewInt(2)))
	require.Equal(t, expected.String(), k.GetVerifierRewardPool(f.ctx).String())
}

func TestVerifierAccuracyRingUsesTheVerifierCadence(t *testing.T) {
	// The ring is stamped when a challenge resolves and read as a window at
	// distribution time, so both must be in verifier-epoch units. The four
	// role cadences are independently editable, and the verifier's is
	// deliberately the longest.
	f := initFixture(t)
	k := f.keeper

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	require.NotEqual(t, params.SentinelRewardEpochBlocks, params.VerifierRewardEpochBlocks,
		"this test is meaningless if the two cadences coincide")

	v := sdk.AccAddress([]byte("ver-ring--------"))
	ctx := f.ctx.WithBlockHeight(int64(params.VerifierRewardEpochBlocks) * 3)
	require.NoError(t, k.RecordRoleOutcome(ctx, vRole, v.String(), types.ActionKindFederationVerify, true))

	up, ov := k.GetRoleWindowedAccuracy(ctx, vRole, v.String(),
		k.CurrentVerifierRewardEpoch(ctx), params.VerifierAccuracyWindowEpochs)
	require.Equal(t, uint64(1), up)
	require.Zero(t, ov)
}

func TestVerifierSlashedDuringTheEpochWindowIsNotPaid(t *testing.T) {
	// The realistic shape of a slash, and the one the gate used to miss.
	//
	// Distribution runs at height N*epoch_blocks, so it labels itself epoch N.
	// But the counters it is paying for accrued over heights
	// [(N-1)*epoch_blocks, N*epoch_blocks) -- every one of which stamps N-1.
	// Matching last_slash_epoch on == N therefore only ever caught a slash
	// landing in the boundary block itself (which
	// TestVerifierSlashedThisEpochIsNotPaid covers) and let an entire epoch's
	// worth of slashes collect pay.
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-midwindow---"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	mkVerifier(t, f, v, 5, 0, 0)

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	// Epoch 2's boundary, so the closed epoch is 1 rather than 0 -- epoch 0 is
	// indistinguishable from "never slashed" under the int64 sentinel.
	ctx := f.ctx.WithBlockHeight(int64(params.VerifierRewardEpochBlocks) * 2)
	require.True(t, k.IsVerifierRewardEpoch(ctx))
	require.Equal(t, uint64(2), k.CurrentVerifierRewardEpoch(ctx))

	ra, err := k.GetRoleActivity(ctx, vRole, v.String())
	require.NoError(t, err)
	ra.LastSlashEpoch = 1 // slashed mid-window, not on the boundary block
	require.NoError(t, k.RoleActivities.Set(ctx, collections.Join(int32(vRole), v.String()), ra))

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))

	require.NoError(t, k.DistributeVerifierRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, v, k.BondDenom(ctx)).Amount.IsZero(),
		"a slash anywhere in the window being paid for disqualifies, not just one on the boundary block")
}

func TestVerifierUnslashedInTheWindowIsStillPaid(t *testing.T) {
	// Guards the widened gate from swallowing everyone: a slash two epochs
	// back is outside the window and must not block this epoch's pay.
	f := initFixture(t)
	k := f.keeper

	v := sdk.AccAddress([]byte("ver-oldslash----"))
	mkReviewMember(t, k, f.ctx, v, "500.0")
	mkVerifier(t, f, v, 5, 0, 0)

	params, err := k.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(int64(params.VerifierRewardEpochBlocks) * 4)
	require.Equal(t, uint64(4), k.CurrentVerifierRewardEpoch(ctx))

	ra, err := k.GetRoleActivity(ctx, vRole, v.String())
	require.NoError(t, err)
	ra.LastSlashEpoch = 2 // two epochs back -- already served
	require.NoError(t, k.RoleActivities.Set(ctx, collections.Join(int32(vRole), v.String()), ra))

	ledger := installLedger(f)
	fundVerifierPool(f, ledger, math.NewInt(1_000_000))

	require.NoError(t, k.DistributeVerifierRewards(ctx))
	require.True(t, f.bankKeeper.GetBalance(ctx, v, k.BondDenom(ctx)).Amount.IsPositive(),
		"a slash outside the window must not disqualify")
}
