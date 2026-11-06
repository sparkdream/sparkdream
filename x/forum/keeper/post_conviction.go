package keeper

// Post conviction-stake mechanics — author-earned, slashable forum reputation.
//
// Lifecycle:
//   1. OpenPostConvictionStake locks the staker's DREAM and inserts a record
//      into PostConvictionStake + the (post_id, stake_id) and (staker,
//      stake_id) secondary indexes.
//   2. AccruePostConvictions runs in the EndBlocker. For every active stake
//      it credits per-tag forum rep to the post's author via
//      repKeeper.AddForumRep, prorated by elapsed blocks and split evenly
//      across the post's tags. The credited amount is also recorded on the
//      stake's accrued_rep_per_tag so a later slash can be exact.
//   3. ReleasePostConvictionStake unlocks the staker's DREAM (minus any slash
//      applied while the stake was open), moves the stake from the active
//      indexes to ReleasedStakesByPost + the ReleasedStakeGc queue, and marks
//      the record released. The record is retained until GC so a late
//      tombstone can still claw back the rep it produced.
//   4. SlashStakesForPost is called whenever a hide on the post stands
//      (unappealed expiry, TTL prune, UPHELD verdict). For every active AND
//      released-but-not-GC'd stake it calls repKeeper.DeductForumRep on the
//      author for the exact amount the stake produced per tag and burns
//      staker_slash_bps of the stake (from the lock for active stakes, from
//      the staker's balance, best effort, for released ones).
//
// Per-(author, tag, epoch) cap is enforced by consumeForumRepEpochHeadroom
// against params.max_forum_rep_per_tag_per_epoch. Epoch = floor(block_time
// / 86400), matching x/session's UTC-day bucket convention.

import (
	"context"
	"fmt"
	stdmath "math"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

// OpenPostConvictionStake creates a new PostConvictionStake, locks the
// staker's DREAM via repKeeper.LockDREAM, and registers the (post_id,
// stake_id) + (staker, stake_id) secondary indexes. Returns the new stake's
// id.
//
// Validation here is the union of constraints owned by the keeper itself.
// Caller-policy (must-not-be-author, ESTABLISHED-or-higher) lives in the
// msg_server so the keeper stays reusable from on-chain callers (governance,
// future module hooks) without re-deriving member context.
func (k Keeper) OpenPostConvictionStake(
	ctx context.Context,
	staker sdk.AccAddress,
	postID uint64,
	amount math.Int,
) (uint64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	if amount.IsNil() || !amount.IsPositive() {
		return 0, fmt.Errorf("conviction stake amount must be positive")
	}
	if !params.MinPostConvictionStake.IsNil() && amount.LT(params.MinPostConvictionStake) {
		return 0, fmt.Errorf(
			"stake amount %s below min %s", amount.String(), params.MinPostConvictionStake.String(),
		)
	}
	if params.PostConvictionLockSeconds <= 0 {
		return 0, fmt.Errorf("post conviction lock period not configured")
	}

	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return 0, fmt.Errorf("post %d not found: %w", postID, err)
	}
	if post.Status != types.PostStatus_POST_STATUS_ACTIVE {
		return 0, fmt.Errorf("cannot stake on non-active post %d (status=%s)", postID, post.Status)
	}
	if k.sameMember(ctx, post.Author, staker.String()) {
		return 0, fmt.Errorf("staker cannot stake on their own post")
	}
	if len(post.Tags) == 0 {
		return 0, fmt.Errorf("post %d has no tags; conviction stake would credit no rep", postID)
	}

	if k.repKeeper == nil {
		return 0, fmt.Errorf("rep keeper not wired")
	}
	if err := k.repKeeper.LockDREAM(ctx, staker, amount); err != nil {
		return 0, fmt.Errorf("lock dream: %w", err)
	}

	id, err := k.PostConvictionStakeSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	stake := types.PostConvictionStake{
		Id:               id,
		Staker:           staker.String(),
		PostId:           postID,
		Amount:           amount,
		StakedAt:         now,
		UnlocksAt:        now + params.PostConvictionLockSeconds,
		AccruedRepPerTag: make(map[string]string),
		LastAccrualAt:    now,
		Released:         false,
	}

	if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
		return 0, err
	}
	if err := k.PostConvictionStakesByPost.Set(ctx, collections.Join(postID, id)); err != nil {
		return 0, err
	}
	if err := k.PostConvictionStakesByStaker.Set(ctx, collections.Join(staker.String(), id)); err != nil {
		return 0, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			"post_conviction_staked",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
			sdk.NewAttribute("staker", staker.String()),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("unlocks_at", fmt.Sprintf("%d", stake.UnlocksAt)),
		),
	)
	return id, nil
}

