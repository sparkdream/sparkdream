package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
)

type fixedDelayRep struct{ seconds int64 }

func (r fixedDelayRep) ZkKeyActivationDelaySeconds(context.Context) int64 { return r.seconds }
func (r fixedDelayRep) CanActAs(_ context.Context, signer, stored string) bool {
	return signer == stored
}

// TestMigrationHooks_SeatAndVoteFollowIdentity: a council seat and a vote on
// an open proposal move with the member; a vote on a closed proposal stays.
func TestMigrationHooks_SeatAndVoteFollowIdentity(t *testing.T) {
	f := initFixture(t)
	from := sdk.AccAddress([]byte("seat-from__________"))
	to := sdk.AccAddress([]byte("seat-to____________"))
	council := "Phoenix Council"
	require.NoError(t, f.keeper.Groups.Set(f.ctx, council, types.Group{PolicyAddress: "policy"}))
	require.NoError(t, f.keeper.Members.Set(f.ctx, collections.Join(council, from.String()), types.Member{Address: from.String(), Weight: "1", AddedAt: 42}))
	require.NoError(t, f.keeper.Proposals.Set(f.ctx, 7, types.Proposal{Id: 7, CouncilName: council, Status: types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED}))
	require.NoError(t, f.keeper.Proposals.Set(f.ctx, 8, types.Proposal{Id: 8, CouncilName: council, Status: types.ProposalStatus_PROPOSAL_STATUS_EXECUTED}))
	require.NoError(t, f.keeper.ProposalsByCouncil.Set(f.ctx, collections.Join(council, uint64(7))))
	require.NoError(t, f.keeper.ProposalsByCouncil.Set(f.ctx, collections.Join(council, uint64(8))))
	require.NoError(t, f.keeper.Votes.Set(f.ctx, collections.Join(uint64(7), from.String()), types.Vote{Voter: from.String()}))
	require.NoError(t, f.keeper.Votes.Set(f.ctx, collections.Join(uint64(8), from.String()), types.Vote{Voter: from.String()}))

	h := keeper.NewMigrationHooks(&f.keeper)
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))

	_, err := f.keeper.Members.Get(f.ctx, collections.Join(council, from.String()))
	require.Error(t, err)
	seat, err := f.keeper.Members.Get(f.ctx, collections.Join(council, to.String()))
	require.NoError(t, err)
	require.Equal(t, to.String(), seat.Address)
	require.Equal(t, int64(42), seat.AddedAt, "added_at is kept: no electoral rate limit flapping")

	_, err = f.keeper.Votes.Get(f.ctx, collections.Join(uint64(7), from.String()))
	require.Error(t, err)
	moved, err := f.keeper.Votes.Get(f.ctx, collections.Join(uint64(7), to.String()))
	require.NoError(t, err)
	require.Equal(t, to.String(), moved.Voter)
	_, err = f.keeper.Votes.Get(f.ctx, collections.Join(uint64(8), from.String()))
	require.NoError(t, err, "votes on closed proposals stay lazy")
}

// TestMaxLiveVotingPeriodAndCap: the longest live voting period is reported,
// and a registration above the ZK activation delay is refused once a rep
// keeper is wired.
func TestMaxLiveVotingPeriodAndCap(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.DecisionPolicies.Set(f.ctx, "p1", types.DecisionPolicy{VotingPeriod: 100}))
	require.NoError(t, f.keeper.DecisionPolicies.Set(f.ctx, "p2", types.DecisionPolicy{VotingPeriod: 3600}))
	require.Equal(t, int64(3600), f.keeper.MaxLiveVotingPeriodSeconds(f.ctx))

	require.NoError(t, f.keeper.CheckVotingPeriodCapForTest(f.ctx, 999999), "no rep keeper: no cap")
	f.keeper.SetRepKeeper(fixedDelayRep{seconds: 1000})
	require.NoError(t, f.keeper.CheckVotingPeriodCapForTest(f.ctx, 1000))
	require.Error(t, f.keeper.CheckVotingPeriodCapForTest(f.ctx, 1001))
	f.keeper.SetRepKeeper(fixedDelayRep{seconds: 0})
	require.NoError(t, f.keeper.CheckVotingPeriodCapForTest(f.ctx, 1001), "a zero delay means the cap is off (test build)")
}
