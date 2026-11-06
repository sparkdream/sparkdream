package keeper_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"cosmossdk.io/math"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// keyRep is an x/rep double with per-address ZK keys and migration aliases
// (alias[old] = new).
type keyRep struct {
	keys  map[string][]byte
	alias map[string]string
}

func (r keyRep) CanActAs(_ context.Context, signer, stored string) bool {
	if to, ok := r.alias[stored]; ok {
		stored = to
	}
	return signer == stored
}
func (r keyRep) ActiveZkPublicKey(_ context.Context, addr string) []byte { return r.keys[addr] }

// cosmosAddr encodes b with the fixture's address codec prefix ("cosmos"),
// whatever the global SDK config holds.
func cosmosAddr(b string) string {
	return sdk.MustBech32ifyAddressBytes("cosmos", []byte(b))
}

func testMember(i int) string {
	return cosmosAddr(fmt.Sprintf("ballot_member_%06d", i))
}

func testZkKey(i int) []byte {
	return zkcrypto.DerivePublicKey(zkcrypto.PadTo32([]byte{byte(i + 1)}))
}

// setupBallotGroup creates a group of n members (weight "1"), the first
// keyed of them holding ZK keys, with the given ballot mode, and wires a
// keyRep. It returns the policy address and the members.
func setupBallotGroup(t *testing.T, k keeper.Keeper, ctx sdk.Context, name string, mode types.BallotMode, n, keyed int) (string, []string) {
	t.Helper()
	policy := cosmosAddr(fmt.Sprintf("%-20s", name+"_pol"))
	members := make([]string, n)
	for i := range members {
		members[i] = testMember(i)
	}
	setupProposalState(t, k, ctx, name, policy, members[0])
	for _, m := range members[1:] {
		require.NoError(t, k.AddMember(ctx, name, types.Member{Address: m, Weight: "1"}))
	}
	g, err := k.Groups.Get(ctx, name)
	require.NoError(t, err)
	g.Index = name
	g.BallotMode = mode
	require.NoError(t, k.Groups.Set(ctx, name, g))

	rep := keyRep{keys: map[string][]byte{}, alias: map[string]string{}}
	for i := 0; i < keyed; i++ {
		rep.keys[members[i]] = testZkKey(i)
	}
	k.SetRepKeeper(rep)
	return policy, members
}

func submitBallotProposal(t *testing.T, k keeper.Keeper, ctx sdk.Context, proposer, policy string, secret bool) (uint64, error) {
	t.Helper()
	resp, err := keeper.NewMsgServerImpl(k).SubmitProposal(ctx, &types.MsgSubmitProposal{
		Proposer:      proposer,
		PolicyAddress: policy,
		Messages: []*codectypes.Any{packMsg(t, &types.MsgUpdateParams{
			Authority: k.GetAuthorityString(), Params: types.DefaultParams(),
		})},
		SecretBallot: secret,
	})
	if err != nil {
		return 0, err
	}
	return resp.ProposalId, nil
}

func anonVote(k keeper.Keeper, ctx sdk.Context, pid uint64, opt types.VoteOption) error {
	_, err := keeper.NewMsgServerImpl(k).AnonymousVoteProposal(ctx, &types.MsgAnonymousVoteProposal{
		Voter: shieldModuleAddr(), ProposalId: pid, Option: opt,
	})
	return err
}