// ReleasePostConvictionStake unlocks the staker's DREAM after the lock
// window has passed. The accrued author rep is not refunded (the stake's
// purpose was to credit rep; that side is final unless a hide on the post
// later stands). The record is marked released and kept, indexed under
// ReleasedStakesByPost, until GcReleasedPostConvictionStakes collects it
// after releasedStakeRetention: a hide that stands in that window still
// claws back the rep the stake produced (SlashStakesForPost).
func (k Keeper) ReleasePostConvictionStake(
	ctx context.Context,
	stakeID uint64,
	caller sdk.AccAddress,
) error {
	stake, err := k.PostConvictionStake.Get(ctx, stakeID)
	if err != nil {
		return fmt.Errorf("stake %d not found: %w", stakeID, err)
	}
	if stake.Released {
		return fmt.Errorf("stake %d already released", stakeID)
	}
	// Alias-aware: after a key migration the stake stays recorded under the
	// old address and the new canonical key releases it.
	if !k.canActAs(ctx, caller.String(), stake.Staker) {
		return fmt.Errorf("only the original staker may release stake %d", stakeID)
	}

	// A standing hide is still being settled against this post's stakes;
	// releasing now would let the stake dodge its slash.
	if queued, _ := k.StakeSlashQueue.Has(ctx, stake.PostId); queued {
		return fmt.Errorf("stake %d is being slashed", stakeID)
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if now < stake.UnlocksAt {
		return fmt.Errorf("stake %d locked until %d (now %d)", stakeID, stake.UnlocksAt, now)
	}

	if k.repKeeper == nil {
		return fmt.Errorf("rep keeper not wired")
	}
	if stake.Amount.IsPositive() {
		// The lock lives on the stored staker; rep's DREAM primitives resolve
		// the alias to the canonical member.
		stakerAddr, err := k.addressCodec.StringToBytes(stake.Staker)
		if err != nil {
			return fmt.Errorf("invalid staker address: %w", err)
		}
		if err := k.repKeeper.UnlockDREAM(ctx, stakerAddr, stake.Amount); err != nil {
			return fmt.Errorf("unlock dream: %w", err)
		}
	}

	stake.Released = true
	if err := k.PostConvictionStake.Set(ctx, stakeID, stake); err != nil {
		return err
	}

	// Move from the active-stake indexes to the released-stake index + GC
	// queue: released stakes no longer accrue, but a hide that stands before
	// GC must still find them (SlashStakesForPost).
	if err := k.PostConvictionStakesByPost.Remove(ctx, collections.Join(stake.PostId, stakeID)); err != nil {
		return err
	}
	if err := k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(stake.Staker, stakeID)); err != nil {
		return err
	}
	if err := k.indexReleasedStake(ctx, stake, now); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		sdk.NewEvent(
			"post_conviction_released",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
			sdk.NewAttribute("staker", caller.String()),
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", stake.PostId)),
			sdk.NewAttribute("amount", stake.Amount.String()),
		),
	)
	return nil
}

// maxAccrualPerBlock bounds AccruePostConvictions per EndBlocker pass.
// Matches the pattern used by maxPrunePerBlock / maxHiddenExpiry /
// maxBountyExpirations in abci.go: each phase walks a bounded slice and
// persists a cursor so the next block resumes from where this one stopped.
// 100 mirrors maxPrunePerBlock — high enough to clear typical bursts in a
// few blocks, low enough not to dominate block gas under saturation.
const maxAccrualPerBlock = 100

