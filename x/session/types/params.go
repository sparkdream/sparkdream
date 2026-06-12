package types

import (
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// Default parameter values
var (
	DefaultMaxSessionsPerGranter uint64 = 10
	DefaultMaxMsgTypesPerSession uint64 = 20
	DefaultMaxExpiration                = 7 * 24 * time.Hour // 7 days
	// DefaultMaxSpendLimitAmount is the default per-session fee budget in
	// bond-denom micro-units (100 SPARK).
	DefaultMaxSpendLimitAmount        = sdkmath.NewInt(100_000_000)
	DefaultMaxExecCount        uint64 = 10_000 // per-session execution ceiling

	// --- RecurringPull defaults (P3) ---
	DefaultMinRecurringPeriodSeconds   int64  = 86_400     // 1 day
	DefaultMaxRecurringDurationSeconds int64  = 31_536_000 // 1 year
	DefaultMaxRecurringPullsPerGranter uint32 = 50

	// --- SpendingAllowance defaults (P4) ---
	DefaultMinAllowancePeriodSeconds int64  = 3_600 // 1 hour
	DefaultMaxAllowancesPerGranter   uint32 = 20
	DefaultMaxAllowanceRecipientList uint32 = 50
	DefaultMinPullAmount                    = "1000" // 1000 bond-denom micro-units (0.001 SPARK)

	// --- ScheduledOneshot defaults (P5) ---
	DefaultMinScheduleDelaySeconds        int64  = 60
	DefaultMaxScheduleHorizonSeconds      int64  = 31_536_000 // 1 year
	DefaultFireToExpiryBufferSeconds      int64  = 3_600      // 1 hour
	DefaultMaxPendingOneshotsPerGranter   uint32 = 100
	DefaultMaxPausedOneshotsPerGranter    uint32 = 20
	DefaultPausedOneshotTtlSeconds        int64  = 604_800 // 7 days
	DefaultMinOneshotExecGas              uint64 = 30_000
	DefaultMaxOneshotExecGas              uint64 = 200_000
	DefaultOneshotGasPrice                       = "0.0025" // 100x typical min_gas_price
	DefaultOneshotCreationFee             uint64 = 1_000
	DefaultMinOneshotDeposit              uint64 = 1_000
	DefaultMaxEndblockerDispatchesPerPass uint32 = 100

	// --- Cross-type defaults (P3) ---
	// DefaultAllowedDenoms is empty: the chain's bond denom is always
	// implicitly allowed; this list adds further permitted denoms (e.g.
	// IBC vouchers) via gov / ops update.
	DefaultAllowedDenoms                 = []string{}
	DefaultMaxGrantLifetimeSeconds int64 = 31_536_000 // 1 year

	// DefaultAuthorizedGrantCreators is the genesis-default allowlist
	// for the module-bypass entrypoints (CreateGrantOnBehalfOf,
	// RevokeGrantInternal, DeclineGrantInternal,
	// ClaimRecurringPullForGrantee). The x/commons RecurringSpend
	// migration relies on this being seeded at block 0 so council
	// recurring-spend handlers can immediately route through the
	// unified registry. A post-launch gov proposal would race against
	// any in-flight schedules.
	//
	// `authtypes.NewModuleAddress("commons").String()` is
	// deterministic, so this default is reproducible across genesis
	// imports. Returns a fresh slice each call to avoid alias-mutation
	// across DefaultParams() invocations.
	DefaultAuthorizedGrantCreators = func() []string {
		return []string{authtypes.NewModuleAddress("commons").String()}
	}

	// DefaultAllowedMsgTypes is the genesis ceiling and initial active allowlist.
	// Each message was reviewed as a low-risk, UX-frequent content/identity/social
	// operation safe for ephemeral key delegation. Excluded across all modules:
	// any message that locks/burns/transfers SPARK or DREAM, requires a bonded
	// role / committee / council privilege (except sentinel hide/unhide, see the
	// x/forum entries), or initiates a dispute that escrows fees. Only expandable
	// via chain upgrade.
	DefaultAllowedMsgTypes = []string{
		// x/blog — content CRUD + reactions (DREAM author_bond fields stripped at dispatch)
		"/sparkdream.blog.v1.MsgCreatePost",
		"/sparkdream.blog.v1.MsgUpdatePost",
		"/sparkdream.blog.v1.MsgCreateReply",
		"/sparkdream.blog.v1.MsgEditReply",
		"/sparkdream.blog.v1.MsgReact",
		"/sparkdream.blog.v1.MsgRemoveReaction",
		// Hide/unhide are author-only self-curation (msg server requires
		// creator == post author), no bond or funds — same risk class as the
		// edit messages above.
		"/sparkdream.blog.v1.MsgHidePost",
		"/sparkdream.blog.v1.MsgUnhidePost",
		"/sparkdream.blog.v1.MsgHideReply",
		"/sparkdream.blog.v1.MsgUnhideReply",
		// Pin/unpin are display-only, reversible, and the trust-level gate is
		// re-checked against the granter at dispatch. MsgMakePostPermanent /
		// MsgMakeReplyPermanent are deliberately excluded: the promotion is
		// irreversible (no demote path), commits permanent state, and burns
		// the granter's per-day make-permanent quota — too much damage for a
		// stolen session key, with no UX-frequency case.
		"/sparkdream.blog.v1.MsgPinPost",
		"/sparkdream.blog.v1.MsgUnpinPost",
		"/sparkdream.blog.v1.MsgPinReply",
		"/sparkdream.blog.v1.MsgUnpinReply",
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
		// Sentinel moderation hide/unhide. Exception to the no-bonded-role-
		// privilege rule: the msg server re-checks the granter's sentinel bond
		// status at dispatch, so a session key grants nothing the granter
		// doesn't already hold, and the per-hide bond reserve is set by forum
		// params rather than by anything the session key controls. Admitted so
		// sentinels can moderate from a UX session without re-signing each
		// action with the full wallet.
		"/sparkdream.forum.v1.MsgHidePost",
		"/sparkdream.forum.v1.MsgUnhidePost",
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
	maxSpendLimitAmount sdkmath.Int,
	maxExecCount uint64,
) Params {
	return Params{
		MaxAllowedMsgTypes:             maxAllowedMsgTypes,
		AllowedMsgTypes:                allowedMsgTypes,
		MaxSessionsPerGranter:          maxSessionsPerGranter,
		MaxMsgTypesPerSession:          maxMsgTypesPerSession,
		MaxExpiration:                  maxExpiration,
		MaxSpendLimitAmount:            maxSpendLimitAmount,
		MaxExecCount:                   maxExecCount,
		MinRecurringPeriodSeconds:      DefaultMinRecurringPeriodSeconds,
		MaxRecurringDurationSeconds:    DefaultMaxRecurringDurationSeconds,
		MaxRecurringPullsPerGranter:    DefaultMaxRecurringPullsPerGranter,
		MinAllowancePeriodSeconds:      DefaultMinAllowancePeriodSeconds,
		MaxAllowancesPerGranter:        DefaultMaxAllowancesPerGranter,
		MaxAllowanceRecipientList:      DefaultMaxAllowanceRecipientList,
		MinPullAmount:                  DefaultMinPullAmount,
		MinScheduleDelaySeconds:        DefaultMinScheduleDelaySeconds,
		MaxScheduleHorizonSeconds:      DefaultMaxScheduleHorizonSeconds,
		FireToExpiryBufferSeconds:      DefaultFireToExpiryBufferSeconds,
		MaxPendingOneshotsPerGranter:   DefaultMaxPendingOneshotsPerGranter,
		MaxPausedOneshotsPerGranter:    DefaultMaxPausedOneshotsPerGranter,
		PausedOneshotTtlSeconds:        DefaultPausedOneshotTtlSeconds,
		MinOneshotExecGas:              DefaultMinOneshotExecGas,
		MaxOneshotExecGas:              DefaultMaxOneshotExecGas,
		OneshotGasPrice:                DefaultOneshotGasPrice,
		OneshotCreationFee:             DefaultOneshotCreationFee,
		MinOneshotDeposit:              DefaultMinOneshotDeposit,
		MaxEndblockerDispatchesPerPass: DefaultMaxEndblockerDispatchesPerPass,
		AllowedDenoms:                  append([]string(nil), DefaultAllowedDenoms...),
		MaxGrantLifetimeSeconds:        DefaultMaxGrantLifetimeSeconds,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	// At genesis, ceiling and active list are identical.
	ceiling := make([]string, len(DefaultAllowedMsgTypes))
	copy(ceiling, DefaultAllowedMsgTypes)
	active := make([]string, len(DefaultAllowedMsgTypes))
	copy(active, DefaultAllowedMsgTypes)

	p := NewParams(
		ceiling,
		active,
		DefaultMaxSessionsPerGranter,
		DefaultMaxMsgTypesPerSession,
		DefaultMaxExpiration,
		DefaultMaxSpendLimitAmount,
		DefaultMaxExecCount,
	)
	// Seed the bypass allowlist with the commons module address so the
	// x/commons RecurringSpend wrapper routes through the unified
	// registry from block 0. See DefaultAuthorizedGrantCreators
	// docstring for rationale.
	p.AuthorizedGrantCreators = DefaultAuthorizedGrantCreators()
	return p
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
	if p.MaxSpendLimitAmount.IsNil() || !p.MaxSpendLimitAmount.IsPositive() {
		return fmt.Errorf("max_spend_limit_amount must be positive, got %s", p.MaxSpendLimitAmount)
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

	// --- RecurringPull / cross-type ---
	if p.MinRecurringPeriodSeconds <= 0 {
		return fmt.Errorf("min_recurring_period_seconds must be > 0")
	}
	if p.MaxRecurringDurationSeconds <= 0 {
		return fmt.Errorf("max_recurring_duration_seconds must be > 0")
	}
	if p.MaxRecurringPullsPerGranter == 0 {
		return fmt.Errorf("max_recurring_pulls_per_granter must be > 0")
	}
	if p.MinAllowancePeriodSeconds <= 0 {
		return fmt.Errorf("min_allowance_period_seconds must be > 0")
	}
	if p.MaxAllowancesPerGranter == 0 {
		return fmt.Errorf("max_allowances_per_granter must be > 0")
	}
	if p.MaxAllowanceRecipientList == 0 {
		return fmt.Errorf("max_allowance_recipient_list must be > 0")
	}
	if p.MinPullAmount == "" {
		return fmt.Errorf("min_pull_amount must be set")
	}
	if minPull, ok := sdkmath.NewIntFromString(p.MinPullAmount); !ok || minPull.IsNegative() {
		return fmt.Errorf("min_pull_amount must parse as a non-negative sdk.Int: %q", p.MinPullAmount)
	}

	// --- ScheduledOneshot ---
	if p.MinScheduleDelaySeconds <= 0 {
		return fmt.Errorf("min_schedule_delay_seconds must be > 0")
	}
	if p.MaxScheduleHorizonSeconds <= 0 {
		return fmt.Errorf("max_schedule_horizon_seconds must be > 0")
	}
	if p.FireToExpiryBufferSeconds <= 0 {
		return fmt.Errorf("fire_to_expiry_buffer_seconds must be > 0")
	}
	if p.MinScheduleDelaySeconds >= p.FireToExpiryBufferSeconds {
		return fmt.Errorf("min_schedule_delay_seconds (%d) must be < fire_to_expiry_buffer_seconds (%d)",
			p.MinScheduleDelaySeconds, p.FireToExpiryBufferSeconds)
	}
	if p.FireToExpiryBufferSeconds >= p.MaxScheduleHorizonSeconds {
		return fmt.Errorf("fire_to_expiry_buffer_seconds (%d) must be < max_schedule_horizon_seconds (%d)",
			p.FireToExpiryBufferSeconds, p.MaxScheduleHorizonSeconds)
	}
	if p.MaxPendingOneshotsPerGranter == 0 {
		return fmt.Errorf("max_pending_oneshots_per_granter must be > 0")
	}
	if p.MaxPausedOneshotsPerGranter == 0 {
		return fmt.Errorf("max_paused_oneshots_per_granter must be > 0")
	}
	if p.PausedOneshotTtlSeconds < 3_600 {
		return fmt.Errorf("paused_oneshot_ttl_seconds must be >= 3600 (1h floor); got %d", p.PausedOneshotTtlSeconds)
	}
	if p.MinOneshotExecGas == 0 {
		return fmt.Errorf("min_oneshot_exec_gas must be > 0")
	}
	if p.MaxOneshotExecGas < p.MinOneshotExecGas {
		return fmt.Errorf("max_oneshot_exec_gas (%d) must be >= min_oneshot_exec_gas (%d)",
			p.MaxOneshotExecGas, p.MinOneshotExecGas)
	}
	if p.OneshotGasPrice == "" {
		return fmt.Errorf("oneshot_gas_price must be set")
	}
	if dec, err := sdkmath.LegacyNewDecFromStr(p.OneshotGasPrice); err != nil || dec.IsNegative() {
		return fmt.Errorf("oneshot_gas_price must parse as a non-negative sdk.Dec: %q", p.OneshotGasPrice)
	}
	if p.MinOneshotDeposit < p.OneshotCreationFee {
		return fmt.Errorf("min_oneshot_deposit (%d) must be >= oneshot_creation_fee (%d)",
			p.MinOneshotDeposit, p.OneshotCreationFee)
	}
	if p.MaxEndblockerDispatchesPerPass == 0 {
		return fmt.Errorf("max_endblocker_dispatches_per_pass must be > 0")
	}

	if p.MaxGrantLifetimeSeconds <= 0 {
		return fmt.Errorf("max_grant_lifetime_seconds must be > 0")
	}

	// --- Module-bypass allowlist ---
	// Each entry must be a syntactically valid bech32 address; the
	// security guarantee is that whoever can edit Params has reviewed
	// these. Duplicates are rejected to keep the list canonical.
	seenAuth := make(map[string]bool, len(p.AuthorizedGrantCreators))
	for _, addr := range p.AuthorizedGrantCreators {
		if _, err := sdk.AccAddressFromBech32(addr); err != nil {
			return fmt.Errorf("authorized_grant_creators contains invalid address %q: %w", addr, err)
		}
		if seenAuth[addr] {
			return fmt.Errorf("duplicate authorized_grant_creators entry: %s", addr)
		}
		seenAuth[addr] = true
	}
	if p.MaxGrantLifetimeSeconds < p.MaxRecurringDurationSeconds {
		return fmt.Errorf("max_grant_lifetime_seconds (%d) must be >= max_recurring_duration_seconds (%d)",
			p.MaxGrantLifetimeSeconds, p.MaxRecurringDurationSeconds)
	}
	// allowed_denoms is the "additional permitted denoms" list — the chain's
	// bond denom is implicitly allowed regardless. Empty (the default) means
	// only the bond denom is allowed. The forbidden-DREAM check moves to
	// per-grant validation in the keeper, where the chain's dream denom is
	// resolved at runtime via x/identity.
	seenDenom := make(map[string]bool, len(p.AllowedDenoms))
	for _, d := range p.AllowedDenoms {
		if err := sdk.ValidateDenom(d); err != nil {
			return fmt.Errorf("allowed_denoms contains invalid denom %q: %w", d, err)
		}
		if seenDenom[d] {
			return fmt.Errorf("duplicate denom in allowed_denoms: %s", d)
		}
		seenDenom[d] = true
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
		AllowedMsgTypes:                active,
		MaxSessionsPerGranter:          DefaultMaxSessionsPerGranter,
		MaxMsgTypesPerSession:          DefaultMaxMsgTypesPerSession,
		MaxExpiration:                  DefaultMaxExpiration,
		MaxSpendLimitAmount:            DefaultMaxSpendLimitAmount,
		MaxExecCount:                   DefaultMaxExecCount,
		MinRecurringPeriodSeconds:      DefaultMinRecurringPeriodSeconds,
		MaxRecurringDurationSeconds:    DefaultMaxRecurringDurationSeconds,
		MaxRecurringPullsPerGranter:    DefaultMaxRecurringPullsPerGranter,
		MinAllowancePeriodSeconds:      DefaultMinAllowancePeriodSeconds,
		MaxAllowancesPerGranter:        DefaultMaxAllowancesPerGranter,
		MaxAllowanceRecipientList:      DefaultMaxAllowanceRecipientList,
		MinPullAmount:                  DefaultMinPullAmount,
		MinScheduleDelaySeconds:        DefaultMinScheduleDelaySeconds,
		MaxScheduleHorizonSeconds:      DefaultMaxScheduleHorizonSeconds,
		FireToExpiryBufferSeconds:      DefaultFireToExpiryBufferSeconds,
		MaxPendingOneshotsPerGranter:   DefaultMaxPendingOneshotsPerGranter,
		MaxPausedOneshotsPerGranter:    DefaultMaxPausedOneshotsPerGranter,
		PausedOneshotTtlSeconds:        DefaultPausedOneshotTtlSeconds,
		MinOneshotExecGas:              DefaultMinOneshotExecGas,
		MaxOneshotExecGas:              DefaultMaxOneshotExecGas,
		OneshotGasPrice:                DefaultOneshotGasPrice,
		OneshotCreationFee:             DefaultOneshotCreationFee,
		MinOneshotDeposit:              DefaultMinOneshotDeposit,
		MaxEndblockerDispatchesPerPass: DefaultMaxEndblockerDispatchesPerPass,
		AllowedDenoms:                  append([]string(nil), DefaultAllowedDenoms...),
		MaxGrantLifetimeSeconds:        DefaultMaxGrantLifetimeSeconds,
	}
}

// ApplyOperationalParams applies operational params to the full params,
// preserving governance-only fields (max_allowed_msg_types).
func (p Params) ApplyOperationalParams(op SessionOperationalParams) Params {
	p.AllowedMsgTypes = op.AllowedMsgTypes
	p.MaxSessionsPerGranter = op.MaxSessionsPerGranter
	p.MaxMsgTypesPerSession = op.MaxMsgTypesPerSession
	p.MaxExpiration = op.MaxExpiration
	p.MaxSpendLimitAmount = op.MaxSpendLimitAmount
	p.MaxExecCount = op.MaxExecCount
	p.MinRecurringPeriodSeconds = op.MinRecurringPeriodSeconds
	p.MaxRecurringDurationSeconds = op.MaxRecurringDurationSeconds
	p.MaxRecurringPullsPerGranter = op.MaxRecurringPullsPerGranter
	p.MinAllowancePeriodSeconds = op.MinAllowancePeriodSeconds
	p.MaxAllowancesPerGranter = op.MaxAllowancesPerGranter
	p.MaxAllowanceRecipientList = op.MaxAllowanceRecipientList
	p.MinPullAmount = op.MinPullAmount
	p.MinScheduleDelaySeconds = op.MinScheduleDelaySeconds
	p.MaxScheduleHorizonSeconds = op.MaxScheduleHorizonSeconds
	p.FireToExpiryBufferSeconds = op.FireToExpiryBufferSeconds
	p.MaxPendingOneshotsPerGranter = op.MaxPendingOneshotsPerGranter
	p.MaxPausedOneshotsPerGranter = op.MaxPausedOneshotsPerGranter
	p.PausedOneshotTtlSeconds = op.PausedOneshotTtlSeconds
	p.MinOneshotExecGas = op.MinOneshotExecGas
	p.MaxOneshotExecGas = op.MaxOneshotExecGas
	p.OneshotGasPrice = op.OneshotGasPrice
	p.OneshotCreationFee = op.OneshotCreationFee
	p.MinOneshotDeposit = op.MinOneshotDeposit
	p.MaxEndblockerDispatchesPerPass = op.MaxEndblockerDispatchesPerPass
	p.AllowedDenoms = op.AllowedDenoms
	p.MaxGrantLifetimeSeconds = op.MaxGrantLifetimeSeconds
	return p
}

// ExtractOperationalParams extracts the operational params subset from full params.
func (p Params) ExtractOperationalParams() SessionOperationalParams {
	return SessionOperationalParams{
		AllowedMsgTypes:                p.AllowedMsgTypes,
		MaxSessionsPerGranter:          p.MaxSessionsPerGranter,
		MaxMsgTypesPerSession:          p.MaxMsgTypesPerSession,
		MaxExpiration:                  p.MaxExpiration,
		MaxSpendLimitAmount:            p.MaxSpendLimitAmount,
		MaxExecCount:                   p.MaxExecCount,
		MinRecurringPeriodSeconds:      p.MinRecurringPeriodSeconds,
		MaxRecurringDurationSeconds:    p.MaxRecurringDurationSeconds,
		MaxRecurringPullsPerGranter:    p.MaxRecurringPullsPerGranter,
		MinAllowancePeriodSeconds:      p.MinAllowancePeriodSeconds,
		MaxAllowancesPerGranter:        p.MaxAllowancesPerGranter,
		MaxAllowanceRecipientList:      p.MaxAllowanceRecipientList,
		MinPullAmount:                  p.MinPullAmount,
		MinScheduleDelaySeconds:        p.MinScheduleDelaySeconds,
		MaxScheduleHorizonSeconds:      p.MaxScheduleHorizonSeconds,
		FireToExpiryBufferSeconds:      p.FireToExpiryBufferSeconds,
		MaxPendingOneshotsPerGranter:   p.MaxPendingOneshotsPerGranter,
		MaxPausedOneshotsPerGranter:    p.MaxPausedOneshotsPerGranter,
		PausedOneshotTtlSeconds:        p.PausedOneshotTtlSeconds,
		MinOneshotExecGas:              p.MinOneshotExecGas,
		MaxOneshotExecGas:              p.MaxOneshotExecGas,
		OneshotGasPrice:                p.OneshotGasPrice,
		OneshotCreationFee:             p.OneshotCreationFee,
		MinOneshotDeposit:              p.MinOneshotDeposit,
		MaxEndblockerDispatchesPerPass: p.MaxEndblockerDispatchesPerPass,
		AllowedDenoms:                  p.AllowedDenoms,
		MaxGrantLifetimeSeconds:        p.MaxGrantLifetimeSeconds,
	}
}
