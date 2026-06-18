package keeper

import (
	"context"
	"fmt"
	"math"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// getAuthorLastActive returns the thread author's most recent forum activity
// timestamp (last post/reply time), used by the auto-confirm EndBlocker to
// decide whether an unanswered proposal is being ignored (auto-confirm) or the
// author simply appears absent (grant the one-time inactivity extension).
// Forum-local — no x/rep dependency. 0 when the author has no rate-limit record.
func (k Keeper) getAuthorLastActive(ctx context.Context, author string) int64 {
	rl, err := k.UserRateLimit.Get(ctx, author)
	if err != nil {
		return 0
	}
	return rl.LastPostTime
}

// enqueueProposalAutoConfirm records a pending proposal in the time-ordered
// auto-confirm queue and stamps the exact fire_at on the metadata so the entry
// can later be removed precisely. The caller is responsible for Set-ing the
// metadata afterwards.
func (k Keeper) enqueueProposalAutoConfirm(ctx context.Context, metadata *types.ThreadMetadata, fireAt int64) error {
	metadata.ProposalFireAt = fireAt
	return k.ProposalAutoConfirmQueue.Set(ctx, collections.Join(fireAt, metadata.ThreadId))
}

// dequeueProposalAutoConfirm removes the current proposal's queue entry (if any)
// using the stamped fire_at. Safe to call when no proposal is pending.
func (k Keeper) dequeueProposalAutoConfirm(ctx context.Context, metadata *types.ThreadMetadata) {
	if metadata.ProposalFireAt != 0 {
		_ = k.ProposalAutoConfirmQueue.Remove(ctx, collections.Join(metadata.ProposalFireAt, metadata.ThreadId))
		metadata.ProposalFireAt = 0
	}
}

// confirmCurationProposal applies a pending sentinel accepted-reply proposal:
// promotes the proposed reply to accepted (credited to the proposing sentinel),
// increments the sentinel's confirmed_proposals + epoch_curations counters,
// mints the curation DREAM reward, clears the proposal fields, and removes the
// auto-confirm queue entry. Shared by MsgConfirmProposedReply and the EndBlocker
// auto-confirm pass. Caller must persist the returned metadata.
//
// The proposal must be pending (ProposedReplyId != 0) and the thread must not
// already have an accepted reply — callers validate this before calling.
func (k Keeper) confirmCurationProposal(ctx context.Context, metadata *types.ThreadMetadata, now int64, autoConfirmed bool) error {
	sentinel := metadata.ProposedBy
	acceptedReplyID := metadata.ProposedReplyId

	// Promote proposed -> accepted, crediting the proposing sentinel.
	metadata.AcceptedReplyId = acceptedReplyID
	metadata.AcceptedBy = sentinel
	metadata.AcceptedAt = now

	// Clear proposal state and drain the queue.
	k.dequeueProposalAutoConfirm(ctx, metadata)
	metadata.ProposedReplyId = 0
	metadata.ProposedBy = ""
	metadata.ProposedAt = 0
	metadata.ProposalExtended = false

	// Sentinel curation counters: confirmed_proposals is lifetime,
	// epoch_curations feeds the per-epoch reward score (reset each epoch).
	local, err := k.SentinelActivity.Get(ctx, sentinel)
	if err != nil {
		local = types.SentinelActivity{Address: sentinel}
	}
	local.ConfirmedProposals++
	local.EpochCurations++
	if err := k.SentinelActivity.Set(ctx, sentinel, local); err != nil {
		return errorsmod.Wrap(err, "failed to update sentinel activity")
	}
	if k.repKeeper != nil {
		_ = k.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinel)
	}

	// Mint the curation DREAM reward to the proposing sentinel.
	if err := k.payCurationReward(ctx, sentinel); err != nil {
		return err
	}

	eventType := "proposed_reply_confirmed"
	if autoConfirmed {
		eventType = "proposed_reply_auto_confirmed"
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", metadata.ThreadId)),
			sdk.NewAttribute("reply_id", fmt.Sprintf("%d", acceptedReplyID)),
			sdk.NewAttribute("proposed_by", sentinel),
		),
	)
	return nil
}

