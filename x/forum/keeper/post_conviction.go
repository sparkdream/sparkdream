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
//      applied while the stake was open), removes the secondary-index entries,
//      and marks the record released. The record is retained until a future
//      GC pass — see ReleasePostConvictionStake for the rationale.
//   4. SlashStakesForPost is called from ExpireHiddenPosts when an unappealed
//      sentinel hide finalizes. It walks every active stake on the post, calls
//      repKeeper.DeductForumRep on the author for the exact amount each stake
//      produced per tag, and burns staker_slash_bps of each staker's locked
//      DREAM. Released stakes that had not yet been GC'd are also slashed.
//
// Per-(author, tag, epoch) cap is enforced by consumeForumRepEpochHeadroom
// against params.max_forum_rep_per_tag_per_epoch. Epoch = floor(block_time
// / 86400), matching x/session's UTC-day bucket convention.

import (
	"context"
	"fmt"

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
	if post.Author == staker.String() {
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
// purpose was to credit rep; that side is final unless ExpireHiddenPosts
// later confirms a hide). The record is marked released but kept on disk
// until a GC pass collects it — see TODO at the bottom of this file for
// the GC sweep. This conservative retention lets a late-arriving
// ExpireHiddenPosts still find and slash the stake; if we deleted on
// release, a hide finalized one block after release would leave the
// author's accrued rep un-clawed-back.
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
	if stake.Staker != caller.String() {
		return fmt.Errorf("only the original staker may release stake %d", stakeID)
	}

	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if now < stake.UnlocksAt {
		return fmt.Errorf("stake %d locked until %d (now %d)", stakeID, stake.UnlocksAt, now)
	}

	if k.repKeeper == nil {
		return fmt.Errorf("rep keeper not wired")
	}
	if stake.Amount.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, caller, stake.Amount); err != nil {
			return fmt.Errorf("unlock dream: %w", err)
		}
	}

	stake.Released = true
	if err := k.PostConvictionStake.Set(ctx, stakeID, stake); err != nil {
		return err
	}

	// Drop from active-stake indexes — released stakes are no longer
	// candidates for accrual, and a GC pass will collect the record later.
	if err := k.PostConvictionStakesByPost.Remove(ctx, collections.Join(stake.PostId, stakeID)); err != nil {
		return err
	}
	if err := k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(stake.Staker, stakeID)); err != nil {
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
			_ = k.PostConvictionStake.Set(ctx, id, stake)
			continue
		}

		post, perr := k.Post.Get(ctx, stake.PostId)
		if perr != nil || len(post.Tags) == 0 {
			stake.LastAccrualAt = effectiveNow
			_ = k.PostConvictionStake.Set(ctx, id, stake)
			continue
		}

		repPerTag := repTotal.QuoInt64(int64(len(post.Tags)))

		authorBytes, addrErr := k.addressCodec.StringToBytes(post.Author)
		if addrErr != nil {
			stake.LastAccrualAt = effectiveNow
			_ = k.PostConvictionStake.Set(ctx, id, stake)
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

// SlashStakesForPost is invoked from ExpireHiddenPosts when an unappealed
// sentinel hide finalizes. For every (active or recently-released) stake on
// the post:
//   - The exact rep each stake produced per tag is clawed back from the
//     author via repKeeper.DeductForumRep (floored at zero).
//   - staker_slash_bps of the staker's locked DREAM is burned: first
//     UnlockDREAM the slash portion, then BurnDREAM it. The remaining
//     amount stays locked until the staker calls ReleasePostConviction
//     after the original window.
//   - Active stakes are marked released so accrual stops and their index
//     entries are dropped. The non-slashed remainder is unlocked here too
//     (caller skips the release path — the post is gone).
//
// Best-effort: per-stake errors log but never abort the slash sweep. A
// failed slash on stake N must not block the slash on stake N+1.
func (k Keeper) SlashStakesForPost(ctx context.Context, postID uint64) error {
	if k.repKeeper == nil {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	slashBps := params.PostConvictionStakerSlashBps
	if slashBps > 10000 {
		slashBps = 10000
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect stake ids under this post.
	var stakeIDs []uint64
	rng := collections.NewPrefixedPairRange[uint64, uint64](postID)
	err = k.PostConvictionStakesByPost.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
		stakeIDs = append(stakeIDs, key.K2())
		return false, nil
	})
	if err != nil {
		sdkCtx.Logger().Warn("post conviction slash: walk failed",
			"post_id", postID, "error", err)
	}

	for _, id := range stakeIDs {
		stake, err := k.PostConvictionStake.Get(ctx, id)
		if err != nil {
			continue
		}

		// Claw back the exact author rep this stake produced, per tag.
		authorBytes, addrErr := k.addressCodec.StringToBytes(
			func() string {
				post, perr := k.Post.Get(ctx, postID)
				if perr != nil {
					return ""
				}
				return post.Author
			}(),
		)
		if addrErr == nil && len(authorBytes) > 0 {
			author := sdk.AccAddress(authorBytes)
			for tag, s := range stake.AccruedRepPerTag {
				amt, perr := math.LegacyNewDecFromStr(s)
				if perr != nil || !amt.IsPositive() {
					continue
				}
				if err := k.repKeeper.DeductForumRep(ctx, author, tag, amt); err != nil {
					sdkCtx.Logger().Warn("forum rep clawback failed",
						"stake_id", id, "tag", tag, "error", err)
				}
			}
		}

		// Burn staker_slash_bps of the locked DREAM. Unlock → burn.
		if slashBps > 0 && stake.Amount.IsPositive() {
			slashAmt := stake.Amount.MulRaw(int64(slashBps)).QuoRaw(10000)
			if slashAmt.IsPositive() && slashAmt.LTE(stake.Amount) {
				stakerBytes, addrErr := k.addressCodec.StringToBytes(stake.Staker)
				if addrErr == nil {
					staker := sdk.AccAddress(stakerBytes)
					if err := k.repKeeper.UnlockDREAM(ctx, staker, slashAmt); err != nil {
						sdkCtx.Logger().Warn("staker slash unlock failed",
							"stake_id", id, "amount", slashAmt.String(), "error", err)
					} else if err := k.repKeeper.BurnDREAM(ctx, staker, slashAmt); err != nil {
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

		_ = k.PostConvictionStakesByPost.Remove(ctx, collections.Join(postID, id))
		_ = k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(stake.Staker, id))

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
	return nil
}

// maxStakeGcPerBlock bounds the GC sweep so it cannot dominate block gas
// when the released-stake backlog is large.
const maxStakeGcPerBlock = 100

// GcReleasedPostConvictionStakes deletes PostConvictionStake records whose
// Released == true AND whose StakedAt is older than the GC retention
// window. The retention window is (post_conviction_lock_seconds +
// hide_appeal_cooldown + margin), so any hide that could still finalize
// against the stake has had time to do so.
//
// Margin is 1 day past the latest possible appeal close — enough to swallow
// clock skew across a chain restart without exposing the slash path to
// races. Per-record errors log but never abort the sweep.
func (k Keeper) GcReleasedPostConvictionStakes(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Retention = lock window + hide-appeal cooldown + 1d margin. A released
	// stake older than this can no longer be the target of a slash because
	// the underlying post has either tombstoned (and slashed already) or
	// the appeal window has closed.
	const oneDay = int64(86400)
	retention := params.PostConvictionLockSeconds + params.HideAppealCooldown + oneDay

	// Collect candidate ids first to avoid mutating during walk.
	var gcIDs []uint64
	err = k.PostConvictionStake.Walk(ctx, nil, func(id uint64, stake types.PostConvictionStake) (bool, error) {
		if !stake.Released {
			return false, nil
		}
		if now-stake.StakedAt < retention {
			return false, nil
		}
		gcIDs = append(gcIDs, id)
		if len(gcIDs) >= maxStakeGcPerBlock {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, id := range gcIDs {
		stake, err := k.PostConvictionStake.Get(ctx, id)
		if err != nil {
			continue
		}
		if err := k.PostConvictionStake.Remove(ctx, id); err != nil {
			sdkCtx.Logger().Warn("post conviction gc: remove failed", "stake_id", id, "error", err)
			continue
		}
		// Released stakes were already removed from the active indexes;
		// best-effort sweep here in case a write was missed historically.
		_ = k.PostConvictionStakesByPost.Remove(ctx, collections.Join(stake.PostId, id))
		_ = k.PostConvictionStakesByStaker.Remove(ctx, collections.Join(stake.Staker, id))
	}
	return nil
}
