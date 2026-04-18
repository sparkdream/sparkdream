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

	_, err := msgServer.SubmitAnonymousProposal(ctx, &types.MsgSubmitAnonymousProposal{
		Proposer:      shieldModuleAddr(),
		PolicyAddress: policyAddr,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty proposal")
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

func TestAnonymousVoteProposal_ProposalNotFound(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	_, err := msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
		Voter:      shieldModuleAddr(),
		ProposalId: 9999,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestAnonymousVoteProposal_UnspecifiedOption(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)

	_, err := msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
		Voter:      shieldModuleAddr(),
		ProposalId: pid,
		Option:     types.VoteOption_VOTE_OPTION_UNSPECIFIED,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "vote option must be specified")
}

func TestAnonymousVoteProposal_TalliesIncrement(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)

	for _, opt := range []types.VoteOption{
		types.VoteOption_VOTE_OPTION_NO,
		types.VoteOption_VOTE_OPTION_ABSTAIN,
		types.VoteOption_VOTE_OPTION_NO_WITH_VETO,
	} {
		_, err := msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
			Voter:      shieldModuleAddr(),
			ProposalId: pid,
			Option:     opt,
		})
		require.NoError(t, err)
	}

	tally, err := k.AnonVoteTallies.Get(ctx, pid)
	require.NoError(t, err)
	require.Equal(t, uint64(0), tally.YesCount)
	require.Equal(t, uint64(1), tally.NoCount)
	require.Equal(t, uint64(1), tally.AbstainCount)
	require.Equal(t, uint64(1), tally.NoWithVetoCount)
}

func TestAnonymousVoteProposal_VotingDeadlinePassed(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	msgServer := keeper.NewMsgServerImpl(k)

	pid := submitAnonProposal(t, k, ctx)

	// Advance proposal's voting deadline into the past.
	prop, err := k.Proposals.Get(ctx, pid)
	require.NoError(t, err)
	prop.VotingDeadline = ctx.BlockTime().Unix() - 1
	require.NoError(t, k.Proposals.Set(ctx, pid, prop))

	_, err = msgServer.AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
		Voter:      shieldModuleAddr(),
		ProposalId: pid,
		Option:     types.VoteOption_VOTE_OPTION_YES,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "voting period has ended")
}
