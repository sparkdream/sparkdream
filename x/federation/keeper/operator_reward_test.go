package keeper_test

import (
	"crypto/sha256"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// Bridge operators were pure cost before this existed: a SPARK bond on
// x/service, gas per submission, and nothing coming back — while the verifier
// on the other side of the same exchange is paid. These tests pin the pay, and
// in particular that it flows for content an INDEPENDENT party verified rather
// than for raw submission volume.

const testDenom = "uspark"

// seedBinding writes a BridgeBinding directly with the given epoch counters,
// standing in for a full epoch of submit/verify/reject traffic.
func seedRewardBinding(t *testing.T, f *fixture, addr, peerID string, submitted, verified, rejected, unverified uint64) collections.Pair[string, string] {
	t.Helper()
	key := collections.Join(addr, peerID)
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, key, types.BridgeBinding{
		Address:           addr,
		PeerId:            peerID,
		Protocol:          "activitypub",
		EpochSubmitted:    submitted,
		EpochVerified:     verified,
		EpochRejected:     rejected,
		EpochUnverified:   unverified,
		CumulativeRewards: math.ZeroInt(),
	}))
	return key
}

func fundOperatorPool(f *fixture, amount int64) {
	f.bankKeeper.balances[keeper.OperatorRewardPoolAddress().String()] =
		sdk.NewCoins(sdk.NewCoin(testDenom, math.NewInt(amount)))
}

func operatorBalance(f *fixture, addr string) math.Int {
	return f.bankKeeper.balances[addr].AmountOf(testDenom)
}

// operatorEpochCtx returns a context at an operator reward-epoch boundary.
func operatorEpochCtx(t *testing.T, f *fixture) sdk.Context {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(int64(params.OperatorRewardEpochBlocks))
	require.True(t, f.keeper.IsOperatorRewardEpoch(ctx))
	return ctx
}

func TestOperatorRewardPaysForVerifiedSubmissions(t *testing.T) {
	f := initFixture(t)
	addr := testAddr(t, f, "op-paid")
	seedRewardBinding(t, f, addr, "peer-a", 10, 10, 0, 0)
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))

	require.True(t, operatorBalance(f, addr).IsPositive(),
		"an active operator with verified submissions must be paid SPARK")
}

func TestOperatorRewardIsProportionalToVerifiedCount(t *testing.T) {
	// Volume weighting is safe HERE, unlike for verifiers, because the count
	// was confirmed by an independent verifier — the operator cannot
	// unilaterally manufacture it.
	f := initFixture(t)
	big := testAddr(t, f, "op-big")
	small := testAddr(t, f, "op-small")
	seedRewardBinding(t, f, big, "peer-a", 30, 30, 0, 0)
	seedRewardBinding(t, f, small, "peer-b", 10, 10, 0, 0)
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))

	bigPay := operatorBalance(f, big)
	smallPay := operatorBalance(f, small)
	require.True(t, smallPay.IsPositive())
	require.Equal(t, "750000", bigPay.String(), "3:1 verified split of a 1,000,000 pool")
	require.Equal(t, "250000", smallPay.String())
}

func TestOperatorRejectedThisEpochEarnsNothing(t *testing.T) {
	// A challenge upheld against the operator's content means they bridged
	// something false. That must cost them the epoch.
	f := initFixture(t)
	clean := testAddr(t, f, "op-clean")
	caught := testAddr(t, f, "op-caught")
	seedRewardBinding(t, f, clean, "peer-a", 10, 10, 0, 0)
	seedRewardBinding(t, f, caught, "peer-b", 10, 10, 1, 0)
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))

	require.True(t, operatorBalance(f, clean).IsPositive())
	require.True(t, operatorBalance(f, caught).IsZero(),
		"a rejected submission disqualifies the operator for the epoch")
}

func TestOperatorSuspendedEarnsNothing(t *testing.T) {
	// AfterOperatorUnderfunded sets suspended when a slash drops the bond
	// below min_bond: a stake that no longer backs the submissions earns
	// nothing.
	f := initFixture(t)
	addr := testAddr(t, f, "op-suspended")
	key := seedRewardBinding(t, f, addr, "peer-a", 10, 10, 0, 0)
	b, err := f.keeper.BridgeBindings.Get(f.ctx, key)
	require.NoError(t, err)
	b.Suspended = true
	require.NoError(t, f.keeper.BridgeBindings.Set(f.ctx, key, b))
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))
	require.True(t, operatorBalance(f, addr).IsZero())
}

func TestOperatorBelowVerifiedFloorEarnsNothing(t *testing.T) {
	f := initFixture(t)
	addr := testAddr(t, f, "op-unverified-only")
	// Plenty submitted, nothing independently verified.
	seedRewardBinding(t, f, addr, "peer-a", 50, 0, 0, 50)
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))
	require.True(t, operatorBalance(f, addr).IsZero(),
		"submission volume alone must not pay — verification is the gate")
	require.Equal(t, math.NewInt(1_000_000), f.keeper.GetOperatorRewardPool(ctx),
		"with nobody eligible the pool is left intact")
}

func TestOperatorAboveUnverifiedRateEarnsNothing(t *testing.T) {
	// Flooding the queue with content no verifier confirms spends verifier
	// attention rather than producing value.
	f := initFixture(t)
	flooder := testAddr(t, f, "op-flooder")
	tidy := testAddr(t, f, "op-tidy")
	seedRewardBinding(t, f, flooder, "peer-a", 100, 5, 0, 95) // 0.95 unverified
	seedRewardBinding(t, f, tidy, "peer-b", 10, 9, 0, 1)      // 0.10 unverified
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))

	require.True(t, operatorBalance(f, flooder).IsZero(),
		"unverified rate above max_unverified_rate disqualifies")
	require.True(t, operatorBalance(f, tidy).IsPositive())
}

