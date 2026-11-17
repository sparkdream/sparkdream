package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// boardFixture is a two-seat board (absolute threshold 2) whose members are
// two councils' standard policies, overseeing a committee, as the Commons
// Supervisory Board oversees the Commons Governance Committee.
type boardFixture struct {
	k   keeper.Keeper
	ctx sdk.Context
	ms  types.MsgServer

	board, committee                         string
	phoenix, phoenixVeto, aurora, auroraVeto string
}

const (
	boardName     = "Zenith Board"
	committeeName = "Zenith Committee"
)

func newBoardFixture(t *testing.T) *boardFixture {
	t.Helper()
	k, ctx, _ := setupCommonsKeeper(t)
	f := &boardFixture{
		k: k, ctx: ctx, ms: keeper.NewMsgServerImpl(k),
		board:       sdk.AccAddress([]byte("board_policy________")).String(),
		committee:   sdk.AccAddress([]byte("committee_policy____")).String(),
		phoenix:     sdk.AccAddress([]byte("phoenix_standard____")).String(),
		phoenixVeto: sdk.AccAddress([]byte("phoenix_veto________")).String(),
		aurora:      sdk.AccAddress([]byte("aurora_standard_____")).String(),
		auroraVeto:  sdk.AccAddress([]byte("aurora_veto_________")).String(),
	}
	expiry := ctx.BlockTime().Unix() + 86400

	for _, c := range []struct{ name, standard, veto string }{
		{"Phoenix Council", f.phoenix, f.phoenixVeto},
		{"Aurora Council", f.aurora, f.auroraVeto},
	} {
		require.NoError(t, k.Groups.Set(ctx, c.name, types.Group{
			PolicyAddress: c.standard, VetoPolicyAddress: c.veto, CurrentTermExpiration: expiry,
		}))
		require.NoError(t, k.PolicyToName.Set(ctx, c.standard, c.name))
		require.NoError(t, k.PolicyToName.Set(ctx, c.veto, c.name))
		require.NoError(t, k.VetoPolicies.Set(ctx, c.name, c.veto))
		require.NoError(t, k.AddMember(ctx, boardName, types.Member{Address: c.standard, Weight: "1"}))
	}

	require.NoError(t, k.Groups.Set(ctx, boardName, types.Group{PolicyAddress: f.board, CurrentTermExpiration: expiry}))
	require.NoError(t, k.PolicyToName.Set(ctx, f.board, boardName))
	require.NoError(t, k.PolicyPermissions.Set(ctx, f.board, types.PolicyPermissions{
		PolicyAddress: f.board,
		AllowedMessages: []string{
			"/sparkdream.commons.v1.MsgVetoGroupProposals",
			"/sparkdream.commons.v1.MsgDeleteGroup",
		},
	}))
	require.NoError(t, k.DecisionPolicies.Set(ctx, f.board, types.DecisionPolicy{
		PolicyType: "threshold", Threshold: "2", VotingPeriod: 3600, MinExecutionPeriod: 600,
	}))

	require.NoError(t, k.Groups.Set(ctx, committeeName, types.Group{
		PolicyAddress: f.committee, ParentPolicyAddress: f.board, CurrentTermExpiration: expiry,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, f.committee, committeeName))
	return f
}

func (f *boardFixture) veto(t *testing.T) *types.MsgSubmitProposal {
	t.Helper()
	return &types.MsgSubmitProposal{
		PolicyAddress: f.board,
		Messages:      []*codectypes.Any{packMsg(t, &types.MsgVetoGroupProposals{Authority: f.board, GroupName: committeeName})},
		Metadata:      "veto the committee's pending proposals",
	}
}

func (f *boardFixture) status(t *testing.T, id uint64) types.ProposalStatus {
	t.Helper()
	p, err := f.k.Proposals.Get(f.ctx, id)
	require.NoError(t, err)
	return p.Status
}

// Each council's veto policy proposes and casts its council's seat on a pure
// veto, and the two seats pass it: no council standard proposal (and none of
// its execution wait) is involved.
func TestVetoSeat_VetoPoliciesPassABoardVeto(t *testing.T) {
	f := newBoardFixture(t)

	submit := f.veto(t)
	submit.Proposer = f.phoenixVeto
	res, err := f.ms.SubmitProposal(f.ctx, submit)
	require.NoError(t, err)
	id := res.ProposalId

	_, err = f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.phoenixVeto, ProposalId: id, Option: types.VoteOption_VOTE_OPTION_YES})
	require.NoError(t, err)
	// The vote is the seat's, not the veto policy's.
	vote, err := f.k.Votes.Get(f.ctx, collections.Join(id, f.phoenix))
	require.NoError(t, err)
	require.Equal(t, f.phoenix, vote.Voter)
	has, err := f.k.Votes.Has(f.ctx, collections.Join(id, f.phoenixVeto))
	require.NoError(t, err)
	require.False(t, has)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, f.status(t, id), "one seat of two is not enough")

	// The veto policy cannot vote the seat twice.
	_, err = f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.phoenixVeto, ProposalId: id, Option: types.VoteOption_VOTE_OPTION_NO})
	require.ErrorContains(t, err, "already voted")

	_, err = f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.auroraVeto, ProposalId: id, Option: types.VoteOption_VOTE_OPTION_YES})
	require.NoError(t, err)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, f.status(t, id))
}

