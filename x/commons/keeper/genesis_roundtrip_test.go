package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// CMN-15: groups built by the production constructors (the genesis bootstrap
// and MsgRegisterGroup) survive ExportGenesis -> InitGenesis on a fresh
// keeper: every group keeps its name key, the policy and veto indexes point
// at it, and members, proposal submission and spend gates still resolve.
// Before the fix the constructors left Group.Index empty and the export
// dropped the key, so the exported genesis failed Validate (duplicate "")
// and a direct import collapsed every group onto "".
func TestGenesis_ProductionRoundTrip(t *testing.T) {
	useCosmosPrefix(t)
	k, ctx, _ := setupCommonsKeeper(t)
	founder := addr("phoenix")
	k.CreateTestAccount(ctx, mustBytes(founder))
	gs := types.DefaultGenesis()
	gs.FoundingMembers = []types.FoundingMember{{Address: founder, DisplayName: "phoenix", Founder: true}}
	require.NoError(t, k.InitGenesis(ctx, *gs))

	ms := keeper.NewMsgServerImpl(k)
	gov := bech(authtypes.NewModuleAddress(types.GovModuleName))
	for _, name := range []string{"Aurora Guild", "Zenith Circle"} {
		msg := childMsg(gov)
		msg.Name = name
		_, err := ms.RegisterGroup(ctx, msg)
		require.NoError(t, err, name)
	}
	aurora, err := k.Groups.Get(ctx, "Aurora Guild")
	require.NoError(t, err)
	require.Equal(t, "Aurora Guild", aurora.Index, "runtime constructor sets the index")
	council, err := k.Groups.Get(ctx, "Commons Council")
	require.NoError(t, err)
	require.Equal(t, "Commons Council", council.Index, "bootstrap constructor sets the index")
	require.NotEmpty(t, council.VetoPolicyAddress, "the round trip covers a veto policy")

	// Anonymous state (CMN-17) rides along.
	require.NoError(t, k.AnonVoteTallies.Set(ctx, 7, types.AnonVoteTally{YesCount: 3, NoCount: 1}))
	day := ctx.BlockTime().Unix() / 86400
	require.NoError(t, k.AnonProposalCount.Set(ctx, collections.Join("Aurora Guild", day), 4))
	// CMN-107: the per-creator poll-opening bucket rides along too.
	pollBucket := types.PollOpenCount{Creator: "cosmos1pollcreator", UtcDay: day, Count: 3}
	require.NoError(t, k.PollOpenCounts.Set(ctx, pollBucket.Creator, pollBucket))

	out, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, out.Validate(), "an exported genesis must validate")

	names := map[string]types.Group{}
	require.NoError(t, k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
		names[name] = g
		return false, nil
	}))
	require.Len(t, out.GroupMap, len(names))
	for _, g := range out.GroupMap {
		_, ok := names[g.Index]
		require.True(t, ok, "exported index %q is a stored group key", g.Index)
	}

	// Import into a fresh keeper: the bootstrap is skipped (group_map is
	// non-empty) and every index is rebuilt from the group records.
	k2, ctx2, _ := setupCommonsKeeper(t)
	require.NoError(t, k2.InitGenesis(ctx2, *out))

	for name, want := range names {
		got, err := k2.Groups.Get(ctx2, name)
		require.NoError(t, err, name)
		require.Equal(t, want.PolicyAddress, got.PolicyAddress, name)
		require.Equal(t, name, got.Index)
		owner, err := k2.PolicyToName.Get(ctx2, want.PolicyAddress)
		require.NoError(t, err, name)
		require.Equal(t, name, owner)
		if want.VetoPolicyAddress != "" {
			owner, err := k2.PolicyToName.Get(ctx2, want.VetoPolicyAddress)
			require.NoError(t, err, name)
			require.Equal(t, name, owner)
			veto, err := k2.VetoPolicies.Get(ctx2, name)
			require.NoError(t, err, name)
			require.Equal(t, want.VetoPolicyAddress, veto)
		}
	}

	// Member resolution.
	has, err := k2.HasMember(ctx2, "Commons Council", founder)
	require.NoError(t, err)
	require.True(t, has)
	member, err := k2.IsGroupPolicyMember(ctx2, aurora.PolicyAddress, addr("aurora1"))
	require.NoError(t, err)
	require.True(t, member)

	// Proposal submission resolves the imported group by its policy.
	anyMsg, err := codectypes.NewAnyWithValue(&types.MsgSpendFromCommons{
		Authority: aurora.PolicyAddress, Recipient: addr("zenith"),
		Amount: sdk.NewCoins(sdk.NewInt64Coin("uspark", 1)),
	})
	require.NoError(t, err)
	_, err = keeper.NewMsgServerImpl(k2).SubmitProposal(ctx2, &types.MsgSubmitProposal{
		Proposer: addr("aurora1"), PolicyAddress: aurora.PolicyAddress, Messages: []*codectypes.Any{anyMsg},
	})
	require.NoError(t, err)

	// Spend authorization resolves the imported group too.
	require.NoError(t, k2.CheckSpendPreconditions(ctx2, aurora.PolicyAddress, sdk.NewCoins(sdk.NewInt64Coin("uspark", 1))))

	// CMN-17: anonymous tallies and the daily counter round-trip.
	tally, err := k2.AnonVoteTallies.Get(ctx2, 7)
	require.NoError(t, err)
	require.Equal(t, types.AnonVoteTally{YesCount: 3, NoCount: 1}, tally)
	count, err := k2.AnonProposalCount.Get(ctx2, collections.Join("Aurora Guild", day))
	require.NoError(t, err)
	require.Equal(t, uint64(4), count)
	out2, err := k2.ExportGenesis(ctx2)
	require.NoError(t, err)
	require.Equal(t, out.AnonVoteTallies, out2.AnonVoteTallies)
	require.Equal(t, out.AnonProposalCounts, out2.AnonProposalCounts)
	gotBucket, err := k2.PollOpenCounts.Get(ctx2, pollBucket.Creator)
	require.NoError(t, err)
	require.Equal(t, pollBucket, gotBucket)
	require.Equal(t, []types.PollOpenCount{pollBucket}, out2.PollOpenCounts)
}

