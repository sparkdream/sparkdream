package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

// Staking needs TRUST_LEVEL_PROVISIONAL or above (spec), not just an active
// membership.
func TestStakeRequiresProvisional(t *testing.T) {
	f := initTestFixture(t)
	id := f.createDefaultProposal(t)
	f.approveContribution(t, id)
	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil
	}
	_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{Staker: f.staker, ContributionId: id, TrancheId: 0, Amount: math.NewInt(1000)})
	require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
}

// Every tranche stays fillable: no threshold below min_stake_amount, no stake
// leaving a remainder below it, and a final stake below the minimum is
// allowed only when it fills the tranche exactly. (Fixture min_stake = 100.)
func TestTranchesStayFillable(t *testing.T) {
	t.Run("propose refuses a threshold below min stake", func(t *testing.T) {
		f := initTestFixture(t)
		_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
			Contributor: f.contributor, ProjectName: "nebula", Description: "d",
			TotalValuation: math.NewInt(150),
			Tranches: []types.TrancheDef{
				{Name: "a", Description: "a", StakeThreshold: math.NewInt(100)},
				{Name: "b", Description: "b", StakeThreshold: math.NewInt(50)},
			},
			InitialLicense: "BSL-1.1", FinalLicense: "Apache-2.0",
		})
		require.ErrorIs(t, err, types.ErrTrancheBelowMinStake)
	})
	t.Run("stake refuses an unfillable remainder", func(t *testing.T) {
		f := initTestFixture(t)
		id := f.createSingleTrancheProposal(t, 150)
		f.approveContribution(t, id)
		_, err := f.msgServer.Stake(f.ctx, &types.MsgStake{Staker: f.staker, ContributionId: id, TrancheId: 0, Amount: math.NewInt(100)})
		require.ErrorIs(t, err, types.ErrStakeLeavesUnfillableRest)
		// Filling it whole works.
		f.stakeOnTranche(t, id, 0, f.staker, 150)
	})
	t.Run("an exact final fill below min stake is allowed", func(t *testing.T) {
		// min_stake_amount raised after the stakes began: the 100 left is
		// below the new minimum but must still be fillable.
		f := initTestFixture(t)
		id := f.createSingleTrancheProposal(t, 250)
		f.approveContribution(t, id)
		f.stakeOnTranche(t, id, 0, f.staker, 150)
		p, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		p.MinStakeAmount = math.NewInt(200)
		require.NoError(t, f.keeper.Params.Set(f.ctx, p))
		_, err = f.msgServer.Stake(f.ctx, &types.MsgStake{Staker: f.staker2, ContributionId: id, TrancheId: 0, Amount: math.NewInt(40)})
		require.ErrorIs(t, err, types.ErrStakeAmountTooLow, "below min and not the exact remainder")
		f.stakeOnTranche(t, id, 0, f.staker2, 100)
	})
}

// IMPROVE and REJECT verdicts need a reason.
func TestResolveDisputeRequiresReason(t *testing.T) {
	f := initTestFixture(t)
	for _, v := range []types.DisputeVerdict{types.DisputeVerdict_DISPUTE_VERDICT_IMPROVE, types.DisputeVerdict_DISPUTE_VERDICT_REJECT} {
		_, err := f.msgServer.ResolveDispute(f.ctx, &types.MsgResolveDispute{
			Authority: f.authority, Proposer: f.authority, ContributionId: 1, Verdict: v, Reason: "  ",
		})
		require.ErrorIs(t, err, types.ErrReasonRequired, v.String())
	}
}

// The recorded initiator of a council-routed msg must be an Operations
// Committee member; under gov it may be omitted and is then the authority.
func TestApproveChecksProposer(t *testing.T) {
	f := initTestFixture(t)
	id := f.createDefaultProposal(t)
	f.commonsKeeper.isCommitteeMemberFn = func(_ context.Context, a sdk.AccAddress, _, _ string) (bool, error) {
		return a.Equals(f.stakerAddr), nil
	}
	_, err := f.msgServer.Approve(f.ctx, &types.MsgApprove{Authority: f.authority, Proposer: f.contributor, ContributionId: id})
	require.ErrorIs(t, err, types.ErrInvalidProposer)

	_, err = f.msgServer.Approve(f.ctx, &types.MsgApprove{Authority: f.authority, ContributionId: id})
	require.NoError(t, err)
	c, err := f.keeper.Contribution.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, f.authority, c.ApprovedBy, "gov path records the authority")

	id2 := f.createSingleTrancheProposal(t, 1000)
	_, err = f.msgServer.Approve(f.ctx, &types.MsgApprove{Authority: f.authority, Proposer: f.staker, ContributionId: id2})
	require.NoError(t, err)
	c, err = f.keeper.Contribution.Get(f.ctx, id2)
	require.NoError(t, err)
	require.Equal(t, f.staker, c.ApprovedBy)
}

// A reveal must carry a code URI and a commit hash.
func TestRevealRequiresCodeAndCommit(t *testing.T) {
	f := initTestFixture(t)
	id := f.createSingleTrancheProposal(t, 1000)
	f.approveContribution(t, id)
	f.stakeOnTranche(t, id, 0, f.staker, 1000)
	for _, m := range []*types.MsgReveal{
		{Contributor: f.contributor, ContributionId: id, TrancheId: 0, CodeUri: "", CommitHash: "abc"},
		{Contributor: f.contributor, ContributionId: id, TrancheId: 0, CodeUri: "ipfs://code", CommitHash: " "},
	} {
		_, err := f.msgServer.Reveal(f.ctx, m)
		require.ErrorIs(t, err, types.ErrEmptyReveal)
	}
	f.revealTranche(t, id, 0)
}
