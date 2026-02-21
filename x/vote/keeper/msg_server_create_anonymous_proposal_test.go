package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func TestCreateAnonymousProposal(t *testing.T) {
	// Helper to build a valid anonymous proposal message with sensible defaults.
	buildMsg := func(f *testFixture, visibility types.VisibilityLevel) *types.MsgCreateAnonymousProposal {
		return &types.MsgCreateAnonymousProposal{
			Submitter:    f.member,
			Title:        "Anonymous Proposal",
			Options:      f.standardOptions(),
			Visibility:   visibility,
			ClaimedEpoch: 10, // matches default season keeper epoch
			Nullifier:    genNullifier(1),
			Proof:        []byte("fake-proof"),
		}
	}

	t.Run("happy: PUBLIC visibility", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		resp, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE, proposal.Status)
		require.Equal(t, types.VisibilityLevel_VISIBILITY_PUBLIC, proposal.Visibility)
	})

	t.Run("happy: SEALED visibility", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_SEALED)
		resp, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.Equal(t, types.VisibilityLevel_VISIBILITY_SEALED, proposal.Visibility)
		require.NotZero(t, proposal.RevealEpoch)
	})

	t.Run("happy: PRIVATE visibility", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PRIVATE)
		resp, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.NoError(t, err)

		proposal, err := f.keeper.VotingProposal.Get(f.ctx, resp.ProposalId)
		require.NoError(t, err)
		require.Equal(t, types.VisibilityLevel_VISIBILITY_PRIVATE, proposal.Visibility)
		require.NotZero(t, proposal.RevealEpoch, "PRIVATE proposals should have a reveal epoch")
	})

	t.Run("error: epoch too old", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		msg.ClaimedEpoch = 8 // current=10, 8 is outside +-1
		_, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrEpochMismatch)
	})

	t.Run("error: epoch too new", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		msg.ClaimedEpoch = 12 // current=10, 12 is outside +-1
		_, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrEpochMismatch)
	})

	t.Run("error: nullifier already used (proposal limit reached)", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		// Pre-record the nullifier for the current epoch.
		nullifier := genNullifier(1)
		key := keeper.ProposalNullifierKeyForTest(10, nullifier)
		err := f.keeper.UsedProposalNullifier.Set(f.ctx, key, types.UsedProposalNullifier{
			Index:     key,
			Epoch:     10,
			Nullifier: nullifier,
			UsedAt:    0,
		})
		require.NoError(t, err)

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		msg.Nullifier = nullifier
		_, err = f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrProposalLimitReached)
	})

	t.Run("error: PRIVATE not allowed", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.AllowPrivateProposals = false
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PRIVATE)
		_, err = f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrPrivateNotAllowed)
	})

	t.Run("error: SEALED not allowed", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.AllowSealedProposals = false
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_SEALED)
		_, err = f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrSealedNotAllowed)
	})

	t.Run("error: too many voters for PRIVATE", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.MaxPrivateEligibleVoters = 0 // no voters allowed for PRIVATE
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PRIVATE)
		_, err = f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrTooManyEligibleVoters)
	})

	t.Run("error: invalid options (no standard)", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		msg.Options = []*types.VoteOption{
			{Id: 0, Label: "Abstain", Role: types.OptionRole_OPTION_ROLE_ABSTAIN},
			{Id: 1, Label: "Veto", Role: types.OptionRole_OPTION_ROLE_VETO},
		}
		_, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrNoStandardOption)
	})

	t.Run("error: no voters", func(t *testing.T) {
		f := initTestFixture(t)
		// Do NOT register any voters.

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		_, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrNoEligibleVoters)
	})

	t.Run("error: threshold > 1", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		msg.Threshold = math.LegacyNewDecWithPrec(200, 2) // 2.00
		_, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.ErrorIs(t, err, types.ErrInvalidThreshold)
	})

	t.Run("verify: proposal nullifier is recorded", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		nullifier := genNullifier(42)
		msg := buildMsg(f, types.VisibilityLevel_VISIBILITY_PUBLIC)
		msg.Nullifier = nullifier

		resp, err := f.msgServer.CreateAnonymousProposal(f.ctx, msg)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify the nullifier was recorded.
		key := keeper.ProposalNullifierKeyForTest(10, nullifier)
		has, err := f.keeper.UsedProposalNullifier.Has(f.ctx, key)
		require.NoError(t, err)
		require.True(t, has, "proposal nullifier should be recorded after creation")
	})
}