// CMN-15: an export of records stored with an empty Index (as every group
// was before the constructors set it) takes the index from the key.
func TestGenesis_ExportUsesCollectionKey(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))
	require.NoError(t, k.Groups.Set(ctx, "Aurora Guild", types.Group{PolicyAddress: "p1"}))
	require.NoError(t, k.Groups.Set(ctx, "Zenith Circle", types.Group{PolicyAddress: "p2"}))
	out, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Len(t, out.GroupMap, 2)
	require.Equal(t, "Aurora Guild", out.GroupMap[0].Index)
	require.Equal(t, "Zenith Circle", out.GroupMap[1].Index)
	require.NoError(t, out.Validate())

	// A direct import of an empty index is refused rather than collapsing
	// groups onto "".
	bad := types.DefaultGenesis()
	bad.GroupMap = []types.Group{{PolicyAddress: "p1"}}
	k2, ctx2, _ := setupCommonsKeeper(t)
	require.Error(t, k2.InitGenesis(ctx2, *bad))
}

// CMN-16: EndBlock processes at most MaxProposalTransitionsPerBlock due
// queue entries per block, oldest first, across voting-deadline tallies and
// execution-window expiries; the rest drain in the following blocks.
func TestEndBlockProposals_PerBlockCap(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	councilName := "Phoenix Council"
	policyAddr := sdk.AccAddress([]byte("policy_cap_eb_______")).String()
	setupProposalState(t, k, ctx, councilName, policyAddr, sdk.AccAddress([]byte("voter_cap_eb________")).String())

	now := ctx.BlockTime().Unix()
	total := keeper.MaxProposalTransitionsPerBlock + 7
	base := now - int64(total) - 10
	for i := 1; i <= total; i++ {
		id := uint64(i)
		due := base + int64(i)
		p := types.Proposal{Id: id, CouncilName: councilName, PolicyAddress: policyAddr}
		if i%2 == 1 {
			p.Status = types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED
			p.VotingDeadline = due
		} else {
			p.Status = types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED
			p.ExecutionTime = due - keeper.ProposalExecutionWindow
		}
		require.NoError(t, k.SetProposalQueued(ctx, id, p))
	}

	open := func() int {
		n := 0
		for i := 1; i <= total; i++ {
			p, err := k.Proposals.Get(ctx, uint64(i))
			require.NoError(t, err)
			if p.Status == types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED ||
				p.Status == types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED {
				n++
			}
		}
		return n
	}

	require.NoError(t, k.EndBlockProposals(ctx))
	require.Equal(t, total-keeper.MaxProposalTransitionsPerBlock, open(), "only the cap is processed")
	for i := 1; i <= total; i++ {
		p, err := k.Proposals.Get(ctx, uint64(i))
		require.NoError(t, err)
		if i <= keeper.MaxProposalTransitionsPerBlock {
			if i%2 == 1 {
				require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_REJECTED, p.Status, "id %d", i)
			} else {
				require.Equal(t, types.ProposalStatus_PROPOSAL_STATUS_EXPIRED, p.Status, "id %d", i)
			}
		} else {
			require.Contains(t, []types.ProposalStatus{
				types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
				types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED,
			}, p.Status, "id %d stays queued", i)
		}
	}

	next := ctx.WithBlockTime(ctx.BlockTime().Add(time.Second))
	require.NoError(t, k.EndBlockProposals(next))
	require.Equal(t, 0, open(), "the backlog drains on the next block")
}
