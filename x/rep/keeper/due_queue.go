package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Due queues for the per-block EndBlocker sweeps (REP-E-10).
//
// The completion sweep over IN_REVIEW initiatives and the auto-uphold sweeps
// over ACTIVE challenges and content challenges used to load every live item
// on every block. Each now reads a (due height, id) queue up to the current
// height, at most maxDueSweepPerBlock entries per queue per block; what is
// left over is due on the next block, in deadline order.
//
// Entries are hints. The status-index helpers write one whenever an item
// enters its live status (which also rebuilds the queues at InitGenesis), at
// the item's deadline when the record carries it and at height 0 otherwise.
// The sweep checks the record: an item that left the live status is dropped,
// one whose deadline is later than the entry is re-queued at the deadline,
// and one whose processing fails is retried on the next block. A retry is
// re-keyed at the next height, behind every item already due, so failing
// items cannot hold the head of the queue and starve the rest.
const (
	maxDueSweepPerBlock = 100
	dueRetryBlocks      = int64(1)
)

// dueOutcome is what a sweep callback decided for one queue entry.
type dueOutcome struct {
	requeueAt int64 // > 0: put the item back at this height
}

var dueDone = dueOutcome{}

func dueAt(height int64) dueOutcome { return dueOutcome{requeueAt: height} }

// drainDueQueue collects at most maxDueSweepPerBlock entries due at or before
// height, removes each and hands its id to fn, then re-queues it if fn asks.
func (k Keeper) drainDueQueue(ctx context.Context, q collections.KeySet[collections.Pair[int64, uint64]], name string, fn func(id uint64) dueOutcome) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](height+1, 0))
	var due []collections.Pair[int64, uint64]
	if err := q.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= maxDueSweepPerBlock, nil
	}); err != nil {
		sdkCtx.Logger().Error("failed to walk due queue", "queue", name, "error", err)
		return
	}
	for _, key := range due {
		if err := q.Remove(ctx, key); err != nil {
			sdkCtx.Logger().Error("failed to dequeue due item", "queue", name, "id", key.K2(), "error", err)
			continue
		}
		out := fn(key.K2())
		if out.requeueAt > 0 {
			at := out.requeueAt
			if at <= height {
				at = height + 1
			}
			if err := q.Set(ctx, collections.Join(at, key.K2())); err != nil {
				sdkCtx.Logger().Error("failed to re-queue due item", "queue", name, "id", key.K2(), "error", err)
			}
		}
	}
}

// enqueueDue writes a hint entry; a deadline below 0 is clamped to 0.
func enqueueDue(ctx context.Context, q collections.KeySet[collections.Pair[int64, uint64]], deadline int64, id uint64) error {
	if deadline < 0 {
		deadline = 0
	}
	return q.Set(ctx, collections.Join(deadline, id))
}

// SweepDueInitiativeCompletions completes IN_REVIEW initiatives whose
// challenge period ended (EndBlocker step 3).
//
// An IN_REVIEW initiative whose ChallengePeriodEnd is still 0 never had its
// window opened (legacy state, or a pre-snapshot rejected challenge). It must
// not complete vacuously, and must not freeze either: it is adopted by opening
// a full window now, the policy the review-gate sweep applies to work that
// came under its gate after submission.
func (k Keeper) SweepDueInitiativeCompletions(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	k.drainDueQueue(ctx, k.InitiativeCompletionDue, "initiative_completion", func(id uint64) dueOutcome {
		initiative, err := k.Initiative.Get(ctx, id)
		if err != nil || initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW {
			return dueDone
		}
		if initiative.ChallengePeriodEnd == 0 {
			if err := k.TransitionToChallengePeriod(ctx, id); err != nil {
				sdkCtx.Logger().Error("failed to open challenge window for adopted initiative",
					"initiative_id", id, "error", err)
				return dueAt(height + dueRetryBlocks)
			}
			adopted, err := k.GetInitiative(ctx, id)
			if err != nil {
				return dueDone
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"initiative_challenge_window_opened",
				sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", id)),
				sdk.NewAttribute("reason", "window_missing_at_completion_sweep"),
				sdk.NewAttribute("challenge_period_end", fmt.Sprintf("%d", adopted.ChallengePeriodEnd)),
			))
			return dueAt(adopted.ChallengePeriodEnd)
		}
		if height < initiative.ChallengePeriodEnd {
			return dueAt(initiative.ChallengePeriodEnd)
		}
		// Skip payout for initiatives whose parent project was cancelled after
		// they entered review: CompleteInitiative would reject them anyway.
		// The cancel cascade closes them and returns the assignee's bond.
		if project, perr := k.GetProject(ctx, initiative.ProjectId); perr == nil &&
			project.Status == types.ProjectStatus_PROJECT_STATUS_CANCELLED {
			return dueAt(height + dueRetryBlocks)
		}
		// CompleteInitiative mints to the completer, the treasury, and every
		// staker before it deletes the stake records, and it is not
		// idempotent. EndBlocker writes straight to the deliver state, so a
		// mid-function error would persist the mints already made and leave
		// the initiative to be retried, paying them twice. Run it in a child
		// cache context and only commit on success.
		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.CompleteInitiative(cacheCtx, id); err != nil {
			sdkCtx.Logger().Error("failed to complete initiative", "initiative_id", id, "error", err)
			return dueAt(height + dueRetryBlocks)
		}
		writeCache()
		return dueDone
	})
}

// SweepDueChallengeResponses auto-upholds ACTIVE challenges whose assignee did
// not respond by the response deadline (EndBlocker step 5).
func (k Keeper) SweepDueChallengeResponses(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	k.drainDueQueue(ctx, k.ChallengeResponseDue, "challenge_response", func(id uint64) dueOutcome {
		challenge, err := k.Challenge.Get(ctx, id)
		if err != nil || challenge.Status != types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE || challenge.ResponseDeadline <= 0 {
			return dueDone
		}
		if height < challenge.ResponseDeadline {
			return dueAt(challenge.ResponseDeadline)
		}
		// UpholdChallenge marks the challenge resolved before it releases the
		// initiative's stakes. On deliver state a mid-function error would
		// persist the resolution and strand the transition half-done, so it
		// runs in a child cache context and commits only on success.
		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.UpholdConcededChallenge(cacheCtx, id); err != nil {
			sdkCtx.Logger().Error("failed to uphold challenge", "challenge_id", id, "error", err)
			return dueAt(height + dueRetryBlocks)
		}
		writeCache()
		return dueDone
	})
}

// SweepDueContentChallengeResponses auto-upholds ACTIVE content challenges
// whose author did not respond by the response deadline (EndBlocker step 5b).
func (k Keeper) SweepDueContentChallengeResponses(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	k.drainDueQueue(ctx, k.ContentChallengeResponseDue, "content_challenge_response", func(id uint64) dueOutcome {
		cc, err := k.ContentChallenge.Get(ctx, id)
		if err != nil || cc.Status != types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE || cc.ResponseDeadline <= 0 {
			return dueDone
		}
		if height < cc.ResponseDeadline {
			return dueAt(cc.ResponseDeadline)
		}
		// UpholdContentChallenge burns the author's bond and deletes the bond
		// stake before it mints the challenger's reward and only flips Status
		// last; contained like the step above.
		cacheCtx, writeCache := sdkCtx.CacheContext()
		if err := k.UpholdContentChallenge(cacheCtx, id); err != nil {
			sdkCtx.Logger().Error("failed to uphold content challenge", "content_challenge_id", id, "error", err)
			return dueAt(height + dueRetryBlocks)
		}
		writeCache()
		return dueDone
	})
}
