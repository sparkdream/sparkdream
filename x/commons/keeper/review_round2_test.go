package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"sparkdream/testutil"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// CMN-102: a proposal carrying x/rep MsgForceMigration needs the strict
// majority of a membership change: one member of a two-member 0.5 HR
// committee can no longer move a peer's seats to a key of their own.
func TestCMN102_ForceMigrationNeedsStrictMajority(t *testing.T) {
	p := types.Proposal{Messages: []*codectypes.Any{{TypeUrl: "/sparkdream.rep.v1.MsgForceMigration"}}}
	require.True(t, keeper.ProposalChangesMembership(p))

	hr := types.DecisionPolicy{PolicyType: "percentage", Threshold: "0.5"}
	one, two := math.LegacyNewDec(1), math.LegacyNewDec(2)
	require.False(t, keeper.ThresholdMet(hr, keeper.ProposalChangesMembership(p), one, two), "1 of 2 cannot force a migration")
	require.True(t, keeper.ThresholdMet(hr, keeper.ProposalChangesMembership(p), two, two))

	cancel := types.Proposal{Messages: []*codectypes.Any{{TypeUrl: "/sparkdream.rep.v1.MsgCancelMigration"}}}
	require.False(t, keeper.ProposalChangesMembership(cancel))
}

// CMN-105: deleting a group cancels its open polls, so a group later
// registered under the same name inherits none of them.
func TestCMN105_DeleteGroupCancelsOpenPolls(t *testing.T) {
	k, ctx, _, _ := setupCommonsKeeperWithSplit(t)
	_, members := setupBallotGroup(t, k, ctx, "Grants Desk", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 3, 3)
	openID, err := openPoll(k, ctx, members[0], "Grants Desk", false)
	require.NoError(t, err)
	parent := cosmosAddr("grants_desk_parent__")
	g, err := k.Groups.Get(ctx, "Grants Desk")
	require.NoError(t, err)
	g.ParentPolicyAddress = parent
	require.NoError(t, k.Groups.Set(ctx, "Grants Desk", g))

	_, err = keeper.NewMsgServerImpl(k).DeleteGroup(ctx, &types.MsgDeleteGroup{Authority: parent, GroupName: "Grants Desk"})
	require.NoError(t, err)

	poll, err := k.Polls.Get(ctx, openID)
	require.NoError(t, err)
	require.Equal(t, types.PollStatus_POLL_STATUS_CANCELLED, poll.Status)
	n := 0
	require.NoError(t, k.OpenPollsByCouncil.Walk(ctx, nil, func(collections.Pair[string, uint64]) (bool, error) { n++; return false, nil }))
	require.Zero(t, n, "no open poll left under the deleted name")
}

// CMN-107: the voter-tree snapshot charges gas per leaf for its hashing.
func TestCMN107_VoterTreeBuildIsMetered(t *testing.T) {
	gasFor := func(keyed int) uint64 {
		k, ctx, _ := setupCommonsKeeper(t)
		_, members := setupBallotGroup(t, k, ctx, "SecretPolls", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 8, keyed)
		ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		_, err := openPoll(k, ctx, members[0], "SecretPolls", true)
		require.NoError(t, err)
		return ctx.GasMeter().GasConsumed()
	}
	six, eight := gasFor(6), gasFor(8)
	require.GreaterOrEqual(t, eight-six, 2*keeper.VoterTreeGasPerLeaf, "two more leaves cost at least their hashing gas")
}

// zkDelayRep is an x/rep double that still reports a ZK key activation
// delay, as the removed voting-period cap read it.
type zkDelayRep struct{ seconds int64 }

func (r zkDelayRep) CanActAs(_ context.Context, signer, stored string) bool {
	return signer == stored
}
func (r zkDelayRep) ActiveZkPublicKey(context.Context, string) []byte { return nil }

// CMN-101: council voting periods are no longer capped by the ZK key
// activation delay. Secret ballots prove against a snapshot taken when they
// open, so a key change mid-window adds no voter; the cap protected nothing
// and, with every network's shipped delay (14 epochs of 14400 / 1440 / 300
// blocks), refused the bootstrap's own windows (5 days, 15 for the board).
func TestCMN101_VotingPeriodNotCappedByZkDelay(t *testing.T) {
	for net, delay := range map[string]int64{"mainnet": 14 * 14400 * 6, "testnet": 14 * 1440 * 6, "devnet": 14 * 300 * 6} {
		t.Run(net, func(t *testing.T) {
			k, ctx, _ := setupCommonsKeeper(t)
			require.NoError(t, k.Params.Set(ctx, types.NewParams("")))
			k.SetRepKeeper(zkDelayRep{seconds: delay})
			govAddr := k.GetModuleAddressByName(govtypes.ModuleName).String()
			spend := math.NewInt(1000)
			_, err := keeper.NewMsgServerImpl(k).RegisterGroup(ctx, &types.MsgRegisterGroup{
				Authority: govAddr, Name: "Zenith Board", Description: "board",
				Members:       []string{sdk.AccAddress([]byte("zenith_member_1_____")).String(), sdk.AccAddress([]byte("zenith_member_2_____")).String()},
				MemberWeights: []string{"1", "1"}, MinMembers: 2, MaxMembers: 5, TermDuration: 86400,
				VoteThreshold: testutil.DecPtr("0.5"), PolicyType: "percentage",
				VotingPeriod:     int64(keeper.WindowSupervisory.Seconds()),
				MaxSpendPerEpoch: &spend, UpdateCooldown: 3600,
			})
			require.NoError(t, err)
		})
	}
}