// A SECRET group snapshots its keyed members at submission (the root a
// client rebuilds from the leaf list), refuses open votes, counts anonymous
// ones at the member weight and drops the snapshot once accepted.
func TestSecretBallot_SnapshotVoteAndAccept(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	policy, members := setupBallotGroup(t, k, ctx, "SecretCouncil", types.BallotMode_BALLOT_MODE_SECRET, 5, 5)

	pid, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
	require.NoError(t, err)
	prop, err := k.Proposals.Get(ctx, pid)
	require.NoError(t, err)
	require.True(t, prop.SecretBallot, "a SECRET group always votes secretly")
	require.Equal(t, uint64(5), prop.BallotMemberCount)
	require.Equal(t, "1", prop.BallotMemberWeight)

	// The root is the sparse tree of MiMC(key, 0) in member-address order.
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)
	leaves := map[uint64][]byte{}
	for i, m := range sorted {
		idx := -1
		for j, mm := range members {
			if mm == m {
				idx = j
			}
		}
		leaves[uint64(i)] = zkcrypto.ComputeLeaf(testZkKey(idx), 0)
	}
	want, err := zkcrypto.SparseMerkleProof(leaves, zkcrypto.TreeDepth, 0)
	require.NoError(t, err)
	require.Equal(t, want.Root, prop.VoterRoot)

	tree, err := keeper.NewQueryServerImpl(k).GetProposalVoterTree(ctx, &types.QueryGetProposalVoterTreeRequest{ProposalId: pid})
	require.NoError(t, err)
	require.Len(t, tree.Tree.Leaves, 5)
	require.Equal(t, uint32(zkcrypto.TreeDepth), tree.Tree.Depth)

	root, err := k.ShieldMerkleRoot(ctx, &types.MsgAnonymousVoteProposal{ProposalId: pid})
	require.NoError(t, err)
	require.Equal(t, prop.VoterRoot, root)

	_, err = keeper.NewMsgServerImpl(k).VoteProposal(ctx, &types.MsgVoteProposal{
		Voter: members[0], ProposalId: pid, Option: types.VoteOption_VOTE_OPTION_YES,
	})
	require.ErrorIs(t, err, types.ErrBallotMode, "no open vote on a secret ballot")

	require.NoError(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_YES))
	require.NoError(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_NO))
	require.NoError(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_YES))
	prop, _ = k.Proposals.Get(ctx, pid)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, prop.Status, "2 of 5 yes is below 0.51")

	tally, err := k.TallyProposal(ctx, prop)
	require.NoError(t, err)
	require.Equal(t, "2.000000000000000000", tally.YesWeight)

	require.NoError(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_YES))
	prop, _ = k.Proposals.Get(ctx, pid)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED, prop.Status, "3 of 5 yes passes 0.51")

	has, err := k.ProposalVoterTrees.Has(ctx, pid)
	require.NoError(t, err)
	require.False(t, has, "the snapshot goes once voting is over")
	_, err = k.ShieldMerkleRoot(ctx, &types.MsgAnonymousVoteProposal{ProposalId: pid})
	require.ErrorIs(t, err, types.ErrBallotMode)
	require.ErrorIs(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_YES), sdkerrors.ErrInvalidRequest)
}

// Members without a ZK key still count in the body: a secret ballot that
// does not reach the threshold of the whole snapshot is rejected at the
// deadline, and its snapshot is dropped.
func TestSecretBallot_KeylessMembersCountInBody(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	// Size the body from the build's minimum so the keyed voters stay a
	// minority under every network tag.
	policy, members := setupBallotGroup(t, k, ctx, "HalfKeyed", types.BallotMode_BALLOT_MODE_SECRET, int(2*keeper.MinSecretBallotVoters+1), int(keeper.MinSecretBallotVoters))

	pid, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
	require.NoError(t, err)
	for i := uint64(0); i < keeper.MinSecretBallotVoters; i++ {
		require.NoError(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_YES))
	}
	prop, _ := k.Proposals.Get(ctx, pid)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED, prop.Status)

	late := ctx.WithBlockTime(time.Unix(prop.VotingDeadline+1, 0))
	require.NoError(t, k.EndBlockProposals(late))
	prop, _ = k.Proposals.Get(late, pid)
	require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_REJECTED, prop.Status)
	has, _ := k.ProposalVoterTrees.Has(late, pid)
	require.False(t, has)
}

func TestSecretBallot_Refusals(t *testing.T) {
	t.Run("open group refuses a secret ballot request", func(t *testing.T) {
		k, ctx, _ := setupCommonsKeeper(t)
		policy, members := setupBallotGroup(t, k, ctx, "OpenCouncil", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 5, 5)
		_, err := submitBallotProposal(t, k, ctx, members[0], policy, true)
		require.ErrorIs(t, err, types.ErrBallotMode)
		pid, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
		require.NoError(t, err)
		require.ErrorIs(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_YES), types.ErrBallotMode)
	})
	t.Run("too few registered keys", func(t *testing.T) {
		k, ctx, _ := setupCommonsKeeper(t)
		policy, members := setupBallotGroup(t, k, ctx, "FewKeys", types.BallotMode_BALLOT_MODE_SECRET, 5, int(keeper.MinSecretBallotVoters)-1)
		_, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
		require.ErrorIs(t, err, types.ErrSecretBallotUnavailable)
	})
	t.Run("unequal weights", func(t *testing.T) {
		k, ctx, _ := setupCommonsKeeper(t)
		policy, members := setupBallotGroup(t, k, ctx, "Weighted", types.BallotMode_BALLOT_MODE_SECRET, 5, 5)
		require.NoError(t, k.AddMember(ctx, "Weighted", types.Member{Address: members[4], Weight: "2"}))
		_, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
		require.ErrorIs(t, err, types.ErrSecretBallotUnavailable)
	})
	t.Run("proposer choice", func(t *testing.T) {
		k, ctx, _ := setupCommonsKeeper(t)
		policy, members := setupBallotGroup(t, k, ctx, "Choice", types.BallotMode_BALLOT_MODE_PROPOSER_CHOICE, 5, 5)
		open, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
		require.NoError(t, err)
		secret, err := submitBallotProposal(t, k, ctx, members[0], policy, true)
		require.NoError(t, err)
		p1, _ := k.Proposals.Get(ctx, open)
		p2, _ := k.Proposals.Get(ctx, secret)
		require.False(t, p1.SecretBallot)
		require.True(t, p2.SecretBallot)
	})
}