// AccruePostConvictions walks at most maxAccrualPerBlock active stakes per
// EndBlocker pass and credits the post's author with prorated per-tag forum
// rep, subject to the per-(author, tag, epoch) cap.
//
// Accrual formula per stake (time-based, not block-based — variable block
// time is irrelevant):
//
//	amount_dream         = stake.amount_uDREAM / 1_000_000
//	elapsed_days         = (effectiveNow - last_accrual_at) / 86400
//	rep_credited_total   = amount_dream * params.rate * elapsed_days
//	rep_credited_per_tag = rep_credited_total / len(post.tags)
//
// effectiveNow is min(now, stake.UnlocksAt) — post-window time does not
// accrue, so a staker who lets the lock window pass before releasing does
// not keep streaming rep to the author.
//
// Each per-tag credit is bounded by max_forum_rep_per_tag_per_epoch (one
// epoch = one UTC day). Excess is silently dropped, never refunded — honest
// stakers cannot foresee saturation by other stakers, so this is the
// simplest fair behavior. The stake's accrued_rep_per_tag tracks the
// *actually credited* amount so SlashStakesForPost claws back exactly what
// was credited and no more.
//
// Per-stake errors log but never abort the loop.
func (k Keeper) AccruePostConvictions(ctx context.Context) error {
	if k.repKeeper == nil {
		return nil
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	rate := params.PostConvictionStreamRatePerBlock
	if rate.IsNil() || !rate.IsPositive() {
		return nil // accrual disabled by config
	}
	perEpochCap := params.MaxForumRepPerTagPerEpoch // may be zero = unlimited

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	currentEpoch := now / 86400

	// Resume from the persisted cursor. First-ever invocation reads zero.
	cursor, err := k.PostConvictionAccrualCursor.Get(ctx)
	if err != nil {
		cursor = 0
	}

	// Iterate active stakes starting at the cursor. Every walked id (active
	// OR released-but-not-GC'd) counts toward the per-block budget — bounding
	// walked-items (not credited-items) keeps the cursor advancing under
	// saturation regardless of how many stakes happen to be released.
	var activeIDs []uint64
	var lastWalked uint64 = cursor
	walked := 0
	wrapped := false
	rng := new(collections.Range[uint64]).StartInclusive(cursor)
	err = k.PostConvictionStake.Walk(ctx, rng, func(id uint64, stake types.PostConvictionStake) (bool, error) {
		if walked >= maxAccrualPerBlock {
			return true, nil
		}
		walked++
		lastWalked = id
		if !stake.Released {
			activeIDs = append(activeIDs, id)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	// If we walked less than the budget and the cursor was non-zero, the
	// natural ordering wrapped past the end of the map. Reset to 0 so the
	// next block resumes from the start of the keyspace. Persist the cursor
	// before returning; downstream errors during accrual should NOT leave it
	// stuck.
	var nextCursor uint64
	if walked < maxAccrualPerBlock {
		nextCursor = 0
		wrapped = true
	} else {
		nextCursor = lastWalked + 1
	}
	_ = wrapped // silence unused if we add metrics later
	if err := k.PostConvictionAccrualCursor.Set(ctx, nextCursor); err != nil {
		sdkCtx.Logger().Warn("failed to persist post conviction accrual cursor",
			"cursor", nextCursor, "error", err)
	}

	for _, id := range activeIDs {
		stake, err := k.PostConvictionStake.Get(ctx, id)
		if err != nil {
			continue
		}
		if stake.Released || !stake.Amount.IsPositive() {
			continue
		}

		// Bound accrual at the lock window so post-window time does not
		// keep crediting.
		effectiveNow := now
		if effectiveNow > stake.UnlocksAt {
			effectiveNow = stake.UnlocksAt
		}
		if effectiveNow <= stake.LastAccrualAt {
			continue
		}

		elapsedSeconds := effectiveNow - stake.LastAccrualAt
		elapsedDays := math.LegacyNewDec(elapsedSeconds).QuoInt64(86400)
		amountDream := math.LegacyNewDecFromInt(stake.Amount).QuoInt64(1_000_000)
		repTotal := amountDream.Mul(rate).Mul(elapsedDays)

		if !repTotal.IsPositive() {
			stake.LastAccrualAt = effectiveNow
			if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("AccruePostConvictions: PostConvictionStake.Set failed", "err", err)
			}
			continue
		}

		// Only a live post earns: time spent HIDDEN (or archived / deleted)
		// does not accrue, so a hide that later stands claws back nothing it
		// should not have credited.
		post, perr := k.Post.Get(ctx, stake.PostId)
		if perr != nil || len(post.Tags) == 0 || post.Status != types.PostStatus_POST_STATUS_ACTIVE {
			stake.LastAccrualAt = effectiveNow
			if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("AccruePostConvictions: PostConvictionStake.Set failed", "err", err)
			}
			continue
		}

		repPerTag := repTotal.QuoInt64(int64(len(post.Tags)))

		authorBytes, addrErr := k.addressCodec.StringToBytes(post.Author)
		if addrErr != nil {
			stake.LastAccrualAt = effectiveNow
			if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("AccruePostConvictions: PostConvictionStake.Set failed", "err", err)
			}
			continue
		}
		author := sdk.AccAddress(authorBytes)

		if stake.AccruedRepPerTag == nil {
			stake.AccruedRepPerTag = make(map[string]string)
		}

		for _, tag := range post.Tags {
			grant := repPerTag

			// Enforce per-(author, tag, epoch) cap if configured.
			if !perEpochCap.IsNil() && perEpochCap.IsPositive() {
				headroom, err := k.consumeForumRepEpochHeadroom(ctx, post.Author, tag, currentEpoch, perEpochCap, grant)
				if err != nil {
					sdkCtx.Logger().Warn("forum rep cap headroom check failed",
						"stake_id", id, "author", post.Author, "tag", tag, "error", err)
					continue
				}
				if !headroom.IsPositive() {
					continue // cap saturated for this tag in this epoch
				}
				grant = headroom
			}

			if err := k.repKeeper.AddForumRep(ctx, author, tag, grant); err != nil {
				sdkCtx.Logger().Warn("forum rep accrual failed",
					"stake_id", id, "author", post.Author, "tag", tag, "error", err)
				continue
			}
			prior := math.LegacyZeroDec()
			if s, ok := stake.AccruedRepPerTag[tag]; ok {
				prior, _ = math.LegacyNewDecFromStr(s)
			}
			stake.AccruedRepPerTag[tag] = prior.Add(grant).String()
		}

		stake.LastAccrualAt = effectiveNow
		if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
			sdkCtx.Logger().Warn("failed to persist stake accrual", "stake_id", id, "error", err)
		}
	}
	return nil
}

