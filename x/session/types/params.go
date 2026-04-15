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

	// DefaultAllowedMsgTypes is the genesis ceiling and initial active allowlist.
	// Each message was reviewed as low-risk, high-frequency content operations
	// safe for ephemeral key delegation. Only expandable via chain upgrade.
	DefaultAllowedMsgTypes = []string{
		// x/blog
		"/sparkdream.blog.v1.MsgCreatePost",
		"/sparkdream.blog.v1.MsgUpdatePost",
		"/sparkdream.blog.v1.MsgCreateReply",
		"/sparkdream.blog.v1.MsgEditReply",
		"/sparkdream.blog.v1.MsgReact",
		"/sparkdream.blog.v1.MsgRemoveReaction",
		// x/forum
		"/sparkdream.forum.v1.MsgCreatePost",
		"/sparkdream.forum.v1.MsgEditPost",
		"/sparkdream.forum.v1.MsgUpvotePost",
		"/sparkdream.forum.v1.MsgDownvotePost",
		"/sparkdream.forum.v1.MsgFollowThread",
		"/sparkdream.forum.v1.MsgUnfollowThread",
		"/sparkdream.forum.v1.MsgMarkAcceptedReply",
		"/sparkdream.forum.v1.MsgConfirmProposedReply",
		"/sparkdream.forum.v1.MsgRejectProposedReply",
		// x/name
		"/sparkdream.name.v1.MsgSetPrimary",
		"/sparkdream.name.v1.MsgUpdateName",
		// x/collect
		"/sparkdream.collect.v1.MsgReact",
		"/sparkdream.collect.v1.MsgRemoveReaction",
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
) Params {
	return Params{
		MaxAllowedMsgTypes:    maxAllowedMsgTypes,
		AllowedMsgTypes:       allowedMsgTypes,
		MaxSessionsPerGranter: maxSessionsPerGranter,
		MaxMsgTypesPerSession: maxMsgTypesPerSession,
		MaxExpiration:         maxExpiration,
		MaxSpendLimit:         maxSpendLimit,
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
	}
}
