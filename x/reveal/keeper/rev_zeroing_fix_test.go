package keeper_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// lockLedger makes the fixture's rep mock track StakedDream per address the
// way x/rep does: an unlock above the locked amount fails with
// ErrInsufficientStake.
func lockLedger(f *testFixture) map[string]math.Int {
	staked := map[string]math.Int{}
	get := func(a sdk.AccAddress) math.Int {
		if v, ok := staked[a.String()]; ok {
			return v
		}
		return math.ZeroInt()
	}
	f.repKeeper.lockDREAMFn = func(_ context.Context, a sdk.AccAddress, amt math.Int) error {
		staked[a.String()] = get(a).Add(amt)
		return nil
	}
	f.repKeeper.unlockDREAMFn = func(_ context.Context, a sdk.AccAddress, amt math.Int) error {
		if get(a).LT(amt) {
			return reptypes.ErrInsufficientStake
		}
		staked[a.String()] = get(a).Sub(amt)
		return nil
	}
	return staked
}

// REV-101: a zeroed staker's stake on a BACKED tranche is dropped by the
// zeroing hook. After an overturned zeroing (x/rep re-mints the burn as
// liquid DREAM) the member locks DREAM elsewhere; settling the tranche must
// leave that unrelated lock alone.
func TestREV101_OverturnedZeroingNeverReleasesAnUnrelatedLock(t *testing.T) {
	f := initTestFixture(t)
	staked := lockLedger(f)
	id := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, id)
	f.stakeOnTranche(t, id, 0, f.staker, 500)
	f.stakeOnTranche(t, id, 0, f.staker2, 500)
	contrib, err := f.keeper.Contribution.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_BACKED, contrib.Tranches[0].Status)

	// x/rep zeroes staker2 (StakedDream reset) and runs the hook.
	staked[f.staker2] = math.ZeroInt()
	require.NoError(t, keeper.NewZeroingHook(&f.keeper).AfterMemberZeroed(f.ctx, f.staker2, []string{f.staker2}))
	contrib, err = f.keeper.Contribution.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1000), contrib.Tranches[0].DreamStaked, "a BACKED tranche keeps the stake's weight")
	resp, err := f.queryServer.StakesByStaker(f.ctx, &types.QueryStakesByStakerRequest{Staker: f.staker2})
	require.NoError(t, err)
	require.Empty(t, resp.Stakes)

	// Overturned: staker2 is restored and locks 500 for something else.
	staked[f.staker2] = math.NewInt(500)

	_, err = f.msgServer.Cancel(f.ctx, &types.MsgCancel{Authority: f.authority, ContributionId: id, Reason: "council"})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), staked[f.staker2], "the unrelated lock is intact")
	require.True(t, staked[f.staker].IsZero(), "the other staker is returned")
}

// REV-102: a zeroed contributor's PROPOSED and IN_PROGRESS contributions are
// cancelled by the hook without touching the burned bond; other stakers get
// their stakes back. MsgReject on a contribution the hook missed settles the
// gone bond instead of failing.
func TestREV102_ZeroedContributorsContributionsAreCancelled(t *testing.T) {
	f := initTestFixture(t)
	staked := lockLedger(f)
	minted := math.ZeroInt()
	f.repKeeper.mintDREAMFn = func(_ context.Context, _ sdk.AccAddress, amt math.Int) error {
		minted = minted.Add(amt)
		return nil
	}
	proposed := f.createSingleTrancheProposal(t, 1000)
	running := f.createDefaultProposal(t)
	f.approveContribution(t, running)
	f.stakeOnTranche(t, running, 0, f.staker, 300)

	staked[f.contributor] = math.ZeroInt() // x/rep burned the bonds
	require.NoError(t, keeper.NewZeroingHook(&f.keeper).AfterMemberZeroed(f.ctx, f.contributor, []string{f.contributor}))

	for _, id := range []uint64{proposed, running} {
		c, err := f.keeper.Contribution.Get(f.ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, c.Status)
		require.True(t, c.BondRemaining.IsZero())
		require.True(t, c.HoldbackAmount.IsZero())
	}
	require.True(t, staked[f.staker].IsZero(), "the staker's lock is returned")
	require.True(t, minted.IsZero(), "nothing is minted to the zeroed contributor")

	// Reject on a PROPOSED contribution whose bond is gone.
	g := initTestFixture(t)
	gStaked := lockLedger(g)
	id := g.createSingleTrancheProposal(t, 1000)
	gStaked[g.contributor] = math.ZeroInt()
	_, err := g.msgServer.Reject(g.ctx, &types.MsgReject{Authority: g.authority, Proposer: g.authority, ContributionId: id, Reason: "zeroed"})
	require.NoError(t, err)
	c, err := g.keeper.Contribution.Get(g.ctx, id)
	require.NoError(t, err)
	require.True(t, c.BondRemaining.IsZero())
}