// consumeForumRepEpochHeadroom returns the amount that can actually be
// credited to (author, tag) in the given epoch and writes through the new
// total. Returns zero if the cap is saturated. The counter is implicitly
// reset whenever the stored epoch_id is stale.
func (k Keeper) consumeForumRepEpochHeadroom(
	ctx context.Context,
	author string,
	tag string,
	epochID int64,
	cap math.LegacyDec,
	requested math.LegacyDec,
) (math.LegacyDec, error) {
	key := collections.Join(author, tag)

	current := math.LegacyZeroDec()
	if rec, err := k.ForumRepEpochCounter.Get(ctx, key); err == nil {
		if rec.EpochId == epochID {
			current = rec.Accumulated
		}
		// else: stale epoch, treat as zero and overwrite below
	}

	remaining := cap.Sub(current)
	if !remaining.IsPositive() {
		return math.LegacyZeroDec(), nil
	}
	grant := requested
	if grant.GT(remaining) {
		grant = remaining
	}

	newTotal := current.Add(grant)
	if err := k.ForumRepEpochCounter.Set(ctx, key, types.ForumRepEpochCounter{
		EpochId:     epochID,
		Accumulated: newTotal,
	}); err != nil {
		return math.LegacyZeroDec(), err
	}
	return grant, nil
}

// maxStakeSlashPerBlock bounds how many conviction stakes one slash pass
// settles, in a tx (a hide upheld on appeal) or in an EndBlocker. A post with
// more stakes stays in StakeSlashQueue and the EndBlocker finishes it, so a
// heavily staked post can neither exhaust a tx's gas nor stall a block.
const maxStakeSlashPerBlock = 200

// SlashStakesForPost is invoked from tombstoneHiddenPost when a hide stands
// (unappealed expiry or an UPHELD appeal). It queues the post, with the
// author the rep is clawed back from, in StakeSlashQueue and settles up to
// maxStakeSlashPerBlock of its stakes at once; ProcessStakeSlashQueue
// finishes the rest in later EndBlockers. For every (active or
// recently-released) stake on the post:
//   - The exact rep each stake produced per tag is clawed back from the
//     author via repKeeper.DeductForumRep (floored at zero).
//   - staker_slash_bps of the staker's locked DREAM is burned out of the
//     lock (released stakes: out of the staker's free balance).
//   - Active stakes are closed and the non-slashed remainder is unlocked
//     (the post is gone, so there is nothing left to release it for).
//
// While a post is queued its stakes cannot be released or collected, so
// every stake is slashed exactly once. Best-effort: per-stake errors log but
// never abort the sweep.
func (k Keeper) SlashStakesForPost(ctx context.Context, postID uint64) error {
	if k.repKeeper == nil {
		return nil
	}
	author := ""
	if post, err := k.Post.Get(ctx, postID); err == nil {
		author = post.Author
	}
	if err := k.StakeSlashQueue.Set(ctx, postID, author); err != nil {
		return err
	}
	_, err := k.slashQueuedPost(ctx, postID, author, maxStakeSlashPerBlock)
	return err
}

