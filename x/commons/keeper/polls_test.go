package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

func minPollSeconds() int64 { return int64(keeper.MinPollDuration.Seconds()) }

func openPoll(k keeper.Keeper, ctx sdk.Context, creator, group string, secret bool, options ...string) (uint64, error) {
	if len(options) == 0 {
		options = []string{"aurora", "zenith", "phoenix"}
	}
	resp, err := keeper.NewMsgServerImpl(k).OpenPoll(ctx, &types.MsgOpenPoll{
		Creator: creator, CouncilName: group, Question: "Which name?",
		Options: options, DurationSeconds: minPollSeconds(), SecretBallot: secret,
	})
	if err != nil {
		return 0, err
	}
	return resp.PollId, nil
}

func votePoll(k keeper.Keeper, ctx sdk.Context, voter string, pollID, option uint64) error {
	_, err := keeper.NewMsgServerImpl(k).VotePoll(ctx, &types.MsgVotePoll{Voter: voter, PollId: pollID, Option: option})
	return err
}

// An open poll takes one final vote per member, closes at its deadline and
// is pruned with its votes after the retention window.
func TestPoll_OpenVoteCloseAndPrune(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "PollCouncil", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 3, 0)

	id, err := openPoll(k, ctx, members[0], "PollCouncil", false)
	require.NoError(t, err)
	require.Equal(t, uint64(1), id, "poll IDs start at 1")
	poll, err := k.Polls.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.PollStatus_POLL_STATUS_OPEN, poll.Status)
	require.Equal(t, uint64(3), poll.EligibleVoters)
	require.Equal(t, []uint64{0, 0, 0}, poll.Tallies)

	require.NoError(t, votePoll(k, ctx, members[0], id, 1))
	require.NoError(t, votePoll(k, ctx, members[1], id, 1))
	require.ErrorIs(t, votePoll(k, ctx, members[0], id, 2), types.ErrAlreadyVoted, "votes are final")
	require.ErrorIs(t, votePoll(k, ctx, members[2], id, 3), types.ErrInvalidPoll, "option out of range")
	outsider := cosmosAddr("poll_outsider_______")
	require.ErrorIs(t, votePoll(k, ctx, outsider, id, 0), sdkerrors.ErrUnauthorized)

	poll, _ = k.Polls.Get(ctx, id)
	require.Equal(t, []uint64{0, 2, 0}, poll.Tallies)
	votes, err := keeper.NewQueryServerImpl(k).GetPollVotes(ctx, &types.QueryGetPollVotesRequest{PollId: id})
	require.NoError(t, err)
	require.Len(t, votes.Votes, 2)

	// Voting stays open through the deadline second.
	atDeadline := ctx.WithBlockTime(time.Unix(poll.Deadline, 0))
	require.NoError(t, k.EndBlockPolls(atDeadline))
	require.NoError(t, votePoll(k, atDeadline, members[2], id, 0))

	closed := ctx.WithBlockTime(time.Unix(poll.Deadline+1, 0))
	require.NoError(t, k.EndBlockPolls(closed))
	poll, _ = k.Polls.Get(closed, id)
	require.Equal(t, types.PollStatus_POLL_STATUS_CLOSED, poll.Status)
	require.Equal(t, []uint64{1, 2, 0}, poll.Tallies)
	require.ErrorIs(t, votePoll(k, closed, members[0], id, 0), types.ErrPollNotOpen)

	open, err := keeper.NewQueryServerImpl(k).ListPolls(closed, &types.QueryListPollsRequest{
		CouncilName: "PollCouncil", Status: types.PollStatus_POLL_STATUS_OPEN,
	})
	require.NoError(t, err)
	require.Empty(t, open.Polls)

	pruned := ctx.WithBlockTime(time.Unix(poll.ClosedAt+int64(keeper.PollRetention.Seconds())+1, 0))
	require.NoError(t, k.EndBlockPolls(pruned))
	has, _ := k.Polls.Has(pruned, id)
	require.False(t, has)
	votes, err = keeper.NewQueryServerImpl(k).GetPollVotes(pruned, &types.QueryGetPollVotesRequest{PollId: id})
	require.NoError(t, err)
	require.Empty(t, votes.Votes)
}