// ProcessProposalAutoConfirm walks the auto-confirm queue for entries whose
// fire_at has elapsed and, for each still-pending proposal, either grants the
// one-time author-inactivity extension or auto-confirms it. Bounded per block by
// maxAutoConfirmPerBlock. Stale entries (no live proposal, or a fire_at that no
// longer matches the metadata) are dropped.
func (k Keeper) ProcessProposalAutoConfirm(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, perr := k.Params.Get(ctx)
	if perr != nil {
		params = types.DefaultParams()
	}
	timeout := params.AcceptProposalTimeout

	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(math.MaxUint64)))

	processed := 0
	return k.ProposalAutoConfirmQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if processed >= maxAutoConfirmPerBlock {
			return true, nil // stop
		}
		fireAt, threadID := key.K1(), key.K2()

		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		// Stale: thread gone, no pending proposal, or fire_at superseded.
		if err != nil || metadata.ProposedReplyId == 0 || metadata.ProposalFireAt != fireAt {
			_ = k.ProposalAutoConfirmQueue.Remove(ctx, key)
			processed++
			return false, nil
		}

		// Determine the thread author for the activity check.
		thread, terr := k.Post.Get(ctx, threadID)
		if terr != nil {
			// Thread vanished — clear the proposal without reward.
			k.dequeueProposalAutoConfirm(ctx, &metadata)
			metadata.ProposedReplyId = 0
			metadata.ProposedBy = ""
			metadata.ProposedAt = 0
			metadata.ProposalExtended = false
			_ = k.ThreadMetadata.Set(ctx, threadID, metadata)
			processed++
			return false, nil
		}

		// Author-inactivity extension: if the author has shown no forum activity
		// since the proposal was submitted and has not already been granted the
		// grace window, extend once instead of auto-confirming.
		lastActive := k.getAuthorLastActive(ctx, thread.Author)
		if !metadata.ProposalExtended && lastActive <= metadata.ProposedAt {
			k.dequeueProposalAutoConfirm(ctx, &metadata)
			metadata.ProposalExtended = true
			if err := k.enqueueProposalAutoConfirm(ctx, &metadata, now+timeout); err != nil {
				return false, err
			}
			if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
				return false, err
			}
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"accept_proposal_extended",
					sdk.NewAttribute("thread_id", fmt.Sprintf("%d", threadID)),
					sdk.NewAttribute("reason", "author_inactive"),
				),
			)
			processed++
			return false, nil
		}

		// Auto-confirm: same path as the manual confirm.
		if err := k.confirmCurationProposal(ctx, &metadata, now, true); err != nil {
			sdkCtx.Logger().Error("auto-confirm failed", "thread_id", threadID, "error", err)
			// confirmCurationProposal mutated `metadata` in memory but we never
			// persist it on error. Reload the stored copy and clear just the
			// proposal fields so the thread is freed (not wedged with a dangling
			// pending proposal that blocks future proposals and never resolves),
			// then drop the queue entry. accepted_reply_id is left untouched.
			if fresh, gerr := k.ThreadMetadata.Get(ctx, threadID); gerr == nil {
				fresh.ProposedReplyId = 0
				fresh.ProposedBy = ""
				fresh.ProposedAt = 0
				fresh.ProposalExtended = false
				fresh.ProposalFireAt = 0
				_ = k.ThreadMetadata.Set(ctx, threadID, fresh)
			}
			_ = k.ProposalAutoConfirmQueue.Remove(ctx, key)
			processed++
			return false, nil
		}
		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return false, err
		}
		processed++
		return false, nil
	})
}

// payCurationReward mints the configured curation DREAM reward to the sentinel.
func (k Keeper) payCurationReward(ctx context.Context, sentinel string) error {
	if k.repKeeper == nil {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		params = types.DefaultParams()
	}
	// Read the param directly: a non-positive reward means curation rewards are
	// disabled (mirrors the IsPositive fee guards in x/rep / x/blog). The value
	// is seeded by DefaultParams at genesis, so there is no unset/default case.
	reward := params.CurationDreamReward
	if reward.IsNil() || !reward.IsPositive() {
		return nil
	}
	addr, err := k.addressCodec.StringToBytes(sentinel)
	if err != nil {
		return errorsmod.Wrap(err, "invalid sentinel address")
	}
	if err := k.repKeeper.MintDREAM(ctx, addr, reward); err != nil {
		return errorsmod.Wrap(err, "failed to mint curation reward")
	}
	return nil
}
