package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/types"
)

func TestCreateProposal(t *testing.T) {
	t.Run("happy: member creates PUBLIC proposal", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Phoenix Initiative",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify stored proposal.
		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.Equal(t, "Phoenix Initiative", proposal.Title)
		require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE, proposal.Status)
		require.Equal(t, types.VisibilityLevel_VISIBILITY_PUBLIC, proposal.Visibility)
		require.Equal(t, f.member, proposal.Proposer)
	})

	t.Run("happy: module account creates (skips membership and deposit check)", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		// Create a module account address.
		moduleAddr := sdk.AccAddress([]byte("module_account______"))
		moduleStr, err := f.addressCodec.BytesToString(moduleAddr)
		require.NoError(t, err)

		// Mock auth keeper to return a module account for this address.
		f.authKeeper.getAccountFn = func(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
			if addr.Equals(moduleAddr) {
				return authtypes.NewModuleAccount(
					authtypes.NewBaseAccountWithAddress(addr),
					"testmodule",
				)
			}
			return nil
		}

		// Module accounts skip membership and deposit checks.
		resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   moduleStr,
			Title:      "Module Proposal",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    nil, // no deposit needed for module accounts
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("happy: custom quorum and threshold", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Custom Thresholds",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
			Quorum:     math.LegacyNewDecWithPrec(50, 2), // 50%
			Threshold:  math.LegacyNewDecWithPrec(67, 2), // 67%
		})
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.True(t, proposal.Quorum.Equal(math.LegacyNewDecWithPrec(50, 2)))
		require.True(t, proposal.Threshold.Equal(math.LegacyNewDecWithPrec(67, 2)))
	})

	t.Run("happy: custom voting period", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:           f.member,
			Title:              "Custom Voting Period",
			Options:            f.standardOptions(),
			Visibility:         types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:            sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
			VotingPeriodEpochs: 15,
		})
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		// VotingEnd should reflect 15 epochs of blocks.
		params, _ := f.keeper.Params.Get(f.ctx)
		expectedEnd := proposal.VotingStart + 15*int64(params.BlocksPerEpoch)
		require.Equal(t, expectedEnd, proposal.VotingEnd)
	})

	t.Run("happy: SEALED proposal sets RevealEpoch", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Sealed Proposal",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_SEALED,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.NotZero(t, proposal.RevealEpoch, "sealed proposal should have non-zero RevealEpoch")
		require.Greater(t, proposal.RevealEnd, proposal.VotingEnd, "reveal end should be after voting end")
	})

	t.Run("error: not member", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.nonMember,
			Title:      "Should Fail",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.ErrorIs(t, err, types.ErrNotAMember)
	})

	t.Run("error: PRIVATE visibility", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Private Not Allowed",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PRIVATE,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.ErrorContains(t, err, types.ErrInvalidVisibility.Error())
	})

	t.Run("error: SEALED not allowed", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.AllowSealedProposals = false
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		_, err = f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Sealed Disabled",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_SEALED,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.ErrorIs(t, err, types.ErrSealedNotAllowed)
	})

	t.Run("error: insufficient deposit", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Low Deposit",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1)), // well below min
		})
		require.ErrorIs(t, err, types.ErrInsufficientDeposit)
	})

	t.Run("error: voting period below range", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:           f.member,
			Title:              "Too Short",
			Options:            f.standardOptions(),
			Visibility:         types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:            sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
			VotingPeriodEpochs: 1, // below MinVotingPeriodEpochs (3)
		})
		require.ErrorIs(t, err, types.ErrVotingPeriodOutOfRange)
	})

	t.Run("error: voting period above range", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:           f.member,
			Title:              "Too Long",
			Options:            f.standardOptions(),
			Visibility:         types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:            sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
			VotingPeriodEpochs: 100, // above MaxVotingPeriodEpochs (30)
		})
		require.ErrorIs(t, err, types.ErrVotingPeriodOutOfRange)
	})

	t.Run("error: no eligible voters", func(t *testing.T) {
		f := initTestFixture(t)
		// Do NOT register any voters.

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "No Voters",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.ErrorIs(t, err, types.ErrNoEligibleVoters)
	})

	t.Run("error: threshold > 1", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Bad Threshold",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
			Threshold:  math.LegacyNewDecWithPrec(150, 2), // 1.50, exceeds 1
		})
		require.ErrorIs(t, err, types.ErrInvalidThreshold)
	})

	t.Run("verify: voter tree snapshot is stored", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		resp, err := f.msgServer.CreateProposal(f.ctx, &types.MsgCreateProposal{
			Proposer:   f.member,
			Title:      "Snapshot Test",
			Options:    f.standardOptions(),
			Visibility: types.VisibilityLevel_VISIBILITY_PUBLIC,
			Deposit:    sdk.NewCoins(sdk.NewInt64Coin("uspark", 1_000_000)),
		})
		require.NoError(t, err)

		snapshot, err := f.keeper.VoterTreeSnapshot.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.Equal(t, resp.ProposalId, snapshot.ProposalId)
		require.NotEmpty(t, snapshot.MerkleRoot)
		require.Equal(t, uint64(1), snapshot.VoterCount)
	})
}