// MsgUpdateGroupConfig sets the ballot mode; UNSPECIFIED leaves it alone.
func TestUpdateGroupConfig_BallotMode(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	setupBallotGroup(t, k, ctx, "Configured", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 1, 0)
	g, err := k.Groups.Get(ctx, "Configured")
	require.NoError(t, err)
	g.MinMembers, g.MaxMembers = 1, 5
	require.NoError(t, k.Groups.Set(ctx, "Configured", g))
	ms := keeper.NewMsgServerImpl(k)
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()

	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{
		Authority: govAddr, GroupName: "Configured", BallotMode: types.BallotMode_BALLOT_MODE_SECRET,
	})
	require.NoError(t, err)
	g, _ = k.Groups.Get(ctx, "Configured")
	require.Equal(t, types.BallotMode_BALLOT_MODE_SECRET, g.BallotMode)

	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{Authority: govAddr, GroupName: "Configured"})
	require.NoError(t, err)
	g, _ = k.Groups.Get(ctx, "Configured")
	require.Equal(t, types.BallotMode_BALLOT_MODE_SECRET, g.BallotMode, "UNSPECIFIED leaves the mode")

	_, err = ms.UpdateGroupConfig(ctx, &types.MsgUpdateGroupConfig{
		Authority: govAddr, GroupName: "Configured", BallotMode: types.BallotMode(9),
	})
	require.Error(t, err)
}

// MsgRegisterGroup stores the requested ballot mode and refuses an unknown one.
func TestRegisterGroup_BallotMode(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.NewParams("")))
	ms := keeper.NewMsgServerImpl(k)
	govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
	spend := math.NewInt(1000)
	threshold := math.LegacyNewDec(1)
	msg := func(name string, mode types.BallotMode) *types.MsgRegisterGroup {
		return &types.MsgRegisterGroup{
			Authority: govAddr, Name: name, Description: "ballot",
			Members: []string{sdk.AccAddress([]byte("ballot_register_____")).String()}, MemberWeights: []string{"1"},
			MinMembers: 1, MaxMembers: 5, TermDuration: 86400,
			VoteThreshold: &threshold, PolicyType: "threshold", VotingPeriod: 3600,
			MaxSpendPerEpoch: &spend, BallotMode: mode,
		}
	}
	_, err := ms.RegisterGroup(ctx, msg("Aurora Assembly", types.BallotMode_BALLOT_MODE_SECRET))
	require.NoError(t, err)
	g, err := k.Groups.Get(ctx, "Aurora Assembly")
	require.NoError(t, err)
	require.Equal(t, types.BallotMode_BALLOT_MODE_SECRET, g.BallotMode)

	_, err = ms.RegisterGroup(ctx, msg("Zenith Assembly", types.BallotMode(7)))
	require.Error(t, err)
	has, _ := k.Groups.Has(ctx, "Zenith Assembly")
	require.False(t, has)
}

// An anonymous vote must carry a real option.
func TestSecretBallot_InvalidOption(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	policy, members := setupBallotGroup(t, k, ctx, "OptionCouncil", types.BallotMode_BALLOT_MODE_SECRET, 5, 5)
	pid, err := submitBallotProposal(t, k, ctx, members[0], policy, false)
	require.NoError(t, err)
	require.ErrorIs(t, anonVote(k, ctx, pid, types.VoteOption_VOTE_OPTION_UNSPECIFIED), sdkerrors.ErrInvalidRequest)
	has, _ := k.AnonVoteTallies.Has(ctx, pid)
	require.False(t, has, "nothing tallied")
}