// A member whose key migrated cannot vote again under the new address.
func TestPoll_MigratedKeyCannotVoteTwice(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "MigCouncil", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 2, 0)
	id, err := openPoll(k, ctx, members[0], "MigCouncil", false)
	require.NoError(t, err)
	require.NoError(t, votePoll(k, ctx, members[1], id, 0))

	moved := cosmosAddr("migrated_member_____")
	k.SetRepKeeper(keyRep{alias: map[string]string{members[1]: moved}})
	require.NoError(t, k.AddMember(ctx, "MigCouncil", types.Member{Address: moved, Weight: "1"}))
	require.ErrorIs(t, votePoll(k, ctx, moved, id, 1), types.ErrAlreadyVoted)
}

func TestPoll_Validation(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "Validating", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 1, 0)
	ms := keeper.NewMsgServerImpl(k)
	open := func(question string, options []string, duration int64) error {
		_, err := ms.OpenPoll(ctx, &types.MsgOpenPoll{
			Creator: members[0], CouncilName: "Validating", Question: question, Options: options, DurationSeconds: duration,
		})
		return err
	}
	two := []string{"yes", "no"}
	require.ErrorIs(t, open("", two, minPollSeconds()), types.ErrInvalidPoll)
	require.ErrorIs(t, open("q", []string{"only"}, minPollSeconds()), types.ErrInvalidPoll)
	require.ErrorIs(t, open("q", []string{"a", "a"}, minPollSeconds()), types.ErrInvalidPoll)
	require.ErrorIs(t, open("q", []string{"a", ""}, minPollSeconds()), types.ErrInvalidPoll)
	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = fmt.Sprintf("option-%d", i)
	}
	require.ErrorIs(t, open("q", eleven, minPollSeconds()), types.ErrInvalidPoll)
	require.ErrorIs(t, open("q", two, minPollSeconds()-1), types.ErrInvalidPoll)
	require.ErrorIs(t, open("q", two, keeper.MaxPollDurationSeconds+1), types.ErrInvalidPoll)
	require.NoError(t, open("q", two, keeper.MaxPollDurationSeconds))

	outsider := cosmosAddr("poll_outsider_______")
	_, err := ms.OpenPoll(ctx, &types.MsgOpenPoll{
		Creator: outsider, CouncilName: "Validating", Question: "q", Options: two, DurationSeconds: minPollSeconds(),
	})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
}

func TestPoll_OpenPollCaps(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "Capped", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 8, 0)
	opened := 0
	for _, m := range members {
		for i := 0; i < keeper.MaxOpenPollsPerCreator; i++ {
			if opened == keeper.MaxOpenPollsPerGroup {
				break
			}
			_, err := openPoll(k, ctx, m, "Capped", false)
			require.NoError(t, err)
			opened++
		}
		if opened < keeper.MaxOpenPollsPerGroup {
			_, err := openPoll(k, ctx, m, "Capped", false)
			require.ErrorIs(t, err, types.ErrPollLimit, "per-creator cap")
		}
	}
	_, err := openPoll(k, ctx, members[len(members)-1], "Capped", false)
	require.ErrorIs(t, err, types.ErrPollLimit, "per-group cap")
}

func TestPoll_Cancel(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "Cancelling", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 2, 0)
	ms := keeper.NewMsgServerImpl(k)

	id, err := openPoll(k, ctx, members[0], "Cancelling", false)
	require.NoError(t, err)
	_, err = ms.CancelPoll(ctx, &types.MsgCancelPoll{Creator: members[1], PollId: id})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	_, err = ms.CancelPoll(ctx, &types.MsgCancelPoll{Creator: members[0], PollId: id})
	require.NoError(t, err)
	poll, _ := k.Polls.Get(ctx, id)
	require.Equal(t, types.PollStatus_POLL_STATUS_CANCELLED, poll.Status)
	require.ErrorIs(t, votePoll(k, ctx, members[1], id, 0), types.ErrPollNotOpen)

	voted, err := openPoll(k, ctx, members[0], "Cancelling", false)
	require.NoError(t, err)
	require.NoError(t, votePoll(k, ctx, members[1], voted, 0))
	_, err = ms.CancelPoll(ctx, &types.MsgCancelPoll{Creator: members[0], PollId: voted})
	require.ErrorIs(t, err, types.ErrPollHasVotes)
}

