package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"
)

// moderationQueued reports whether a moderation belongs in ModerationQueue:
// active and not under appeal (appealed records resolve through
// ResolveDisplayNameAppeal, never by timeout).
func moderationQueued(m types.DisplayNameModeration) bool {
	return m.Active && m.AppealChallengeId == "" && m.AutoResolveAttempts == 0
}

// moderationRetryQueued reports whether a moderation belongs in
// ModerationRetryQueue: active, unappealed, and an automatic resolution
// failed and is due again at auto_resolve_retry_at (SEA-09).
func moderationRetryQueued(m types.DisplayNameModeration) bool {
	return m.Active && m.AppealChallengeId == "" && m.AutoResolveRetryAt > 0
}

// MaxModerationAutoResolveAttempts bounds the automatic resolutions of one
// unappealed moderation. After the last failure the record leaves the
// queues and waits for MsgResolveUnappealedModeration (SEA-09).
const MaxModerationAutoResolveAttempts = 5

// ModerationRetryBaseBlocks is the backoff before the first retry; it
// doubles with every further failure.
const ModerationRetryBaseBlocks = 100

// appealQueued reports whether a moderation belongs in AppealQueue: active
// and under appeal. Appeals nobody resolves time out (SEA-23).
func appealQueued(m types.DisplayNameModeration) bool {
	return m.Active && m.AppealChallengeId != ""
}

// AppealResolutionPeriodMultiplier sets how long an appeal may wait for a
// committee verdict before it times out: this many
// display_name_appeal_period_blocks after the appeal (SEA-23). Three report
// windows (about three weeks on networks) leave the Operations Committee
// time for a proposal.
const AppealResolutionPeriodMultiplier = 3

// appealDeadline is the last block an appeal filed at appealedAt waits for a
// verdict.
func appealDeadline(appealedAt int64, params types.Params) int64 {
	return appealedAt + AppealResolutionPeriodMultiplier*commontypes.Saturate[int64](params.DisplayNameAppealPeriodBlocks)
}

// MaxBlockedDisplayNames caps DisplayNameModeration.blocked_names; the
// oldest entry drops out first.
const MaxBlockedDisplayNames = 5

// normalizeDisplayName is the comparison form of a display name for the
// blocked-name check: trimmed and case-folded.
func normalizeDisplayName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// blockName appends name to m.BlockedNames (deduplicated, capped).
func blockName(m *types.DisplayNameModeration, name string) {
	n := normalizeDisplayName(name)
	if n == "" {
		return
	}
	for _, b := range m.BlockedNames {
		if b == n {
			return
		}
	}
	m.BlockedNames = append(m.BlockedNames, n)
	if len(m.BlockedNames) > MaxBlockedDisplayNames {
		m.BlockedNames = m.BlockedNames[len(m.BlockedNames)-MaxBlockedDisplayNames:]
	}
}

// isDisplayNameBlocked reports whether member may not use name: it was
// upheld as rejected for them in a closed case (SEA-23).
func (k Keeper) isDisplayNameBlocked(ctx context.Context, member, name string) bool {
	m, err := k.DisplayNameModeration.Get(ctx, member)
	if err != nil {
		return false
	}
	n := normalizeDisplayName(name)
	for _, b := range m.BlockedNames {
		if b == n {
			return true
		}
	}
	return false
}

// SetDisplayNameModeration writes a moderation record and keeps
// ModerationQueue in step with it. Every write of DisplayNameModeration goes
// through here so the BeginBlocker auto-resolution walks only live,
// unappealed records in moderated_at order (SEA-09).
func (k Keeper) SetDisplayNameModeration(ctx context.Context, member string, m types.DisplayNameModeration) error {
	old, err := k.DisplayNameModeration.Get(ctx, member)
	switch {
	case err == nil:
		if moderationQueued(old) {
			if err := k.ModerationQueue.Remove(ctx, collections.Join(old.ModeratedAt, member)); err != nil {
				return err
			}
		}
		if moderationRetryQueued(old) {
			if err := k.ModerationRetryQueue.Remove(ctx, collections.Join(old.AutoResolveRetryAt, member)); err != nil {
				return err
			}
		}
		if appealQueued(old) {
			if err := k.AppealQueue.Remove(ctx, collections.Join(old.AppealedAt, member)); err != nil {
				return err
			}
		}
	case !errors.Is(err, collections.ErrNotFound):
		return err
	}
	if err := k.DisplayNameModeration.Set(ctx, member, m); err != nil {
		return err
	}
	if moderationQueued(m) {
		if err := k.ModerationQueue.Set(ctx, collections.Join(m.ModeratedAt, member)); err != nil {
			return err
		}
	}
	if moderationRetryQueued(m) {
		if err := k.ModerationRetryQueue.Set(ctx, collections.Join(m.AutoResolveRetryAt, member)); err != nil {
			return err
		}
	}
	if appealQueued(m) {
		return k.AppealQueue.Set(ctx, collections.Join(m.AppealedAt, member))
	}
	return nil
}
