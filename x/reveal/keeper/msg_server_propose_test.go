package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

func TestMsgPropose_Success(t *testing.T) {
	f := initTestFixture(t)

	resp, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "zenith-core",
		Description:    "Core library implementation",
		TotalValuation: math.NewInt(10000),
		Tranches: []types.TrancheDef{
			{Name: "phase-1", Description: "First", StakeThreshold: math.NewInt(5000)},
			{Name: "phase-2", Description: "Second", StakeThreshold: math.NewInt(5000)},
		},
		InitialLicense: "BSL-1.1",
		FinalLicense:   "Apache-2.0",
	})
	require.NoError(t, err)

	// Verify stored contribution
	contrib, err := f.keeper.Contribution.Get(f.ctx, resp.ContributionId)
	require.NoError(t, err)
	require.Equal(t, types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED, contrib.Status)
	require.Equal(t, f.contributor, contrib.Contributor)
	require.Equal(t, "zenith-core", contrib.ProjectName)
	require.Equal(t, 2, len(contrib.Tranches))
	require.Equal(t, math.NewInt(10000), contrib.TotalValuation)
	require.True(t, contrib.BondAmount.IsPositive()) // 10% of 10000 = 1000
	require.Equal(t, contrib.BondAmount, contrib.BondRemaining)
	require.Equal(t, math.ZeroInt(), contrib.HoldbackAmount)

	// All tranches start LOCKED
	for _, tr := range contrib.Tranches {
		require.Equal(t, types.TrancheStatus_TRANCHE_STATUS_LOCKED, tr.Status)
	}

	// Verify indexes
	has, err := f.keeper.ContributionsByStatus.Has(f.ctx, collections.Join(int32(types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED), resp.ContributionId))
	require.NoError(t, err)
	require.True(t, has)

	has, err = f.keeper.ContributionsByContributor.Has(f.ctx, collections.Join(f.contributor, resp.ContributionId))
	require.NoError(t, err)
	require.True(t, has)
}

func TestMsgPropose_NotMember(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.isMemberFn = func(_ context.Context, _ sdk.AccAddress) bool { return false }

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(1000),
		Tranches:       []types.TrancheDef{{Name: "t", StakeThreshold: math.NewInt(1000)}},
	})
	require.ErrorIs(t, err, types.ErrNotMember)
}

func TestMsgPropose_InsufficientTrustLevel(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.getTrustLevelFn = func(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
		return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil // trust level 0, need 2
	}

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(1000),
		Tranches:       []types.TrancheDef{{Name: "t", StakeThreshold: math.NewInt(1000)}},
	})
	require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
}

func TestMsgPropose_EmptyProjectName(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "",
		TotalValuation: math.NewInt(1000),
		Tranches:       []types.TrancheDef{{Name: "t", StakeThreshold: math.NewInt(1000)}},
	})
	require.ErrorIs(t, err, types.ErrEmptyProjectName)
}

func TestMsgPropose_NoTranches(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(1000),
		Tranches:       []types.TrancheDef{},
	})
	require.ErrorIs(t, err, types.ErrNoTranches)
}

func TestMsgPropose_TooManyTranches(t *testing.T) {
	f := initTestFixture(t)

	// Default max is 10; create 11 tranches
	defs := make([]types.TrancheDef, 11)
	for i := range defs {
		defs[i] = types.TrancheDef{Name: "t", StakeThreshold: math.NewInt(100)}
	}
	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(1100),
		Tranches:       defs,
	})
	require.ErrorIs(t, err, types.ErrTooManyTranches)
}

func TestMsgPropose_ValuationTooHigh(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(999999),
		Tranches:       []types.TrancheDef{{Name: "t", StakeThreshold: math.NewInt(999999)}},
	})
	require.ErrorIs(t, err, types.ErrValuationTooHigh)
}

func TestMsgPropose_ValuationMismatch(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(10000),
		Tranches: []types.TrancheDef{
			{Name: "t1", StakeThreshold: math.NewInt(5000)},
			{Name: "t2", StakeThreshold: math.NewInt(4000)}, // sum = 9000, not 10000
		},
	})
	require.ErrorIs(t, err, types.ErrValuationMismatch)
}

func TestMsgPropose_BondLockFailure(t *testing.T) {
	f := initTestFixture(t)
	f.repKeeper.lockDREAMFn = func(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
		return types.ErrInsufficientBond
	}

	_, err := f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "test",
		TotalValuation: math.NewInt(1000),
		Tranches:       []types.TrancheDef{{Name: "t", StakeThreshold: math.NewInt(1000)}},
	})
	require.ErrorIs(t, err, types.ErrInsufficientBond)
}

func TestMsgPropose_ProposalCooldown(t *testing.T) {
	f := initTestFixture(t)

	// Create first proposal, then reject it to set cooldown
	contribID := f.createSingleTrancheProposal(t, 1000)
	_, err := f.msgServer.Reject(f.ctx, &types.MsgReject{
		Authority:      f.authority,
		Proposer:       f.authority,
		ContributionId: contribID,
		Reason:         "not ready",
	})
	require.NoError(t, err)

	// Try to propose again during cooldown
	_, err = f.msgServer.Propose(f.ctx, &types.MsgPropose{
		Contributor:    f.contributor,
		ProjectName:    "retry",
		TotalValuation: math.NewInt(1000),
		Tranches:       []types.TrancheDef{{Name: "t", StakeThreshold: math.NewInt(1000)}},
	})
	require.ErrorIs(t, err, types.ErrProposalCooldown)
}