// A secret-ballot poll snapshots its voters, refuses open votes and counts
// anonymous ones; the snapshot goes when the poll closes.
func TestPoll_SecretBallot(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "SecretPolls", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 5, 5)
	ms := keeper.NewMsgServerImpl(k)

	id, err := openPoll(k, ctx, members[0], "SecretPolls", true)
	require.NoError(t, err)
	poll, _ := k.Polls.Get(ctx, id)
	require.True(t, poll.SecretBallot)
	require.Equal(t, uint64(5), poll.EligibleVoters)
	root, err := k.ShieldMerkleRoot(ctx, &types.MsgAnonymousVotePoll{PollId: id})
	require.NoError(t, err)
	require.Equal(t, poll.VoterRoot, root)

	require.ErrorIs(t, votePoll(k, ctx, members[0], id, 0), types.ErrBallotMode)
	_, err = ms.AnonymousVotePoll(ctx, &types.MsgAnonymousVotePoll{Voter: members[0], PollId: id, Option: 0})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized, "only x/shield casts anonymous votes")
	_, err = ms.AnonymousVotePoll(ctx, &types.MsgAnonymousVotePoll{Voter: shieldModuleAddr(), PollId: id, Option: 2})
	require.NoError(t, err)
	poll, _ = k.Polls.Get(ctx, id)
	require.Equal(t, []uint64{0, 0, 1}, poll.Tallies)

	closed := ctx.WithBlockTime(time.Unix(poll.Deadline+1, 0))
	require.NoError(t, k.EndBlockPolls(closed))
	has, _ := k.PollVoterTrees.Has(closed, id)
	require.False(t, has)
	_, err = k.ShieldMerkleRoot(closed, &types.MsgAnonymousVotePoll{PollId: id})
	require.ErrorIs(t, err, types.ErrBallotMode)

	// An open poll has no snapshot to prove against.
	openID, err := openPoll(k, ctx, members[1], "SecretPolls", false)
	require.NoError(t, err)
	_, err = ms.AnonymousVotePoll(ctx, &types.MsgAnonymousVotePoll{Voter: shieldModuleAddr(), PollId: openID, Option: 0})
	require.ErrorIs(t, err, types.ErrBallotMode)
}

// Polls, votes, the sequence and open snapshots survive export and import;
// the indexes and queue are rebuilt, so the imported poll still closes.
func TestPoll_GenesisRoundTrip(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	_, members := setupBallotGroup(t, k, ctx, "RoundTrip", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 5, 5)
	open, err := openPoll(k, ctx, members[0], "RoundTrip", false)
	require.NoError(t, err)
	require.NoError(t, votePoll(k, ctx, members[1], open, 1))
	secret, err := openPoll(k, ctx, members[1], "RoundTrip", true)
	require.NoError(t, err)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	gs, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NoError(t, gs.Validate())
	require.Len(t, gs.Polls, 2)
	require.Len(t, gs.PollVotes, 1)
	require.Len(t, gs.PollVoterTrees, 1)
	require.Equal(t, uint64(3), gs.NextPollId)

	k2, ctx2, _ := setupCommonsKeeper(t)
	require.NoError(t, k2.InitGenesis(ctx2, *gs))
	k2.SetRepKeeper(keyRep{})
	gs2, err := k2.ExportGenesis(ctx2)
	require.NoError(t, err)
	require.Equal(t, gs.Polls, gs2.Polls)
	require.Equal(t, gs.PollVotes, gs2.PollVotes)
	require.Equal(t, gs.PollVoterTrees, gs2.PollVoterTrees)
	require.Equal(t, gs.NextPollId, gs2.NextPollId)

	p, _ := k2.Polls.Get(ctx2, secret)
	require.NoError(t, k2.EndBlockPolls(ctx2.WithBlockTime(time.Unix(p.Deadline+1, 0))))
	p, _ = k2.Polls.Get(ctx2, secret)
	require.Equal(t, types.PollStatus_POLL_STATUS_CLOSED, p.Status)

	bad := *gs
	bad.PollVotes = append(bad.PollVotes, types.PollVote{PollId: secret, Voter: members[2], Option: 0})
	require.Error(t, bad.Validate(), "no open vote on a secret poll")
}