// REV-103: Stake, Reveal and Verify are refused from their phase's deadline
// block on; the EndBlocker owns the tranche from then.
func TestREV103_ActionsRefusedAtTheirDeadline(t *testing.T) {
	f := initTestFixture(t)
	id := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, id)
	c, err := f.keeper.Contribution.Get(f.ctx, id)
	require.NoError(t, err)

	at := f.sdkCtx.WithBlockHeight(c.Tranches[0].StakeDeadline)
	_, err = f.msgServer.Stake(at, &types.MsgStake{Staker: f.staker, ContributionId: id, TrancheId: 0, Amount: math.NewInt(1000)})
	require.ErrorIs(t, err, types.ErrDeadlinePassed)

	f.stakeOnTranche(t, id, 0, f.staker, 1000)
	c, err = f.keeper.Contribution.Get(f.ctx, id)
	require.NoError(t, err)
	at = f.sdkCtx.WithBlockHeight(c.Tranches[0].RevealDeadline)
	_, err = f.msgServer.Reveal(at, &types.MsgReveal{Contributor: f.contributor, ContributionId: id, TrancheId: 0, CodeUri: "ipfs://c", CommitHash: "abc"})
	require.ErrorIs(t, err, types.ErrDeadlinePassed)

	f.revealTranche(t, id, 0)
	c, err = f.keeper.Contribution.Get(f.ctx, id)
	require.NoError(t, err)
	at = f.sdkCtx.WithBlockHeight(c.Tranches[0].VerificationDeadline)
	_, err = f.msgServer.Verify(at, &types.MsgVerify{Voter: f.staker, ContributionId: id, TrancheId: 0, ValueConfirmed: true, QualityRating: 4})
	require.ErrorIs(t, err, types.ErrDeadlinePassed)
	f.castVerifyVote(t, id, 0, f.staker, true, 4)
}

// REV-104: a contribution whose deadline step fails is parked behind later
// deadlines instead of being picked first every block.
func TestREV104_FailingDeadlineIsDeferred(t *testing.T) {
	f := initTestFixture(t)
	failing := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, failing)
	f.stakeOnTranche(t, failing, 0, f.staker, 300)
	c, err := f.keeper.Contribution.Get(f.ctx, failing)
	require.NoError(t, err)

	f.repKeeper.unlockDREAMFn = func(_ context.Context, a sdk.AccAddress, _ math.Int) error {
		if a.String() == f.staker {
			return errors.New("store failure")
		}
		return nil
	}
	h := c.Tranches[0].StakeDeadline
	ctx := f.sdkCtx.WithBlockHeight(h)
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))

	next, err := f.keeper.ContributionNextDeadline.Get(ctx, failing)
	require.NoError(t, err)
	require.Greater(t, next, h, "the failing entry is no longer due")
	c, err = f.keeper.Contribution.Get(ctx, failing)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS, c.Status)

	// Once the step can succeed, the deferred entry is processed.
	f.repKeeper.unlockDREAMFn = nil
	ctx = f.sdkCtx.WithBlockHeight(next)
	require.NoError(t, f.keeper.ProcessDeadlines(ctx))
	c, err = f.keeper.Contribution.Get(ctx, failing)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED, c.Status)
}