func TestOperatorEpochCountersResetForEveryone(t *testing.T) {
	// Including the ineligible, or stale epoch activity leaks into the next
	// window and an operator gets paid twice for the same work.
	f := initFixture(t)
	paid := testAddr(t, f, "op-reset-paid")
	skipped := testAddr(t, f, "op-reset-skipped")
	paidKey := seedRewardBinding(t, f, paid, "peer-a", 10, 10, 0, 0)
	skippedKey := seedRewardBinding(t, f, skipped, "peer-b", 10, 10, 2, 0) // rejected → ineligible
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))

	for _, key := range []collections.Pair[string, string]{paidKey, skippedKey} {
		b, err := f.keeper.BridgeBindings.Get(ctx, key)
		require.NoError(t, err)
		require.Zero(t, b.EpochSubmitted)
		require.Zero(t, b.EpochVerified)
		require.Zero(t, b.EpochRejected)
		require.Zero(t, b.EpochUnverified)
	}
}

func TestOperatorRewardRecordsBookkeeping(t *testing.T) {
	f := initFixture(t)
	addr := testAddr(t, f, "op-books")
	key := seedRewardBinding(t, f, addr, "peer-a", 10, 10, 0, 0)
	fundOperatorPool(f, 1_000_000)

	ctx := operatorEpochCtx(t, f)
	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))

	b, err := f.keeper.BridgeBindings.Get(ctx, key)
	require.NoError(t, err)
	require.Equal(t, int64(f.keeper.CurrentOperatorRewardEpoch(ctx)), b.LastRewardEpoch)
	require.Equal(t, "1000000", b.CumulativeRewards.String())
}

func TestOperatorRewardOffEpochIsNoop(t *testing.T) {
	f := initFixture(t)
	addr := testAddr(t, f, "op-offepoch")
	seedRewardBinding(t, f, addr, "peer-a", 10, 10, 0, 0)
	fundOperatorPool(f, 1_000_000)

	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(int64(params.OperatorRewardEpochBlocks) + 1)
	require.False(t, f.keeper.IsOperatorRewardEpoch(ctx))

	require.NoError(t, f.keeper.DistributeOperatorRewards(ctx))
	require.True(t, operatorBalance(f, addr).IsZero())
	require.Equal(t, math.NewInt(1_000_000), f.keeper.GetOperatorRewardPool(ctx))
}

func TestOperatorRewardPoolOverflowIsBurned(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	poolCap := params.MaxOperatorRewardPool
	f.bankKeeper.balances[keeper.OperatorRewardPoolAddress().String()] =
		sdk.NewCoins(sdk.NewCoin(testDenom, poolCap.MulRaw(2)))

	require.NoError(t, f.keeper.BurnOperatorRewardPoolOverflow(f.ctx))

	expected := poolCap.MulRaw(2).Sub(poolCap.Quo(math.NewInt(2)))
	require.Equal(t, expected.String(), f.keeper.GetOperatorRewardPool(f.ctx).String())
}

func TestOperatorPoolFundingIsNoopWithoutDistrKeeper(t *testing.T) {
	// The standalone fixture wires no distribution keeper. Funding must be a
	// silent no-op rather than an error — it runs in BeginBlock, where an
	// error would take the block with it.
	f := initFixture(t)
	require.NoError(t, f.keeper.FundOperatorRewardPool(f.ctx))
	require.True(t, f.keeper.GetOperatorRewardPool(f.ctx).IsZero())
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
}

// TestChallengeUpheldIncrementsOperatorRejected pins the counter wiring that
// makes the rejection gate work.
//
// content_rejected was declared on BridgeBinding and documented as
// "incremented on CHALLENGE_UPHELD" from the start, but nothing ever
// incremented it — it read zero for every operator on chain. It is now
// load-bearing: epoch_rejected is what disqualifies an operator whose content
// a jury found false, so a regression here silently pays operators for
// falsified submissions.
func TestChallengeUpheldIncrementsOperatorRejected(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	registerTestPeer(t, f, ms, "reject-peer")
	opStr := registerTestBridge(t, f, ms, "reject-peer", "reject-op")

	hash := sha256.Sum256([]byte("reject-body"))
	contentID := submitTestContent(t, f, ms, opStr, "reject-peer", hash[:])
	verifierStr := bondTestVerifier(t, f, ms, "reject-verifier")

	_, err := ms.VerifyContent(f.ctx, &types.MsgVerifyContent{
		Creator: verifierStr, ContentId: contentID, ContentHash: hash[:],
	})
	require.NoError(t, err)

	// Stand up the jury lifecycle record the resolve handler requires.
	require.NoError(t, f.keeper.EscalatedChallenges.Set(f.ctx, contentID, types.EscalatedChallenge{
		ContentId:             contentID,
		Escalator:             verifierStr,
		JuryDeadline:          sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix() + 3600,
		EscrowedEscalationFee: math.ZeroInt(),
	}))

	_, err = ms.ResolveEscalatedChallenge(f.ctx, &types.MsgResolveEscalatedChallenge{
		Authority: f.authority,
		ContentId: contentID,
		Verdict:   types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD,
		Reasoning: "hash did not match source",
	})
	require.NoError(t, err)

	b, err := f.keeper.BridgeBindings.Get(f.ctx, collections.Join(opStr, "reject-peer"))
	require.NoError(t, err)
	require.Equal(t, uint64(1), b.ContentRejected, "lifetime rejection counter must move")
	require.Equal(t, uint64(1), b.EpochRejected, "and the epoch counter the pay gate reads")
}