// The council's standard policy (its majority) keeps the last word on its
// seat: it can replace the vote its veto policy cast.
func TestVetoSeat_StandardPolicyOverridesItsSeat(t *testing.T) {
	f := newBoardFixture(t)
	submit := f.veto(t)
	submit.Proposer = f.phoenixVeto
	res, err := f.ms.SubmitProposal(f.ctx, submit)
	require.NoError(t, err)

	_, err = f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.phoenixVeto, ProposalId: res.ProposalId, Option: types.VoteOption_VOTE_OPTION_YES})
	require.NoError(t, err)
	_, err = f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.phoenix, ProposalId: res.ProposalId, Option: types.VoteOption_VOTE_OPTION_NO})
	require.NoError(t, err)
	vote, err := f.k.Votes.Get(f.ctx, collections.Join(res.ProposalId, f.phoenix))
	require.NoError(t, err)
	require.Equal(t, types.VoteOption_VOTE_OPTION_NO, vote.Option)
}

// The seat rule is a brake only: anything but a pure veto still needs the
// council's standard policy, to propose and to vote.
func TestVetoSeat_OnlyPureVetoes(t *testing.T) {
	f := newBoardFixture(t)

	del := packMsg(t, &types.MsgDeleteGroup{Authority: f.board, GroupName: committeeName})
	for name, msgs := range map[string][]*codectypes.Any{
		"another message":     {del},
		"a veto plus another": {f.veto(t).Messages[0], del},
		"a signaling vote":    nil,
	} {
		_, err := f.ms.SubmitProposal(f.ctx, &types.MsgSubmitProposal{
			Proposer: f.phoenixVeto, PolicyAddress: f.board, Messages: msgs, Metadata: "not a veto",
		})
		require.ErrorContains(t, err, "is not a member", name)
	}

	// A board proposal that deletes the committee, however it was filed.
	const id = uint64(77)
	require.NoError(t, f.k.Proposals.Set(f.ctx, id, types.Proposal{
		Id: id, CouncilName: boardName, PolicyAddress: f.board, Messages: []*codectypes.Any{del},
		Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: f.ctx.BlockTime().Unix() + 3600,
	}))
	_, err := f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.phoenixVeto, ProposalId: id, Option: types.VoteOption_VOTE_OPTION_YES})
	require.ErrorContains(t, err, "is not a member")
	_, err = f.ms.VoteProposal(f.ctx, &types.MsgVoteProposal{Voter: f.phoenix, ProposalId: id, Option: types.VoteOption_VOTE_OPTION_YES})
	require.NoError(t, err)
}

// Only the veto policy of a council seated on the body qualifies, and only
// in a body without a veto policy of its own.
func TestVetoSeat_WhoQualifies(t *testing.T) {
	f := newBoardFixture(t)

	// A council that holds no seat on the board.
	outsider := sdk.AccAddress([]byte("outsider_standard___")).String()
	outsiderVeto := sdk.AccAddress([]byte("outsider_veto_______")).String()
	require.NoError(t, f.k.Groups.Set(f.ctx, "Outsider Council", types.Group{PolicyAddress: outsider, VetoPolicyAddress: outsiderVeto}))
	require.NoError(t, f.k.PolicyToName.Set(f.ctx, outsider, "Outsider Council"))
	require.NoError(t, f.k.PolicyToName.Set(f.ctx, outsiderVeto, "Outsider Council"))
	require.NoError(t, f.k.VetoPolicies.Set(f.ctx, "Outsider Council", outsiderVeto))

	for name, proposer := range map[string]string{
		"an unseated council's veto policy": outsiderVeto,
		"an ordinary account":               sdk.AccAddress([]byte("somebody____________")).String(),
		"the overseen committee's policy":   f.committee,
	} {
		submit := f.veto(t)
		submit.Proposer = proposer
		_, err := f.ms.SubmitProposal(f.ctx, submit)
		require.ErrorContains(t, err, "is not a member", name)
	}

	// Give the board a veto policy of its own: its members vote there
	// directly and the borrowed seat is off.
	board, err := f.k.Groups.Get(f.ctx, boardName)
	require.NoError(t, err)
	board.VetoPolicyAddress = sdk.AccAddress([]byte("board_veto__________")).String()
	require.NoError(t, f.k.Groups.Set(f.ctx, boardName, board))
	submit := f.veto(t)
	submit.Proposer = f.phoenixVeto
	_, err = f.ms.SubmitProposal(f.ctx, submit)
	require.ErrorContains(t, err, "is not a member")
}

