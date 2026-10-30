package types

import (
	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "session"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	GovModuleName = "gov"
)

// Store prefixes. The chain is pre-launch, so the legacy Session collections
// (prefixes 0-3) are reused for the unified Grant collections rather than
// preserved alongside; there is no committed state to migrate.
var (
	ParamsKey = collections.NewPrefix("p_session")

	// Primary store: id -> Grant.
	GrantsKey = collections.NewPrefix(0)

	// Secondary indexes.
	GrantsByGranterKey        = collections.NewPrefix(1)
	GrantsByGranteeKey        = collections.NewPrefix(2)
	GrantsByExpirationKey     = collections.NewPrefix(3)
	GrantSeqKey               = collections.NewPrefix(4)
	GrantsByTypeAndGranterKey = collections.NewPrefix(5)
	ActiveGrantCountByTypeKey = collections.NewPrefix(6)

	// Back-compat lookup: (granter, grantee) -> grant_id for SESSION_KEY
	// grants only. Enforces the legacy invariant that there is at most
	// one active session key per (granter, grantee) pair.
	SessionKeyByPairKey = collections.NewPrefix(7)

	// EpochSpend: (grant_id, utc_day_index) -> sdk.Int (string-encoded
	// spent_in_day). Backs the per-grant max_per_epoch self-throttle
	// on RECURRING_PULL grants. UTC-day buckets prune lazily; bounded to
	// O(7) entries per active grant in steady state.
	EpochSpendByGrantKey = collections.NewPrefix(8)

	// OneshotGasDeposit: grant_id -> Coin. SPARK deposit posted at
	// MsgCreateGrant time for SCHEDULED_ONESHOT grants (both Transfer and
	// Exec variants per Rev 4). Refunded on Revoke / Decline / paused-TTL
	// auto-revoke; sent to fee collector on FIRED.
	OneshotGasDepositKey = collections.NewPrefix(9)

	// PausedOneshotByPauseTime: (pause_unix, grant_id) -> empty. Drives
	// the EndBlocker paused-TTL auto-revoke pass.
	PausedOneshotByPauseTimeKey = collections.NewPrefix(10)

	// GrantsByFireTime: (fire_at_unix, grant_id) -> empty for ACTIVE
	// SCHEDULED_ONESHOT grants. The EndBlocker fire pass range-walks it up
	// to block time instead of decoding every grant in the store.
	GrantsByFireTimeKey = collections.NewPrefix(11)

	// GrantCreatorModule: grant_id -> module address, for grants created
	// through CreateGrantOnBehalfOf. Binds the bypass decline and claim
	// paths to the module that created the grant.
	GrantCreatorModuleKey = collections.NewPrefix(12)
)

// NonDelegableSessionMsgs are session module messages that can never appear in the
// allowlist. This prevents recursive execution (MsgExecSession containing MsgExecSession)
// and session-key self-management. Every new x/session signer Msg defaults to
// denylisted at the time it lands; removing one requires a security review.
var NonDelegableSessionMsgs = map[string]bool{
	"/sparkdream.session.v1.MsgCreateSession":           true,
	"/sparkdream.session.v1.MsgRevokeSession":           true,
	"/sparkdream.session.v1.MsgExecSession":             true,
	"/sparkdream.session.v1.MsgCreateGrant":             true,
	"/sparkdream.session.v1.MsgDeclineGrant":            true,
	"/sparkdream.session.v1.MsgClaimRecurringPull":      true,
	"/sparkdream.session.v1.MsgPullAllowance":           true,
	"/sparkdream.session.v1.MsgRetryScheduledOneshot":   true,
	"/sparkdream.session.v1.MsgUpdateParams":            true,
	"/sparkdream.session.v1.MsgUpdateOperationalParams": true,
	// MsgRevokeGrant is intentionally absent (Rev 2 §7.2) — it's
	// opt-in via SessionKeyPayload.allow_self_revoke, scoped to the
	// same-granter at the msg-server level.
}

// MsgRevokeGrantTypeURL is the canonical type URL of MsgRevokeGrant.
// Used by validateSessionKeyPayload to enforce that the
// allow_self_revoke gate must be true when the session key includes
// this msg type in its allowlist.
const MsgRevokeGrantTypeURL = "/sparkdream.session.v1.MsgRevokeGrant"

// DreamFieldsToStrip maps message type URLs to field names that commit DREAM
// tokens and must be zeroed when dispatched via session key.
var DreamFieldsToStrip = map[string][]string{
	"/sparkdream.blog.v1.MsgCreatePost":  {"author_bond"},
	"/sparkdream.blog.v1.MsgCreateReply": {"author_bond"},
	"/sparkdream.forum.v1.MsgCreatePost": {"author_bond"},
}

// ContextKeySessionFeePaid is set by SessionFeeDecorator when it pays fees
// on behalf of the granter. It is the one typed key shared with x/shield
// (x/common/types.ContextKeyFeePaid), so SkipIfFeePaidDecorator detects it.
var ContextKeySessionFeePaid = commontypes.ContextKeyFeePaid

// fireInProgressCtxKey is the private type of ContextKeySessionFireInProgress.
type fireInProgressCtxKey struct{}

// ContextKeySessionFireInProgress is set (to the grant id, uint64) on the
// child context of a ScheduledOneshot Exec fire (spec §23.3). It is
// informational only — telemetry, events and re-entrance detection.
// Downstream handlers MUST NOT branch on it to skip ante-equivalent logic:
// the OneshotExec security contract is the audited allowed_msg_types list.
var ContextKeySessionFireInProgress = fireInProgressCtxKey{}
