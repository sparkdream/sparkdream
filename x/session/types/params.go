package types

import (
	"fmt"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Default parameter values
var (
	DefaultMaxSessionsPerGranter uint64 = 10
	DefaultMaxMsgTypesPerSession uint64 = 20
	DefaultMaxExpiration                = 7 * 24 * time.Hour                      // 7 days
	DefaultMaxSpendLimit                = sdk.NewInt64Coin("uspark", 100_000_000) // 100 SPARK
	DefaultMaxExecCount          uint64 = 10_000                                  // per-session execution ceiling

	// DefaultAllowedMsgTypes is the genesis ceiling and initial active allowlist.
	// Each message was reviewed as a low-risk, UX-frequent content/identity/social
	// operation safe for ephemeral key delegation. Excluded across all modules:
	// any message that locks/burns/transfers SPARK or DREAM, requires a bonded
	// role / committee / council privilege, or initiates a dispute that escrows
	// fees. Only expandable via chain upgrade.
	DefaultAllowedMsgTypes = []string{
		// x/blog — content CRUD + reactions (DREAM author_bond fields stripped at dispatch)
		"/sparkdream.blog.v1.MsgCreatePost",
		"/sparkdream.blog.v1.MsgUpdatePost",
		"/sparkdream.blog.v1.MsgCreateReply",
		"/sparkdream.blog.v1.MsgEditReply",
		"/sparkdream.blog.v1.MsgReact",
		"/sparkdream.blog.v1.MsgRemoveReaction",
		// x/forum — content CRUD + voting + thread subscription (author_bond stripped)
		"/sparkdream.forum.v1.MsgCreatePost",
		"/sparkdream.forum.v1.MsgEditPost",
		"/sparkdream.forum.v1.MsgUpvotePost",
		"/sparkdream.forum.v1.MsgDownvotePost",
		"/sparkdream.forum.v1.MsgFollowThread",
		"/sparkdream.forum.v1.MsgUnfollowThread",
		"/sparkdream.forum.v1.MsgMarkAcceptedReply",
		"/sparkdream.forum.v1.MsgConfirmProposedReply",
		"/sparkdream.forum.v1.MsgRejectProposedReply",
		// x/name — identity metadata (no fee, no dispute). MsgRegisterName, transfer,
		// and dispute messages are deliberately excluded.
		"/sparkdream.name.v1.MsgSetPrimary",
		"/sparkdream.name.v1.MsgUpdateName",
		"/sparkdream.name.v1.MsgSetDisplayName",
		"/sparkdream.name.v1.MsgSetTarget",
		"/sparkdream.name.v1.MsgAcceptTarget",
		// x/collect — UX-frequent collection actions. Excluded: any item/collection
		// CRUD that escrows a SPARK deposit, MsgDownvoteContent (burns SPARK),
		// MsgFlagContent (non-member spam tax), endorsement/sponsorship (lock DREAM),
		// and MsgRateCollection (requires bonded curator role).
		"/sparkdream.collect.v1.MsgReact",
		"/sparkdream.collect.v1.MsgRemoveReaction",
		"/sparkdream.collect.v1.MsgUpvoteContent",
		"/sparkdream.collect.v1.MsgUpdateItem",
		"/sparkdream.collect.v1.MsgReorderItem",
		"/sparkdream.collect.v1.MsgSetSeekingEndorsement",
		// x/season — gamification UX (guild membership, quests, identity cosmetics).
		// Excluded: MsgCreateGuild and MsgSetUsername (burn DREAM), report/appeal
		// (lock DREAM), founder-transfer/dissolve (rare, identity-significant), and
		// all admin Msgs (Create/Update/Delete{Achievement,Title,Quest}, season
		// transition controls).
		"/sparkdream.season.v1.MsgJoinGuild",
		"/sparkdream.season.v1.MsgLeaveGuild",
		"/sparkdream.season.v1.MsgAcceptGuildInvite",
		"/sparkdream.season.v1.MsgInviteToGuild",
		"/sparkdream.season.v1.MsgRevokeGuildInvite",
		"/sparkdream.season.v1.MsgKickFromGuild",
		"/sparkdream.season.v1.MsgUpdateGuildDescription",
		"/sparkdream.season.v1.MsgSetGuildInviteOnly",
		"/sparkdream.season.v1.MsgPromoteToOfficer",
		"/sparkdream.season.v1.MsgDemoteOfficer",
		"/sparkdream.season.v1.MsgSetDisplayName",
		"/sparkdream.season.v1.MsgSetDisplayTitle",
		"/sparkdream.season.v1.MsgStartQuest",
		"/sparkdream.season.v1.MsgAbandonQuest",
		"/sparkdream.season.v1.MsgClaimQuestReward",
		// x/service — bridge-operator daemon ergonomics (federation→
		// service migration, Phase 8). Three of these (UnbondOperator,
		// UpdateMetadata, ClaimUnbondedBond) are purely
		// state-machine/metadata changes with no SPARK transfer, so
		// they fit the "ephemeral key UX" criterion cleanly.
		// MsgTopUpBond IS a SPARK transfer and breaks the strict no-
		// transfer rule above; we admit it anyway because the SpendLimit
		// on the session bounds the maximum a compromised daemon can
		// move (a granter setting up a session for a bridge daemon
		// must size SpendLimit to their intended top-up budget; the
		// daemon cannot exceed that). Without this entry, daemons that
		// need to top up to recover from underfunded state would
		// require an out-of-band human-signed tx, which defeats the
		// session UX goal.
		"/sparkdream.service.v1.MsgUnbondOperator",
		"/sparkdream.service.v1.MsgTopUpBond",
		"/sparkdream.service.v1.MsgUpdateMetadata",
		"/sparkdream.service.v1.MsgClaimUnbondedBond",
	}
)

// NewParams creates a new Params instance.
func NewParams(
	maxAllowedMsgTypes []string,
	allowedMsgTypes []string,
	maxSessionsPerGranter uint64,
	maxMsgTypesPerSession uint64,
	maxExpiration time.Duration,
	maxSpendLimit sdk.Coin,
	maxExecCount uint64,
) Params {
	return Params{
		MaxAllowedMsgTypes:    maxAllowedMsgTypes,
		AllowedMsgTypes:       allowedMsgTypes,
		MaxSessionsPerGranter: maxSessionsPerGranter,
		MaxMsgTypesPerSession: maxMsgTypesPerSession,
		MaxExpiration:         maxExpiration,
		MaxSpendLimit:         maxSpendLimit,
		MaxExecCount:          maxExecCount,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	// At genesis, ceiling and active list are identical.
	ceiling := make([]string, len(DefaultAllowedMsgTypes))
	copy(ceiling, DefaultAllowedMsgTypes)
	active := make([]string, len(DefaultAllowedMsgTypes))
	copy(active, DefaultAllowedMsgTypes)

	return NewParams(
		ceiling,
		active,
		DefaultMaxSessionsPerGranter,
		DefaultMaxMsgTypesPerSession,
		DefaultMaxExpiration,
		DefaultMaxSpendLimit,
		DefaultMaxExecCount,
	)
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.MaxSessionsPerGranter == 0 {
		return fmt.Errorf("max_sessions_per_granter must be > 0")
	}
	if p.MaxMsgTypesPerSession == 0 {
		return fmt.Errorf("max_msg_types_per_session must be > 0")
	}
	if p.MaxExpiration <= 0 {
		return fmt.Errorf("max_expiration must be > 0")
	}
	if !p.MaxSpendLimit.IsValid() || p.MaxSpendLimit.IsZero() {
		return fmt.Errorf("max_spend_limit must be a valid positive coin")
	}
	if p.MaxExecCount == 0 {
		return fmt.Errorf("max_exec_count must be > 0")
	}
	if len(p.MaxAllowedMsgTypes) == 0 {
		return fmt.Errorf("max_allowed_msg_types must not be empty")
	}
	if len(p.AllowedMsgTypes) == 0 {
		return fmt.Errorf("allowed_msg_types must not be empty")
	}

	// Check for NonDelegableSessionMsgs in ceiling
	for _, msgType := range p.MaxAllowedMsgTypes {
		if NonDelegableSessionMsgs[msgType] {
			return fmt.Errorf("max_allowed_msg_types contains non-delegable message: %s", msgType)
		}
	}

	// Check for NonDelegableSessionMsgs in active list
	for _, msgType := range p.AllowedMsgTypes {
		if NonDelegableSessionMsgs[msgType] {
			return fmt.Errorf("allowed_msg_types contains non-delegable message: %s", msgType)
		}
	}

	// Check allowed_msg_types is subset of max_allowed_msg_types
	ceilingSet := make(map[string]bool, len(p.MaxAllowedMsgTypes))
	for _, msgType := range p.MaxAllowedMsgTypes {
		ceilingSet[msgType] = true
	}
	for _, msgType := range p.AllowedMsgTypes {
		if !ceilingSet[msgType] {
			return fmt.Errorf("allowed_msg_types contains type not in ceiling: %s", msgType)
		}
	}

	// Check for duplicates in ceiling
	seen := make(map[string]bool, len(p.MaxAllowedMsgTypes))
	for _, msgType := range p.MaxAllowedMsgTypes {
		if seen[msgType] {
			return fmt.Errorf("duplicate in max_allowed_msg_types: %s", msgType)
		}
		seen[msgType] = true
	}

	// Check for duplicates in active list
	seen = make(map[string]bool, len(p.AllowedMsgTypes))
	for _, msgType := range p.AllowedMsgTypes {
		if seen[msgType] {
			return fmt.Errorf("duplicate in allowed_msg_types: %s", msgType)
		}
		seen[msgType] = true
	}

	return nil
}

// DefaultSessionOperationalParams returns default operational params.
func DefaultSessionOperationalParams() SessionOperationalParams {
	active := make([]string, len(DefaultAllowedMsgTypes))
	copy(active, DefaultAllowedMsgTypes)

	return SessionOperationalParams{
		AllowedMsgTypes:       active,
		MaxSessionsPerGranter: DefaultMaxSessionsPerGranter,
		MaxMsgTypesPerSession: DefaultMaxMsgTypesPerSession,
		MaxExpiration:         DefaultMaxExpiration,
		MaxSpendLimit:         DefaultMaxSpendLimit,
		MaxExecCount:          DefaultMaxExecCount,
	}
}

// ApplyOperationalParams applies operational params to the full params,
// preserving governance-only fields (max_allowed_msg_types).
func (p Params) ApplyOperationalParams(op SessionOperationalParams) Params {
	p.AllowedMsgTypes = op.AllowedMsgTypes
	p.MaxSessionsPerGranter = op.MaxSessionsPerGranter
	p.MaxMsgTypesPerSession = op.MaxMsgTypesPerSession
	p.MaxExpiration = op.MaxExpiration
	p.MaxSpendLimit = op.MaxSpendLimit
	p.MaxExecCount = op.MaxExecCount
	return p
}

// ExtractOperationalParams extracts the operational params subset from full params.
func (p Params) ExtractOperationalParams() SessionOperationalParams {
	return SessionOperationalParams{
		AllowedMsgTypes:       p.AllowedMsgTypes,
		MaxSessionsPerGranter: p.MaxSessionsPerGranter,
		MaxMsgTypesPerSession: p.MaxMsgTypesPerSession,
		MaxExpiration:         p.MaxExpiration,
		MaxSpendLimit:         p.MaxSpendLimit,
		MaxExecCount:          p.MaxExecCount,
	}
}
