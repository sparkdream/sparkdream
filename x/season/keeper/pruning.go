package keeper

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"
)

// History pruning (SEA-11). Each block walks a small window of every
// history collection from a persisted cursor, deletes the records past their
// retention param, and wraps around at the end, so the whole collection is
// revisited continuously at a bounded per-block cost. Guild invites are
// expired on the invite_cleanup_* cadence.

// historyPruneBudget bounds the records examined per collection per block.
const historyPruneBudget = 50

// closedModerationRetentionBlocks keeps a closed display-name moderation
// record queryable for about 90 days (6 s blocks) after the moderation
// (moderated_at is a block height).
const closedModerationRetentionBlocks = int64(90 * 24 * 60 * 10)

// pruneOldNominations walks NominationsBySeason from the oldest season and
// deletes nominations of seasons at or past the retention that carry no
// stake, at most budget index entries per block, resuming at a persisted
// cursor ("season:id") and wrapping at the retention boundary.
func (k Keeper) pruneOldNominations(ctx context.Context, current, retention uint64, budget int) error {
	if retention == 0 || current <= retention {
		return nil
	}
	cutoff := current - retention // seasons <= cutoff are prunable
	cursor, err := k.PruneCursor.Get(ctx, "nomination")
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	rng := new(collections.Range[collections.Pair[uint64, uint64]]).
		EndExclusive(collections.Join(cutoff+1, uint64(0)))
	if s, id, ok := strings.Cut(cursor, ":"); ok {
		sn, e1 := strconv.ParseUint(s, 10, 64)
		in, e2 := strconv.ParseUint(id, 10, 64)
		if e1 == nil && e2 == nil {
			rng = rng.StartExclusive(collections.Join(sn, in))
		}
	}
	var drop []collections.Pair[uint64, uint64]
	seen := 0
	var last collections.Pair[uint64, uint64]
	if err := k.NominationsBySeason.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
		seen++
		last = key
		staked := false
		if err := k.NominationStake.Walk(ctx, nominationStakeRange(key.K2()), func(string, types.NominationStake) (bool, error) {
			staked = true
			return true, nil
		}); err != nil {
			return true, err
		}
		// A nomination whose payout is still being retried is kept until
		// the retry pays or abandons it (SEA-21).
		owed, err := k.RetroPayoutRetry.Has(ctx, key)
		if err != nil {
			return true, err
		}
		if !staked && !owed {
			drop = append(drop, key)
		}
		return seen >= budget, nil
	}); err != nil {
		return err
	}
	for _, key := range drop {
		if nom, err := k.Nomination.Get(ctx, key.K2()); err == nil {
			if err := k.unindexNomination(ctx, nom); err != nil {
				return err
			}
		}
		if err := k.Nomination.Remove(ctx, key.K2()); err != nil {
			return err
		}
		if err := k.NominationsBySeason.Remove(ctx, key); err != nil {
			return err
		}
	}
	if seen < budget {
		return k.PruneCursor.Remove(ctx, "nomination")
	}
	return k.PruneCursor.Set(ctx, "nomination", strconv.FormatUint(last.K1(), 10)+":"+strconv.FormatUint(last.K2(), 10))
}

// rollingPrune examines up to budget entries of m after the stored cursor
// for `name`, removes those stale() reports, and advances (or wraps) the
// cursor.
func rollingPrune[V any](ctx context.Context, k Keeper, name string, m collections.Map[string, V], budget int, stale func(key string, v V) bool) error {
	cursor, err := k.PruneCursor.Get(ctx, name)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	var toRemove []string
	seen := 0
	last := ""
	if err := m.Walk(ctx, cursorAfter(cursor), func(key string, v V) (bool, error) {
		seen++
		last = key
		if stale(key, v) {
			toRemove = append(toRemove, key)
		}
		return seen >= budget, nil
	}); err != nil {
		return err
	}
	for _, key := range toRemove {
		if err := m.Remove(ctx, key); err != nil {
			return err
		}
	}
	if seen < budget {
		// Reached the end: start over next block.
		return k.PruneCursor.Remove(ctx, name)
	}
	return k.PruneCursor.Set(ctx, name, last)
}

