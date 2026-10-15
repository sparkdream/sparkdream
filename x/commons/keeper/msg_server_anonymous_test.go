package keeper_test

import (
	"testing"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

func shieldModuleAddr() string {
	return authtypes.NewModuleAddress("shield").String()
}

// --- SubmitAnonymousProposal ---

func TestSubmitAnonymousProposal_Success(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	councilName := "AnonCouncil"
	policyAddr := sdk.AccAddress([]byte("anon_policy_________")).String()
	setupProposalState(t, k, ctx, councilName, policyAddr, shieldModuleAddr())

	anyMsg := packMsg(t, &types.MsgUpdateParams{
		Authority: k.GetAuthorityString(),
		Params:    types.DefaultParams(),
	})

	resp, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
		Messages:      []*codectypes.Any{anyMsg},
		Metadata:      "note",
	})
	require.NoError(t, err)

	prop, err := k.Proposals.Get(ctx, resp.ProposalId)
	require.NoError(t, err)
	require.Equal(t, shieldModuleAddr(), prop.Proposer)
	require.Contains(t, prop.Metadata, "[anonymous]")
}

func TestSubmitAnonymousProposal_UnauthorizedProposer(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("anon_policy_________")).String()
	setupProposalState(t, k, ctx, "AnonCouncil", policyAddr, shieldModuleAddr())

	_, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      sdk.AccAddress([]byte("not_shield__________")).String(),
		PolicyAddress: policyAddr,
		Messages:      []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{Authority: "x", Params: types.DefaultParams()})},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only the shield module account")
}

func TestSubmitAnonymousProposal_UnknownPolicy(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: "bogus_policy",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown policy address")
}

func TestSubmitAnonymousProposal_EmptyMessages(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("anon_policy_________")).String()
	setupProposalState(t, k, ctx, "AnonCouncil", policyAddr, shieldModuleAddr())

	// No messages and no metadata: a blank vote.
	_, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "signaling proposal requires non-empty metadata")

	// A signaling proposal with metadata is accepted, as for SubmitProposal.
	_, err = msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
		Metadata:      "Should we hold a community call?",
	})
	require.NoError(t, err)
}

func TestSubmitAnonymousProposal_MsgNotAllowed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("anon_policy_________")).String()
	setupProposalState(t, k, ctx, "AnonCouncil", policyAddr, shieldModuleAddr())

	// Overwrite permissions so MsgUpdateParams is not allowed.
	require.NoError(t, k.PolicyPermissions.Set(ctx, policyAddr, types.PolicyPermissions{
		PolicyAddress:   policyAddr,
		AllowedMessages: []string{"/some.other.Msg"},
	}))

	_, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
		Messages: []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{
			Authority: "x", Params: types.DefaultParams(),
		})},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed for policy")
}

func TestSubmitAnonymousProposal_TermExpired(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("anon_policy_________")).String()
	setupProposalState(t, k, ctx, "AnonCouncil", policyAddr, shieldModuleAddr())

	// Force the group term to be in the past.
	group, err := k.Groups.Get(ctx, "AnonCouncil")
	require.NoError(t, err)
	group.CurrentTermExpiration = ctx.BlockTime().Unix() - 1
	require.NoError(t, k.Groups.Set(ctx, "AnonCouncil", group))

	_, err = msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
		Messages: []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{
			Authority: "x", Params: types.DefaultParams(),
		})},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "TERM EXPIRED")
}

// --- AnonymousVoteProposal ---

func submitAnonProposal(t *testing.T, k keeper.Keeper, ctx sdk.Context) uint64 {
	t.Helper()
	msgServer := keeper.NewMsgServerImpl(k)

	policyAddr := sdk.AccAddress([]byte("anon_vote_policy____")).String()
	setupProposalState(t, k, ctx, "AnonVoteCouncil", policyAddr, shieldModuleAddr())

	resp, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
		Messages: []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{
			Authority: k.GetAuthorityString(), Params: types.DefaultParams(),
		})},
	})
	require.NoError(t, err)
	return resp.ProposalId
}

func TestAnonymousVoteProposal_UnauthorizedVoter(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)

	_, err := msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
		Voter:      sdk.AccAddress([]byte("not_shield__________")).String(),
		ProposalId: pid,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "only the shield module account")
}

// Anonymous votes are refused on every proposal, and nothing is tallied.
func TestAnonymousVoteProposal_Disabled(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)

	for _, opt := range []types.VoteOption{
		types.VoteOption_VOTE_OPTION_YES,
		types.VoteOption_VOTE_OPTION_NO,
	} {
		_, err := msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
			Voter:      shieldModuleAddr(),
			ProposalId: pid,
			Option:     opt,
		})
		require.ErrorIs(t, err, types.ErrAnonymousVotingDisabled)
	}

	has, err := k.AnonVoteTallies.Has(ctx, pid)
	require.NoError(t, err)
	require.False(t, has)
}

// The proposal queries still report a stored anonymous tally (one recorded
// before anonymous votes were disabled): zeros when there is none.
func TestProposalQueries_ReportAnonTally(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	qs := keeper.NewQueryServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)

	got, err := qs.GetProposal(ctx, &types.QueryGetProposalRequest{ProposalId: pid})
	require.NoError(t, err)
	require.Equal(t, types.AnonVoteTally{}, got.AnonTally)

	want := types.AnonVoteTally{NoCount: 2, AbstainCount: 1}
	require.NoError(t, k.AnonVoteTallies.Set(ctx, pid, want))

	got, err = qs.GetProposal(ctx, &types.QueryGetProposalRequest{ProposalId: pid})
	require.NoError(t, err)
	require.Equal(t, want, got.AnonTally)

	votes, err := qs.GetProposalVotes(ctx, &types.QueryGetProposalVotesRequest{ProposalId: pid})
	require.NoError(t, err)
	require.Equal(t, want, votes.AnonTally)
}
