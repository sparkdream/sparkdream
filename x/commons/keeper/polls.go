package keeper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Polls (spec §11.4): a non-binding, single-choice question put to a group's
// members for a duration the creator picks. Nothing executes. Votes are one
// member one vote and final; a secret-ballot poll is voted through x/shield
// against a voter snapshot exactly like a secret-ballot proposal.
const (
	MinPollOptions        = 2
	MaxPollOptions        = 10
	MaxPollQuestionLength = 500
	MaxPollOptionLength   = 100
	// MaxPollDurationSeconds is the longest a poll stays open (30 days).
	MaxPollDurationSeconds int64 = 30 * 24 * 3600
	// MaxOpenPollsPerGroup and MaxOpenPollsPerCreator bound the open polls
	// (polls are fee-free; each open one holds a queue entry and, if secret,
	// a voter snapshot).
	MaxOpenPollsPerGroup   = 20
	MaxOpenPollsPerCreator = 3
	// MaxPollOpensPerCreatorPerDay caps the polls one creator opens per UTC
	// day, counted at opening and never given back: the open-poll cap
	// alone resets on cancel, so an open/cancel loop could churn fee-free
	// secret polls (each a voter-tree build and a record kept for
	// PollRetention) without limit.
	MaxPollOpensPerCreatorPerDay = 3
	// MaxPollTransitionsPerBlock caps the PollQueue entries (closes and
	// prunes) EndBlock processes per block; the rest drain in order.
	MaxPollTransitionsPerBlock = 20
)

// validatePollContent checks a poll's question, options and duration.
func validatePollContent(question string, options []string, durationSeconds int64) error {
	if strings.TrimSpace(question) == "" || len(question) > MaxPollQuestionLength {
		return errorsmod.Wrapf(types.ErrInvalidPoll, "question must be 1-%d bytes", MaxPollQuestionLength)
	}
	if len(options) < MinPollOptions || len(options) > MaxPollOptions {
		return errorsmod.Wrapf(types.ErrInvalidPoll, "a poll needs %d-%d options, got %d", MinPollOptions, MaxPollOptions, len(options))
	}
	seen := make(map[string]bool, len(options))
	for i, o := range options {
		if strings.TrimSpace(o) == "" || len(o) > MaxPollOptionLength {
			return errorsmod.Wrapf(types.ErrInvalidPoll, "option %d must be 1-%d bytes", i, MaxPollOptionLength)
		}
		if seen[o] {
			return errorsmod.Wrapf(types.ErrInvalidPoll, "option %q appears twice", o)
		}
		seen[o] = true
	}
	minSeconds := int64(MinPollDuration.Seconds())
	if durationSeconds < minSeconds || durationSeconds > MaxPollDurationSeconds {
		return errorsmod.Wrapf(types.ErrInvalidPoll, "duration must be %d-%d seconds, got %d", minSeconds, MaxPollDurationSeconds, durationSeconds)
	}
	return nil
}

// checkOpenPollCaps refuses a new poll beyond MaxOpenPollsPerGroup open
// polls in the group or MaxOpenPollsPerCreator by the same member.
func (k Keeper) checkOpenPollCaps(ctx context.Context, councilName, creator string) error {
	var total, mine int
	err := k.OpenPollsByCouncil.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](councilName),
		func(key collections.Pair[string, uint64]) (bool, error) {
			total++
			if p, err := k.Polls.Get(ctx, key.K2()); err == nil && k.canActAs(ctx, creator, p.Creator) {
				mine++
			}
			return false, nil
		})
	if err != nil {
		return err
	}
	if total >= MaxOpenPollsPerGroup {
		return errorsmod.Wrapf(types.ErrPollLimit, "group %s already has %d open polls", councilName, total)
	}
	if mine >= MaxOpenPollsPerCreator {
		return errorsmod.Wrapf(types.ErrPollLimit, "you already have %d open polls in group %s", mine, councilName)
	}
	return nil
}

// recordPollOpen counts one poll opening against the creator's bucket for
// the current UTC day and refuses the opening past
// MaxPollOpensPerCreatorPerDay. The bucket is keyed by the signing address
// and reset when the day changes; cancelling a poll never decrements it.
func (k Keeper) recordPollOpen(ctx sdk.Context, creator string) error {
	day := ctx.BlockTime().Unix() / 86400
	bucket, err := k.PollOpenCounts.Get(ctx, creator)
	if err != nil {
		if !errors.Is(err, collections.ErrNotFound) {
			return err
		}
		bucket = types.PollOpenCount{Creator: creator}
	}
	if bucket.UtcDay != day {
		bucket = types.PollOpenCount{Creator: creator, UtcDay: day}
	}
	if bucket.Count >= MaxPollOpensPerCreatorPerDay {
		return errorsmod.Wrapf(types.ErrPollRateLimit, "%s already opened %d polls today (limit %d per UTC day)",
			creator, bucket.Count, MaxPollOpensPerCreatorPerDay)
	}
	bucket.Count++
	return k.PollOpenCounts.Set(ctx, creator, bucket)
}

// openPollForVoting loads a poll that is open and before its deadline.
func (k Keeper) openPollForVoting(ctx sdk.Context, pollID, option uint64) (types.Poll, error) {
	poll, err := k.Polls.Get(ctx, pollID)
	if err != nil {
		return types.Poll{}, errorsmod.Wrapf(types.ErrPollNotFound, "poll %d", pollID)
	}
	if poll.Status != types.PollStatus_POLL_STATUS_OPEN || ctx.BlockTime().Unix() > poll.Deadline {
		return types.Poll{}, errorsmod.Wrapf(types.ErrPollNotOpen, "poll %d", pollID)
	}
	if option >= uint64(len(poll.Options)) {
		return types.Poll{}, errorsmod.Wrapf(types.ErrInvalidPoll, "option %d out of range (poll has %d options)", option, len(poll.Options))
	}
	return poll, nil
}