// leadingUint parses the decimal before the first sep in key.
func leadingUint(key string, sep string) (uint64, bool) {
	head, _, ok := strings.Cut(key, sep)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseUint(head, 10, 64)
	return n, err == nil
}

// trailingInt parses the decimal after the last sep in key.
func trailingInt(key string, sep string) (int64, bool) {
	i := strings.LastIndex(key, sep)
	if i < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(key[i+len(sep):], 10, 64)
	return n, err == nil
}

// pruneHistory runs one bounded pruning step over every history collection.
// Errors are logged, never returned: pruning must not fail a block.
func (k Keeper) pruneHistory(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}
	season, err := k.Season.Get(ctx)
	if err != nil {
		return
	}
	epoch := k.GetCurrentEpoch(ctx)
	logErr := func(name string, err error) {
		if err != nil {
			sdkCtx.Logger().Error("season history pruning failed", "collection", name, "error", err)
		}
	}

	// EpochXpTracker "member:epoch": per-epoch caps only look at the
	// current epoch.
	logErr("epochXpTracker", rollingPrune(ctx, k, "epochXpTracker", k.EpochXpTracker, historyPruneBudget,
		func(key string, _ types.EpochXpTracker) bool {
			e, ok := trailingInt(key, ":")
			return ok && e+int64(params.EpochTrackerRetentionEpochs) < epoch
		}))

	// VoteXpRecord "season:member:proposal": the dedup is per season.
	logErr("voteXpRecord", rollingPrune(ctx, k, "voteXpRecord", k.VoteXpRecord, historyPruneBudget,
		func(key string, _ types.VoteXpRecord) bool {
			s, ok := leadingUint(key, ":")
			return ok && s+uint64(params.VoteXpRecordRetentionSeasons) <= season.Number
		}))

	// ForumXpCooldown: never prune inside the longest cooldown it enforces.
	forumKeep := int64(params.ForumCooldownRetentionEpochs)
	if c := commontypes.Saturate[int64](params.ForumXpReciprocalCooldownEpochs); c > forumKeep {
		forumKeep = c
	}
	if c := commontypes.Saturate[int64](params.ForumXpSelfReplyCooldownEpochs); c > forumKeep {
		forumKeep = c
	}
	logErr("forumXpCooldown", rollingPrune(ctx, k, "forumXpCooldown", k.ForumXpCooldown, historyPruneBudget,
		func(_ string, v types.ForumXpCooldown) bool {
			return v.LastGrantedEpoch+forumKeep < epoch
		}))

	// MemberSeasonSnapshot "season/address".
	logErr("memberSeasonSnapshot", rollingPrune(ctx, k, "memberSeasonSnapshot", k.MemberSeasonSnapshot, historyPruneBudget,
		func(key string, _ types.MemberSeasonSnapshot) bool {
			s, ok := leadingUint(key, "/")
			return ok && s+uint64(params.SnapshotRetentionSeasons) <= season.Number
		}))

	// RetroRewardRecord "season/nominationId": payout history, kept as long
	// as member snapshots.
	logErr("retroRewardRecord", rollingPrune(ctx, k, "retroRewardRecord", k.RetroRewardRecord, historyPruneBudget,
		func(key string, _ types.RetroRewardRecord) bool {
			s, ok := leadingUint(key, "/")
			return ok && s+uint64(params.SnapshotRetentionSeasons) <= season.Number
		}))

	// Nominations of seasons past the same retention, once no stake is left
	// on them (a stake still locks DREAM until its owner unstakes).
	logErr("nomination", k.pruneOldNominations(ctx, season.Number, uint64(params.SnapshotRetentionSeasons), historyPruneBudget))

	// Closed display-name moderation records (not active, no appeal stake
	// outstanding) past closedModerationRetentionBlocks.
	height := sdkCtx.BlockHeight()
	logErr("displayNameModeration", rollingPrune(ctx, k, "displayNameModeration", k.DisplayNameModeration, historyPruneBudget,
		func(_ string, v types.DisplayNameModeration) bool {
			if v.Active || v.ModeratedAt+closedModerationRetentionBlocks >= height {
				return false
			}
			if v.AppealChallengeId != "" {
				if has, err := k.DisplayNameAppealStake.Has(ctx, v.AppealChallengeId); err != nil || has {
					return false
				}
			}
			return true
		}))

	// Guild invites past their expiry, on the invite_cleanup_* cadence.
	if params.InviteCleanupIntervalBlocks > 0 && params.InviteCleanupBatchSize > 0 &&
		sdkCtx.BlockHeight()%int64(params.InviteCleanupIntervalBlocks) == 0 {
		logErr("guildInvite", k.cleanupExpiredGuildInvites(ctx, int(params.InviteCleanupBatchSize), epoch))
	}
}

