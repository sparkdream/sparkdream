package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	for _, elem := range genState.PostMap {
		// Media labels are recomputed rather than trusted, so a genesis
		// exported by an older binary (or hand-edited) imports correctly.
		applyPostMediaLabels(&elem)
		if err := k.Post.Set(ctx, elem.PostId, elem); err != nil {
			return err
		}
		// Rebuild the EphemeralByAuthor index for any still-ephemeral posts.
		// PromotionQueue is hook-driven and carried verbatim (promotion_queue).
		// A soft-deleted post keeps its expiration_time (the TTL prune still
		// hard-deletes it) but MsgDeletePost drops its index entry, so it is
		// not indexed here either.
		if elem.ExpirationTime > 0 {
			if elem.Status != types.PostStatus_POST_STATUS_DELETED {
				if err := k.AddEphemeralAuthorIndex(ctx, elem.Author, elem.PostId); err != nil {
					return err
				}
			}
			// The TTL queue is derived from expiration_time; without it an
			// imported ephemeral post would never be pruned.
			if err := k.ExpirationQueue.Set(ctx, collections.Join(elem.ExpirationTime, elem.PostId)); err != nil {
				return err
			}
		}
		if elem.ParentId == 0 && elem.Status != types.PostStatus_POST_STATUS_DELETED {
			if err := k.RootsByCategory.Set(ctx, collections.Join(elem.CategoryId, elem.PostId)); err != nil {
				return err
			}
			if elem.Locked {
				if err := k.LockedRoots.Set(ctx, elem.PostId); err != nil {
					return err
				}
			}
		}
		if elem.ParentId != 0 {
			if err := k.PostsByRoot.Set(ctx, collections.Join(elem.RootId, elem.PostId)); err != nil {
				return err
			}
		} else if elem.Status != types.PostStatus_POST_STATUS_DELETED {
			if err := k.PostsByUpvotes.Set(ctx, collections.Join(elem.UpvoteCount, elem.PostId)); err != nil {
				return err
			}
			if elem.Pinned {
				if err := k.PostsByPinned.Set(ctx, collections.Join(elem.CategoryId, elem.PostId)); err != nil {
					return err
				}
			}
		}
	}
	for _, elem := range genState.UserRateLimitMap {
		if err := k.UserRateLimit.Set(ctx, elem.UserAddress, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.UserReactionLimitMap {
		if err := k.UserReactionLimit.Set(ctx, elem.StoreKey(), elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.SentinelActivityMap {
		if err := k.SentinelActivity.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.HideRecordMap {
		if err := k.HideRecord.Set(ctx, elem.PostId, elem); err != nil {
			return err
		}
		if !elem.Appealed {
			if err := k.indexHideExpiry(ctx, elem.PostId, elem); err != nil {
				return err
			}
		}
	}
	for _, elem := range genState.ThreadLockRecordMap {
		if err := k.ThreadLockRecord.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
		if !elem.AppealPending {
			if err := k.LockRecordExpiry.Set(ctx, collections.Join(lockExpiryAt(elem), elem.RootId)); err != nil {
				return err
			}
		}
	}
	for _, elem := range genState.ThreadMoveRecordMap {
		if err := k.ThreadMoveRecord.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
		if !elem.AppealPending {
			if err := k.MoveRecordExpiry.Set(ctx, collections.Join(moveExpiryAt(elem), elem.RootId)); err != nil {
				return err
			}
		}
	}
	for _, elem := range genState.PostFlagMap {
		if err := k.PostFlag.Set(ctx, elem.PostId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.BountyList {
		if err := k.Bounty.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
		if err := k.BountiesByCreator.Set(ctx, collections.Join(elem.Creator, elem.Id)); err != nil {
			return err
		}
		// Every open bounty keeps its thread index, so moderation
		// reconciliation can still find a suspended (MODERATION_PENDING)
		// one; only an ACTIVE bounty's timer runs, so only it is in the
		// expiry index (FRM-25).
		if elem.IsOpen() {
			if err := k.ActiveBountyByThread.Set(ctx, elem.ThreadId, elem.Id); err != nil {
				return err
			}
		}
		if elem.Status == types.BountyStatus_BOUNTY_STATUS_ACTIVE {
			if err := k.BountiesByExpiry.Set(ctx, collections.Join(elem.ExpiresAt, elem.Id)); err != nil {
				return err
			}
		}
	}

	if err := k.BountySeq.Set(ctx, genState.BountyCount); err != nil {
		return err
	}
	for _, elem := range genState.ThreadMetadataMap {
		if err := k.ThreadMetadata.Set(ctx, elem.ThreadId, elem); err != nil {
			return err
		}
		// Rebuild the (derived) auto-confirm queue for any pending sentinel
		// accepted-reply proposal. The queue is not a genesis field — it is
		// reconstructed here from the stamped proposal_fire_at.
		if elem.ProposedReplyId != 0 && elem.ProposalFireAt != 0 {
			if err := k.ProposalAutoConfirmQueue.Set(ctx, collections.Join(elem.ProposalFireAt, elem.ThreadId)); err != nil {
				return err
			}
		}
	}
	for _, elem := range genState.ProposalCountMap {
		if err := k.ProposalCountByThreadSentinel.Set(ctx, collections.Join(elem.ThreadId, elem.Sentinel), elem.Count); err != nil {
			return err
		}
	}
	for _, elem := range genState.ThreadFollowMap {
		// Same key MsgFollowThread writes ("<follower>:<thread_id>").
		if err := k.ThreadFollow.Set(ctx, fmt.Sprintf("%s:%d", elem.Follower, elem.ThreadId), elem); err != nil {
			return err
		}
		if err := k.FollowersByThread.Set(ctx, collections.Join(elem.ThreadId, elem.Follower)); err != nil {
			return err
		}
		if err := k.ThreadsByFollower.Set(ctx, collections.Join(elem.Follower, elem.ThreadId)); err != nil {
			return err
		}
	}
	for _, elem := range genState.ThreadFollowCountMap {
		if err := k.ThreadFollowCount.Set(ctx, elem.ThreadId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ArchiveMetadataMap {
		if err := k.ArchiveMetadata.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
	}
	// Post conviction stakes: active stakes keep DREAM locked in x/rep, so
	// the record (and its indexes) must survive export/import.
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	stakeSeq := genState.PostConvictionStakeCount
	for _, elem := range genState.PostConvictionStakeList {
		if err := k.PostConvictionStake.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
		if elem.Released {
			if err := k.indexReleasedStake(ctx, elem, now); err != nil {
				return err
			}
		} else {
			if err := k.PostConvictionStakesByPost.Set(ctx, collections.Join(elem.PostId, elem.Id)); err != nil {
				return err
			}
			if err := k.PostConvictionStakesByStaker.Set(ctx, collections.Join(elem.Staker, elem.Id)); err != nil {
				return err
			}
		}
		if elem.Id >= stakeSeq {
			stakeSeq = elem.Id + 1
		}
	}
	if err := k.PostConvictionStakeSeq.Set(ctx, stakeSeq); err != nil {
		return err
	}
	for _, elem := range genState.PostVoteList {
		if err := k.PostVote.Set(ctx, collections.Join(elem.PostId, elem.Voter)); err != nil {
			return err
		}
	}
	for _, elem := range genState.ForumRepEpochCounterList {
		if err := k.ForumRepEpochCounter.Set(ctx, collections.Join(elem.Author, elem.Tag), elem.Counter); err != nil {
			return err
		}
	}
	for _, elem := range genState.PendingStakeSlashList {
		if err := k.StakeSlashQueue.Set(ctx, elem.PostId, elem.Author); err != nil {
			return err
		}
	}

	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Write-through forum's sentinel bond-role config to x/rep so MsgBondRole
	// enforcement uses forum's seeded values (Phase 2 bonded-role generalization).
	if err := k.SyncSentinelBondedRoleConfig(ctx, genState.Params); err != nil {
		return err
	}

	// Restore PostSeq (next post ID). Floored at 1 — ID 0 is reserved
	// (PostId=0 conflicts with ParentId=0 meaning "no parent") — and above
	// every imported post ID, so legacy exports that predate post_count
	// cannot make new posts overwrite imported ones.
	postSeq := max(genState.PostCount, 1)
	for _, elem := range genState.PostMap {
		if elem.PostId >= postSeq {
			postSeq = elem.PostId + 1
		}
	}
	if err := k.PostSeq.Set(ctx, postSeq); err != nil {
		return err
	}

	for _, e := range genState.PromotionQueue {
		if err := k.PromotionQueue.Set(ctx, e.Address, e.EnqueuedAt); err != nil {
			return err
		}
	}
	for _, e := range genState.TagPruneQueue {
		if err := k.TagPruneQueue.Set(ctx, e.Tag, e.NextPostId); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Post.Walk(ctx, nil, func(_ uint64, val types.Post) (stop bool, err error) {
		genesis.PostMap = append(genesis.PostMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UserRateLimit.Walk(ctx, nil, func(_ string, val types.UserRateLimit) (stop bool, err error) {
		genesis.UserRateLimitMap = append(genesis.UserRateLimitMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UserReactionLimit.Walk(ctx, nil, func(_ string, val types.UserReactionLimit) (stop bool, err error) {
		genesis.UserReactionLimitMap = append(genesis.UserReactionLimitMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.SentinelActivity.Walk(ctx, nil, func(_ string, val types.SentinelActivity) (stop bool, err error) {
		genesis.SentinelActivityMap = append(genesis.SentinelActivityMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.HideRecord.Walk(ctx, nil, func(_ uint64, val types.HideRecord) (stop bool, err error) {
		genesis.HideRecordMap = append(genesis.HideRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadLockRecord.Walk(ctx, nil, func(_ uint64, val types.ThreadLockRecord) (stop bool, err error) {
		genesis.ThreadLockRecordMap = append(genesis.ThreadLockRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadMoveRecord.Walk(ctx, nil, func(_ uint64, val types.ThreadMoveRecord) (stop bool, err error) {
		genesis.ThreadMoveRecordMap = append(genesis.ThreadMoveRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.PostFlag.Walk(ctx, nil, func(_ uint64, val types.PostFlag) (stop bool, err error) {
		genesis.PostFlagMap = append(genesis.PostFlagMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.Bounty.Walk(ctx, nil, func(key uint64, elem types.Bounty) (bool, error) {
		genesis.BountyList = append(genesis.BountyList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.BountyCount, err = k.BountySeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	genesis.PostCount, err = k.PostSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.ThreadMetadata.Walk(ctx, nil, func(_ uint64, val types.ThreadMetadata) (stop bool, err error) {
		genesis.ThreadMetadataMap = append(genesis.ThreadMetadataMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadFollow.Walk(ctx, nil, func(_ string, val types.ThreadFollow) (stop bool, err error) {
		genesis.ThreadFollowMap = append(genesis.ThreadFollowMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadFollowCount.Walk(ctx, nil, func(_ uint64, val types.ThreadFollowCount) (stop bool, err error) {
		genesis.ThreadFollowCountMap = append(genesis.ThreadFollowCountMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ArchiveMetadata.Walk(ctx, nil, func(_ uint64, val types.ArchiveMetadata) (stop bool, err error) {
		genesis.ArchiveMetadataMap = append(genesis.ArchiveMetadataMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ProposalCountByThreadSentinel.Walk(ctx, nil, func(key collections.Pair[uint64, string], count uint64) (stop bool, err error) {
		genesis.ProposalCountMap = append(genesis.ProposalCountMap, types.ProposalCountEntry{
			ThreadId: key.K1(),
			Sentinel: key.K2(),
			Count:    count,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.PostConvictionStake.Walk(ctx, nil, func(_ uint64, val types.PostConvictionStake) (stop bool, err error) {
		genesis.PostConvictionStakeList = append(genesis.PostConvictionStakeList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.PostConvictionStakeCount, err = k.PostConvictionStakeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.PostVote.Walk(ctx, nil, func(key collections.Pair[uint64, string]) (stop bool, err error) {
		genesis.PostVoteList = append(genesis.PostVoteList, types.PostVoteEntry{PostId: key.K1(), Voter: key.K2()})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ForumRepEpochCounter.Walk(ctx, nil, func(key collections.Pair[string, string], val types.ForumRepEpochCounter) (stop bool, err error) {
		genesis.ForumRepEpochCounterList = append(genesis.ForumRepEpochCounterList, types.ForumRepEpochCounterEntry{
			Author:  key.K1(),
			Tag:     key.K2(),
			Counter: val,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.StakeSlashQueue.Walk(ctx, nil, func(postID uint64, author string) (bool, error) {
		genesis.PendingStakeSlashList = append(genesis.PendingStakeSlashList, types.PendingStakeSlash{
			PostId: postID,
			Author: author,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.PromotionQueue.Walk(ctx, nil, func(addr string, at int64) (bool, error) {
		genesis.PromotionQueue = append(genesis.PromotionQueue, types.PromotionQueueEntry{Address: addr, EnqueuedAt: at})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TagPruneQueue.Walk(ctx, nil, func(tag string, next uint64) (bool, error) {
		genesis.TagPruneQueue = append(genesis.TagPruneQueue, types.TagPruneEntry{Tag: tag, NextPostId: next})
		return false, nil
	}); err != nil {
		return nil, err
	}
	return genesis, nil
}