// pollRetentionEnd is when a closed or cancelled poll is pruned.
func pollRetentionEnd(p types.Poll) int64 {
	return p.ClosedAt + int64(PollRetention.Seconds())
}

// pollQueueDue is the time a poll's PollQueue entry fires: its deadline
// while open, the end of its retention once closed or cancelled.
func pollQueueDue(p types.Poll) int64 {
	if p.Status == types.PollStatus_POLL_STATUS_OPEN {
		return p.Deadline
	}
	return pollRetentionEnd(p)
}

// endPoll moves an open poll to status (CLOSED or CANCELLED) now: it leaves
// the open index and its deadline queue entry, drops its voter snapshot and
// is queued for pruning after PollRetention.
func (k Keeper) endPoll(ctx sdk.Context, poll *types.Poll, status types.PollStatus) error {
	if err := k.PollQueue.Remove(ctx, collections.Join(poll.Deadline, poll.Id)); err != nil {
		return err
	}
	if err := k.OpenPollsByCouncil.Remove(ctx, collections.Join(poll.CouncilName, poll.Id)); err != nil {
		return err
	}
	if err := k.PollVoterTrees.Remove(ctx, poll.Id); err != nil {
		return err
	}
	poll.Status = status
	poll.ClosedAt = ctx.BlockTime().Unix()
	if err := k.Polls.Set(ctx, poll.Id, *poll); err != nil {
		return err
	}
	return k.PollQueue.Set(ctx, collections.Join(pollRetentionEnd(*poll), poll.Id))
}

// cancelOpenPollsOf cancels every open poll of councilName (MsgDeleteGroup).
func (k Keeper) cancelOpenPollsOf(ctx sdk.Context, councilName string) error {
	var ids []uint64
	if err := k.OpenPollsByCouncil.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](councilName),
		func(key collections.Pair[string, uint64]) (bool, error) {
			ids = append(ids, key.K2())
			return false, nil
		}); err != nil {
		return err
	}
	for _, id := range ids {
		poll, err := k.Polls.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := k.endPoll(ctx, &poll, types.PollStatus_POLL_STATUS_CANCELLED); err != nil {
			return err
		}
		ctx.EventManager().EmitEvent(pollEvent("poll_cancelled", id, sdk.NewAttribute("reason", "group deleted")))
	}
	return nil
}

// prunePoll deletes a poll past its retention with its open votes.
func (k Keeper) prunePoll(ctx context.Context, poll types.Poll) error {
	var voteKeys []collections.Pair[uint64, string]
	if err := k.PollVotes.Walk(ctx, collections.NewPrefixedPairRange[uint64, string](poll.Id),
		func(key collections.Pair[uint64, string], _ types.PollVote) (bool, error) {
			voteKeys = append(voteKeys, key)
			return false, nil
		}); err != nil {
		return err
	}
	for _, key := range voteKeys {
		if err := k.PollVotes.Remove(ctx, key); err != nil {
			return err
		}
	}
	if err := k.PollsByCouncil.Remove(ctx, collections.Join(poll.CouncilName, poll.Id)); err != nil {
		return err
	}
	return k.Polls.Remove(ctx, poll.Id)
}

// EndBlockPolls closes polls whose deadline has passed and prunes closed or
// cancelled polls past their retention, at most MaxPollTransitionsPerBlock
// queue entries per block, oldest first. Voting stays open through the
// deadline second, as for proposals.
func (k Keeper) EndBlockPolls(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now, uint64(0)))
	var due []collections.Pair[int64, uint64]
	if err := k.PollQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= MaxPollTransitionsPerBlock, nil
	}); err != nil {
		return err
	}

	for _, key := range due {
		poll, err := k.Polls.Get(ctx, key.K2())
		if err != nil || pollQueueDue(poll) != key.K1() {
			// Stale entry: the poll moved on or is gone.
			if err := k.PollQueue.Remove(ctx, key); err != nil {
				return err
			}
			continue
		}
		if poll.Status == types.PollStatus_POLL_STATUS_OPEN {
			if err := k.endPoll(sdkCtx, &poll, types.PollStatus_POLL_STATUS_CLOSED); err != nil {
				return err
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"poll_closed",
				sdk.NewAttribute("poll_id", strconv.FormatUint(poll.Id, 10)),
				sdk.NewAttribute("council_name", poll.CouncilName),
				sdk.NewAttribute("tallies", formatTallies(poll.Tallies)),
			))
			continue
		}
		if err := k.PollQueue.Remove(ctx, key); err != nil {
			return err
		}
		if err := k.prunePoll(ctx, poll); err != nil {
			return err
		}
	}
	return nil
}

func formatTallies(tallies []uint64) string {
	parts := make([]string, len(tallies))
	for i, t := range tallies {
		parts[i] = strconv.FormatUint(t, 10)
	}
	return strings.Join(parts, ",")
}

// pollVoteTotal is the number of votes cast on a poll.
func pollVoteTotal(p types.Poll) uint64 {
	var n uint64
	for _, t := range p.Tallies {
		n += t
	}
	return n
}

// pollEvent builds a poll event with the poll ID.
func pollEvent(kind string, pollID uint64, attrs ...sdk.Attribute) sdk.Event {
	return sdk.NewEvent(kind, append([]sdk.Attribute{sdk.NewAttribute("poll_id", fmt.Sprintf("%d", pollID))}, attrs...)...)
}