// cleanupExpiredGuildInvites expires up to budget guild invites past their
// expiry epoch, freeing the guild's pending-invite slots.
func (k Keeper) cleanupExpiredGuildInvites(ctx context.Context, budget int, epoch int64) error {
	cursor, err := k.PruneCursor.Get(ctx, "guildInvite")
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	type expired struct {
		key     string
		invitee string
	}
	var due []expired
	seen := 0
	last := ""
	if err := k.GuildInvite.Walk(ctx, cursorAfter(cursor), func(key string, inv types.GuildInvite) (bool, error) {
		seen++
		last = key
		if inv.ExpiresEpoch > 0 && epoch > inv.ExpiresEpoch {
			_, invitee, _ := strings.Cut(key, ":")
			due = append(due, expired{key, invitee})
		}
		return seen >= budget, nil
	}); err != nil {
		return err
	}
	for _, e := range due {
		if err := k.expireGuildInvite(ctx, extractGuildIdFromInviteKey(e.key), e.invitee); err != nil {
			return err
		}
	}
	if seen < budget {
		return k.PruneCursor.Remove(ctx, "guildInvite")
	}
	return k.PruneCursor.Set(ctx, "guildInvite", last)
}

// expireGuildInvite removes an invite and its slot in the guild's pending
// list. A slot whose invite record is already gone (a dangling slot) is
// still freed: the missing record is not an error.
func (k Keeper) expireGuildInvite(ctx context.Context, guildID uint64, invitee string) error {
	if err := k.GuildInvite.Remove(ctx, guildInviteKey(guildID, invitee)); err != nil && !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return nil // guild gone; nothing else to free
	}
	kept := guild.PendingInvites[:0]
	for _, a := range guild.PendingInvites {
		if a != invitee {
			kept = append(kept, a)
		}
	}
	guild.PendingInvites = kept
	return k.Guild.Set(ctx, guildID, guild)
}

// pruneExpiredGuildInvitesOf expires every expired invite of one guild,
// freeing slots before an InviteToGuild capacity check (bounded by
// max_pending_invites).
func (k Keeper) pruneExpiredGuildInvitesOf(ctx context.Context, guildID uint64, epoch int64) error {
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return err
	}
	for _, invitee := range append([]string(nil), guild.PendingInvites...) {
		inv, err := k.GuildInvite.Get(ctx, guildInviteKey(guildID, invitee))
		if err != nil {
			// Dangling slot with no invite record: free it.
			if err := k.expireGuildInvite(ctx, guildID, invitee); err != nil {
				return err
			}
			continue
		}
		if inv.ExpiresEpoch > 0 && epoch > inv.ExpiresEpoch {
			if err := k.expireGuildInvite(ctx, guildID, invitee); err != nil {
				return err
			}
		}
	}
	return nil
}

// guildInviteKey is the GuildInvite key, "guildId:invitee".
func guildInviteKey(guildID uint64, invitee string) string {
	return strconv.FormatUint(guildID, 10) + ":" + invitee
}