// ProcessStakeSlashQueue settles at most maxStakeSlashPerBlock queued stakes
// across the posts in StakeSlashQueue, in post order, dropping each post
// from the queue once none of its stakes is left.
func (k Keeper) ProcessStakeSlashQueue(ctx context.Context) error {
	if k.repKeeper == nil {
		return nil
	}
	type entry struct {
		postID uint64
		author string
	}
	var posts []entry
	if err := k.StakeSlashQueue.Walk(ctx, nil, func(postID uint64, author string) (bool, error) {
		posts = append(posts, entry{postID, author})
		return len(posts) >= maxStakeSlashPerBlock, nil
	}); err != nil {
		return err
	}
	budget := maxStakeSlashPerBlock
	for _, e := range posts {
		if budget <= 0 {
			break
		}
		used, err := k.slashQueuedPost(ctx, e.postID, e.author, budget)
		if err != nil {
			return err
		}
		budget -= used
		// An empty post still costs one unit, so the walk always progresses.
		if used == 0 {
			budget--
		}
	}
	return nil
}

// slashQueuedPost settles up to budget stakes of a queued post (released
// first, then active) and removes the post from StakeSlashQueue once it has
// none left. It returns how many stakes it settled.
func (k Keeper) slashQueuedPost(ctx context.Context, postID uint64, author string, budget int) (int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	slashBps := params.PostConvictionStakerSlashBps
	if slashBps > 10000 {
		slashBps = 10000
	}
	// A hide on an anonymous post could never be appealed (nobody can act as
	// the shield account), so it never faced a challenge: its stakes are
	// closed and unlocked without the staker slash (FRM-102).
	if k.isAnonymous(author) {
		slashBps = 0
	}

	rng := collections.NewPrefixedPairRange[uint64, uint64](postID)
	collect := func(idx collections.KeySet[collections.Pair[uint64, uint64]], limit int) ([]uint64, error) {
		var ids []uint64
		if limit <= 0 {
			return nil, nil
		}
		err := idx.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
			ids = append(ids, key.K2())
			return len(ids) >= limit, nil
		})
		return ids, err
	}

	releasedIDs, err := collect(k.ReleasedStakesByPost, budget)
	if err != nil {
		return 0, err
	}
	for _, id := range releasedIDs {
		k.slashReleasedStake(ctx, postID, id, author, slashBps)
	}
	activeIDs, err := collect(k.PostConvictionStakesByPost, budget-len(releasedIDs))
	if err != nil {
		return 0, err
	}
	for _, id := range activeIDs {
		k.slashActiveStake(ctx, postID, id, author, slashBps)
	}
	used := len(releasedIDs) + len(activeIDs)

	if used < budget {
		// Both indexes ran out inside the budget: nothing is left to slash.
		return used, k.StakeSlashQueue.Remove(ctx, postID)
	}
	return used, nil
}