// The Commons Council's golden-share seat (half the votes on the Technical
// and Ecosystem Councils) sits out a council's decision about its board
// seat, on the veto policy and on the standard policy; on everything else it
// still counts.
func TestBoardSeatRecusal_GoldenShareSitsOut(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	const council = "Technical Council" // a golden-share council
	standard := sdk.AccAddress([]byte("tech_standard_______")).String()
	veto := sdk.AccAddress([]byte("tech_veto___________")).String()
	commons := sdk.AccAddress([]byte("commons_standard____")).String()
	members := []string{
		sdk.AccAddress([]byte("tech_member_one_____")).String(),
		sdk.AccAddress([]byte("tech_member_two_____")).String(),
		sdk.AccAddress([]byte("tech_member_three___")).String(),
	}

	require.NoError(t, k.Groups.Set(ctx, "Commons Council", types.Group{PolicyAddress: commons}))
	require.NoError(t, k.Groups.Set(ctx, council, types.Group{
		PolicyAddress: standard, VetoPolicyAddress: veto, CurrentTermExpiration: ctx.BlockTime().Unix() + 86400,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, standard, council))
	require.NoError(t, k.PolicyToName.Set(ctx, veto, council))
	require.NoError(t, k.VetoPolicies.Set(ctx, council, veto))
	for _, m := range members {
		require.NoError(t, k.AddMember(ctx, council, types.Member{Address: m, Weight: "1"}))
	}
	// The golden seat weighs as much as everyone else together.
	require.NoError(t, k.AddMember(ctx, council, types.Member{Address: commons, Weight: "3"}))
	require.NoError(t, k.DecisionPolicies.Set(ctx, standard, types.DecisionPolicy{PolicyType: "percentage", Threshold: "0.66", VotingPeriod: 3600}))
	require.NoError(t, k.DecisionPolicies.Set(ctx, veto, types.DecisionPolicy{PolicyType: "percentage", Threshold: "0.49", VotingPeriod: 3600}))

	var nextID uint64
	open := func(policy string, msg sdk.Msg) uint64 {
		nextID++
		require.NoError(t, k.Proposals.Set(ctx, nextID, types.Proposal{
			Id: nextID, CouncilName: council, PolicyAddress: policy, Messages: []*codectypes.Any{packMsg(t, msg)},
			Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, VotingDeadline: ctx.BlockTime().Unix() + 3600,
		}))
		return nextID
	}
	yes := func(id uint64, voter string) types.ProposalStatus {
		_, err := ms.VoteProposal(ctx, &types.MsgVoteProposal{Voter: voter, ProposalId: id, Option: types.VoteOption_VOTE_OPTION_YES})
		require.NoError(t, err)
		p, err := k.Proposals.Get(ctx, id)
		require.NoError(t, err)
		return p.Status
	}
	const submitted, accepted = types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED
	seatVote := &types.MsgVoteProposal{Voter: veto, ProposalId: 999, Option: types.VoteOption_VOTE_OPTION_YES}

	// Veto policy, board-seat message: the golden seat alone no longer
	// passes it; 2 of the 3 own members do (0.67 >= 0.49).
	id := open(veto, seatVote)
	require.Equal(t, submitted, yes(id, commons))
	require.Equal(t, submitted, yes(id, members[0]))
	require.Equal(t, accepted, yes(id, members[1]))

	// Veto policy, any other message: the golden seat alone still passes it
	// (3 of 6 >= 0.49).
	id = open(veto, &types.MsgVetoGroupProposals{Authority: veto, GroupName: "Technical Operations Committee"})
	require.Equal(t, accepted, yes(id, commons))

	// Standard policy, board-seat message: the own members decide without
	// the golden seat (2 of 3 >= 0.66).
	id = open(standard, &types.MsgVoteProposal{Voter: standard, ProposalId: 999, Option: types.VoteOption_VOTE_OPTION_YES})
	require.Equal(t, submitted, yes(id, members[0]))
	require.Equal(t, accepted, yes(id, members[1]))

	// Standard policy, any other message: nothing passes without the golden
	// seat (3 of 6 < 0.66), and it passes with it.
	id = open(standard, &types.MsgRenewGroup{Authority: standard, GroupName: "Technical Operations Committee"})
	for _, m := range members {
		require.Equal(t, submitted, yes(id, m))
	}
	require.Equal(t, accepted, yes(id, commons))
}