// CMN-107: the open-poll cap resets on cancel, so openings are also counted
// per creator per UTC day. Three opens succeed, an open/cancel loop cannot
// get a fourth that day, and the next day starts a fresh bucket.
func TestCMN107_PollOpenRateLimit(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)
	// 01:00 UTC, so a six-hour step stays inside the day.
	ctx = ctx.WithBlockTime(time.Unix(1_700_006_400+3600, 0))
	_, members := setupBallotGroup(t, k, ctx, "ChurnCouncil", types.BallotMode_BALLOT_MODE_UNSPECIFIED, 2, 0)
	creator, other := members[0], members[1]
	ms := keeper.NewMsgServerImpl(k)
	cancel := func(ctx sdk.Context, id uint64) {
		_, err := ms.CancelPoll(ctx, &types.MsgCancelPoll{Creator: creator, PollId: id})
		require.NoError(t, err)
	}

	// Open, cancel, open, cancel, open: never more than one open at a time,
	// so the open-poll cap (3) is never the limit.
	var last uint64
	for i := 0; i < keeper.MaxPollOpensPerCreatorPerDay; i++ {
		if last != 0 {
			cancel(ctx, last)
		}
		id, err := openPoll(k, ctx, creator, "ChurnCouncil", false)
		require.NoError(t, err, "open %d", i+1)
		last = id
	}
	cancel(ctx, last)
	open, err := k.OpenPollsByCouncil.Has(ctx, collections.Join("ChurnCouncil", last))
	require.NoError(t, err)
	require.False(t, open, "nothing open, the cap alone would allow a fourth")
	_, err = openPoll(k, ctx, creator, "ChurnCouncil", false)
	require.ErrorIs(t, err, types.ErrPollRateLimit, "the fourth opening of the day is refused")
	bucket, err := k.PollOpenCounts.Get(ctx, creator)
	require.NoError(t, err)
	require.Equal(t, uint64(keeper.MaxPollOpensPerCreatorPerDay), bucket.Count)
	require.Equal(t, ctx.BlockTime().Unix()/86400, bucket.UtcDay)

	// Another member has a bucket of their own.
	_, err = openPoll(k, ctx, other, "ChurnCouncil", false)
	require.NoError(t, err)

	// Later the same UTC day: still refused. The next day: allowed again.
	sameDay := ctx.WithBlockTime(ctx.BlockTime().Add(6 * time.Hour))
	require.Equal(t, ctx.BlockTime().Unix()/86400, sameDay.BlockTime().Unix()/86400)
	_, err = openPoll(k, sameDay, creator, "ChurnCouncil", false)
	require.ErrorIs(t, err, types.ErrPollRateLimit)
	nextDay := ctx.WithBlockTime(ctx.BlockTime().Add(24 * time.Hour))
	_, err = openPoll(k, nextDay, creator, "ChurnCouncil", false)
	require.NoError(t, err, "a new UTC day starts a fresh bucket")
	bucket, err = k.PollOpenCounts.Get(nextDay, creator)
	require.NoError(t, err)
	require.Equal(t, uint64(1), bucket.Count)
	require.Equal(t, nextDay.BlockTime().Unix()/86400, bucket.UtcDay)

	// A refused opening leaves no poll behind.
	polls, err := keeper.NewQueryServerImpl(k).ListPolls(nextDay, &types.QueryListPollsRequest{CouncilName: "ChurnCouncil"})
	require.NoError(t, err)
	require.Len(t, polls.Polls, keeper.MaxPollOpensPerCreatorPerDay+2)
}