// slashActiveStake applies a standing hide to a stake still locked on the
// post: exact rep clawback from author, staker_slash_bps burned out of the
// lock, the remainder unlocked and the stake closed.
func (k Keeper) slashActiveStake(ctx context.Context, postID, id uint64, author string, slashBps uint64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	// Drop the index entries first so a failure below can never leave the
	// stake to be slashed twice.
	if err := k.PostConvictionStakesByPost.Remove(ctx, collections.Join(postID, id)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("slashActiveStake: PostConvictionStakesByPost.Remove failed", "err", err)
	}
	stake, err := k.PostConvictionStake.Get(ctx, id)
	if err != nil || stake.Released {
		return // stale index entry
	}
	if err := k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(stake.Staker, id)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("slashActiveStake: PostConvictionStakesByStaker.Remove failed", "err", err)
	}

	// Claw back the exact author rep this stake produced, per tag.
	k.clawBackStakeRep(ctx, stake, author)

	// Burn staker_slash_bps of the locked DREAM, out of the lock.
	if slashBps > 0 && stake.Amount.IsPositive() {
		slashAmt := stake.Amount.MulRaw(int64(slashBps)).QuoRaw(10000)
		if slashAmt.IsPositive() && slashAmt.LTE(stake.Amount) {
			stakerBytes, addrErr := k.addressCodec.StringToBytes(stake.Staker)
			if addrErr == nil {
				staker := sdk.AccAddress(stakerBytes)
				if err := k.repKeeper.BurnLockedDREAM(ctx, staker, slashAmt); err != nil {
					sdkCtx.Logger().Warn("staker slash burn failed",
						"stake_id", id, "amount", slashAmt.String(), "error", err)
				} else {
					stake.Amount = stake.Amount.Sub(slashAmt)
				}
			}
		}
	}

	// Release the remaining locked DREAM to the staker and close the
	// stake. The post is gone; no further accrual is possible.
	if stake.Amount.IsPositive() {
		stakerBytes, addrErr := k.addressCodec.StringToBytes(stake.Staker)
		if addrErr == nil {
			staker := sdk.AccAddress(stakerBytes)
			if err := k.repKeeper.UnlockDREAM(ctx, staker, stake.Amount); err != nil {
				sdkCtx.Logger().Warn("post-slash unlock failed",
					"stake_id", id, "amount", stake.Amount.String(), "error", err)
			} else {
				stake.Amount = math.ZeroInt()
			}
		}
	}

	stake.Released = true
	if err := k.PostConvictionStake.Set(ctx, id, stake); err != nil {
		sdkCtx.Logger().Warn("post-slash stake persist failed", "stake_id", id, "error", err)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"post_conviction_slashed",
			sdk.NewAttribute("stake_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
			sdk.NewAttribute("staker", stake.Staker),
			sdk.NewAttribute("slash_bps", fmt.Sprintf("%d", slashBps)),
		),
	)
}

// clawBackStakeRep deducts from author exactly the forum rep the stake
// produced in each tag (floored at zero by x/rep).
func (k Keeper) clawBackStakeRep(ctx context.Context, stake types.PostConvictionStake, author string) {
	authorBytes, err := k.addressCodec.StringToBytes(author)
	if err != nil || len(authorBytes) == 0 {
		return
	}
	for tag, v := range stake.AccruedRepPerTag {
		amt, derr := math.LegacyNewDecFromStr(v)
		if derr != nil || !amt.IsPositive() {
			continue
		}
		if err := k.repKeeper.DeductForumRep(ctx, sdk.AccAddress(authorBytes), tag, amt); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Warn("forum rep clawback failed",
				"stake_id", stake.Id, "tag", tag, "error", err)
		}
	}
}

// maxStakeGcPerBlock bounds the GC sweep so it cannot dominate block gas
// when the released-stake backlog is large.
const maxStakeGcPerBlock = 100

// releasedStakeRetention is how long a released stake is kept for a late
// slash: long enough for a hide placed right after release to finalize
// unappealed (hidden_expiration) or through a full appeal, plus a day of
// margin. GC also defers any stake whose post still carries a hide.
func releasedStakeRetention() int64 {
	return int64(types.DefaultHiddenExpiration) + types.DefaultAppealDeadline + 86400
}

// indexReleasedStake files a released stake under ReleasedStakesByPost and
// queues it for GC at releasedAt + retention.
func (k Keeper) indexReleasedStake(ctx context.Context, stake types.PostConvictionStake, releasedAt int64) error {
	if err := k.ReleasedStakesByPost.Set(ctx, collections.Join(stake.PostId, stake.Id)); err != nil {
		return err
	}
	return k.ReleasedStakeGc.Set(ctx, collections.Join(releasedAt+releasedStakeRetention(), stake.Id))
}

