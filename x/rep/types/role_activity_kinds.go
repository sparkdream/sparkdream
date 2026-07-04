package types

import "cosmossdk.io/math"

// Action kinds recorded on RoleActivity counter maps. Owned by x/rep so the
// kind vocabulary (and the policy tables below) live next to the record and
// the reward distribution that consume them. Owning modules report with
// these constants; adding a surface = adding a kind here.
const (
	// Moderation actions (count toward reward-epoch activity).
	ActionKindForumHide     = "forum_hide"
	ActionKindForumLock     = "forum_lock"
	ActionKindForumMove     = "forum_move"
	ActionKindForumPin      = "forum_pin"
	ActionKindForumCuration = "forum_curation"
	ActionKindCollectHide   = "collect_hide"

	// Appeals filed AGAINST a role holder's actions. Not sentinel work —
	// excluded from the activity gate; feeds the Gate 4 appeal-rate check.
	ActionKindForumAppealFiled   = "forum_appeal_filed"
	ActionKindCollectAppealFiled = "collect_appeal_filed"
)

// RoleAccuracyRingSize is the fixed number of reward-epoch slots in a
// RoleActivity accuracy ring. Must stay >= the reward params'
// SentinelAccuracyWindowEpochs read window so ring overwrites never drop
// in-window data. (Value carried over from forum's SentinelAccuracyRingSize
// when the ring moved to rep.)
const RoleAccuracyRingSize = 24

// ActivityKinds are the kinds that count toward the reward-eligibility
// epoch-activity gate (Gate 3) — the role holder's own moderation work.
var ActivityKinds = []string{
	ActionKindForumHide,
	ActionKindForumLock,
	ActionKindForumMove,
	ActionKindForumPin,
	ActionKindForumCuration,
	ActionKindCollectHide,
}

// HideKinds are the hide-action kinds; together with AppealFiledKinds they
// form the cross-surface Gate 4 appeal-rate check (appeals filed / hides).
var HideKinds = []string{ActionKindForumHide, ActionKindCollectHide}

// AppealFiledKinds are the appeal-filed kinds (see Gate 4).
var AppealFiledKinds = []string{ActionKindForumAppealFiled, ActionKindCollectAppealFiled}

// ScoreWeights maps action kinds to their reward-score bonus per epoch
// action. Kinds absent from the map contribute activity (Gate 3) but no
// score bonus — forum_pin deliberately so, preserving the pre-migration
// score formula.
var ScoreWeights = map[string]math.LegacyDec{
	ActionKindForumHide:     math.LegacyNewDecWithPrec(1, 2), // 0.01
	ActionKindForumLock:     math.LegacyNewDecWithPrec(5, 2), // 0.05
	ActionKindForumMove:     math.LegacyNewDecWithPrec(3, 2), // 0.03
	ActionKindForumCuration: math.LegacyNewDecWithPrec(2, 2), // 0.02
	ActionKindCollectHide:   math.LegacyNewDecWithPrec(1, 2), // 0.01
}

// ActionKindForGovAction maps a jury-adjudicated forum gov-action type to
// its RoleActivity action kind.
func ActionKindForGovAction(t GovActionType) string {
	switch t {
	case GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:
		return ActionKindForumLock
	case GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:
		return ActionKindForumMove
	case GovActionType_GOV_ACTION_TYPE_REPLY_PIN:
		return ActionKindForumPin
	default:
		return ActionKindForumHide
	}
}

// CooldownOnOverturn marks the kinds whose overturned verdicts start the
// shared overturn cooldown. Curation-proposal rejections deliberately do
// NOT — the cooldown is the moderation-action circuit breaker, and a
// rejected curation proposal must not lock the role holder out of
// moderation (the accuracy hit + demotion ratchet are the cost).
var CooldownOnOverturn = map[string]bool{
	ActionKindForumHide:   true,
	ActionKindForumLock:   true,
	ActionKindForumMove:   true,
	ActionKindForumPin:    true,
	ActionKindCollectHide: true,
}