// slashReleasedStake applies a standing hide to a stake its staker already
// released: the rep it produced is clawed back from the author exactly, and
// staker_slash_bps of the stake is burned from the staker's balance (best
// effort: the DREAM is liquid now, so a staker who spent it loses only what
// is left). The record is then deleted; its GC queue entry goes stale.
func (k Keeper) slashReleasedStake(ctx context.Context, postID, stakeID uint64, author string, slashBps uint64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if err := k.ReleasedStakesByPost.Remove(ctx, collections.Join(postID, stakeID)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("slashReleasedStake: ReleasedStakesByPost.Remove failed", "err", err)
	}
	stake, err := k.PostConvictionStake.Get(ctx, stakeID)
	if err != nil || !stake.Released {
		return
	}

	k.clawBackStakeRep(ctx, stake, author)

	burned := math.ZeroInt()
	if slashBps > 0 && stake.Amount.IsPositive() {
		slashAmt := stake.Amount.MulRaw(int64(slashBps)).QuoRaw(10000)
		if stakerBytes, aerr := k.addressCodec.StringToBytes(stake.Staker); aerr == nil && slashAmt.IsPositive() {
			staker := sdk.AccAddress(stakerBytes)
			// The stake was already released, so the slash comes out of the
			// staker's free balance; x/rep burns only unlocked DREAM, so the
			// cap is the unlocked balance, not the total.
			if m, merr := k.repKeeper.GetMember(ctx, staker); merr == nil && m.DreamBalance != nil {
				unlocked := *m.DreamBalance
				if m.StakedDream != nil {
					unlocked = unlocked.Sub(*m.StakedDream)
				}
				if unlocked.LT(slashAmt) {
					slashAmt = unlocked
				}
			}
			if slashAmt.IsPositive() {
				if err := k.repKeeper.BurnDREAM(ctx, staker, slashAmt); err != nil {
					sdkCtx.Logger().Warn("released-stake slash burn failed",
						"stake_id", stakeID, "amount", slashAmt.String(), "error", err)
				} else {
					burned = slashAmt
				}
			}
		}
	}

	if err := k.PostConvictionStake.Remove(ctx, stakeID); err != nil {
		sdkCtx.Logger().Warn("released-stake slash: remove failed", "stake_id", stakeID, "error", err)
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("post_conviction_slashed",
		sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stakeID)),
		sdk.NewAttribute("post_id", fmt.Sprintf("%d", postID)),
		sdk.NewAttribute("staker", stake.Staker),
		sdk.NewAttribute("slash_bps", fmt.Sprintf("%d", slashBps)),
		sdk.NewAttribute("released", "true"),
		sdk.NewAttribute("burned", burned.String()),
	))
}

// GcReleasedPostConvictionStakes deletes released stakes whose retention
// window has passed. It walks the ReleasedStakeGc queue up to now, so active
// stakes are never scanned. A stake whose post still carries a hide, or is
// queued in StakeSlashQueue, is deferred a day: the slash may yet need it.
func (k Keeper) GcReleasedPostConvictionStakes(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(now, uint64(stdmath.MaxUint64)))
	var keys []collections.Pair[int64, uint64]
	if err := k.ReleasedStakeGc.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(keys) >= maxStakeGcPerBlock {
			return true, nil
		}
		keys = append(keys, key)
		return false, nil
	}); err != nil {
		return err
	}

	for _, key := range keys {
		if err := k.ReleasedStakeGc.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("GcReleasedPostConvictionStakes: ReleasedStakeGc.Remove failed", "err", err)
		}
		id := key.K2()
		stake, err := k.PostConvictionStake.Get(ctx, id)
		if err != nil || !stake.Released {
			continue // already slashed / removed
		}
		// Defer while a hide may still stand or a standing one is still
		// being settled: the slash must find the stake.
		hidden, getErr := k.HideRecord.Has(ctx, stake.PostId)
		if getErr != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("Has failed", "err", getErr)
		}
		queued, getErr := k.StakeSlashQueue.Has(ctx, stake.PostId)
		if getErr != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("Has failed", "err", getErr)
		}
		if hidden || queued {
			if err := k.ReleasedStakeGc.Set(ctx, collections.Join(now+86400, id)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("GcReleasedPostConvictionStakes: ReleasedStakeGc.Set failed", "err", err)
			}
			continue
		}
		if err := k.PostConvictionStake.Remove(ctx, id); err != nil {
			sdkCtx.Logger().Warn("post conviction gc: remove failed", "stake_id", id, "error", err)
			continue
		}
		if err := k.ReleasedStakesByPost.Remove(ctx, collections.Join(stake.PostId, id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("GcReleasedPostConvictionStakes: ReleasedStakesByPost.Remove failed", "err", err)
		}
		if err := k.PostConvictionStakesByPost.Remove(ctx, collections.Join(stake.PostId, id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("GcReleasedPostConvictionStakes: PostConvictionStakesByPost.Remove failed", "err", err)
		}
		if err := k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(stake.Staker, id)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("GcReleasedPostConvictionStakes: PostConvictionStakesByStaker.Remove failed", "err", err)
		}
	}
	return nil
}
