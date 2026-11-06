package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"

	reptypes "sparkdream/x/rep/types"
)

// federationGenesisParams holds the network-specific parameter values
// that vary across testparams/devnet/testnet/mainnet builds.
// Provided by genesis_vals_*.go files via build tags.
type federationGenesisParams struct {
	// MinBridgeStake, BridgeRevocationCooldown, BridgeUnbondingPeriod
	// were dropped in the federation→service migration (Phase 1). Their
	// values now live on x/service ServiceTypeConfig per service_type.
	// Existing build-tagged values are retained here as comments for
	// reference when seeding the corresponding ServiceTypeConfigs at
	// genesis.

	ContentTTL     time.Duration
	AttestationTTL time.Duration

	MaxIdentityLinksPerUser uint32
	UnverifiedLinkTTL       time.Duration
	ChallengeTTL            time.Duration

	VerificationWindow time.Duration
	ChallengeWindow    time.Duration
	// ChallengeFeeAmount and EscalationFeeAmount are bare amounts in the
	// chain's bond denom (resolved at runtime from x/identity); the keeper
	// wraps them into sdk.Coin at the point of use.
	ChallengeFeeAmount           math.Int
	ChallengeJuryDeadline        time.Duration
	VerifierDemotionCooldown     time.Duration
	VerifierUnbondCooldown       time.Duration
	VerifierOverturnBaseCooldown time.Duration
	ChallengeCooldown            time.Duration

	ArbiterResolutionWindow time.Duration
	ArbiterEscalationWindow time.Duration
	EscalationFeeAmount     math.Int

	RateLimitWindow  time.Duration
	IBCPacketTimeout time.Duration

	// Verifier-bond economics. Moved out of the package-var defaults so
	// testparams can set aggressive values (low min_bond, small slash) to
	// keep RECOVERY auto-bond and cap-scaling tests fast. Devnet/testnet/
	// mainnet retain the spec-default values.
	MinVerifierBond           math.Int
	VerifierRecoveryThreshold math.Int
	VerifierSlashAmount       math.Int

	// OperatorRewardEpochBlocks is per-network so testparams can drive a
	// distribution in seconds rather than days.
	OperatorRewardEpochBlocks uint64
}

// Default parameter values — network-independent constants.
// Network-specific values (TTLs, fees, cooldowns) come from
// getFederationGenesisParams() in genesis_vals_*.go files.
var (
	// DefaultMaxBridgesPerPeer is a *kill-switch* default per Decision 6
	// of the federation→service migration plan: set high enough (1000)
	// that the cap never bites legit use. Bridge participation is gated
	// by service.MinBond + content-hash dedup + per-peer rate limits.
	// Gov may dial this down without a chain upgrade if an unknown-
	// unknown materializes; it is NOT a normal policy lever.
	DefaultMaxBridgesPerPeer         = uint64(1000)
	DefaultKnownContentTypes         = []string{"blog_post", "blog_reply", "forum_thread", "forum_reply", "collection"}
	DefaultMaxInboundPerBlock        = uint64(50)
	DefaultMaxOutboundPerBlock       = uint64(50)
	DefaultMaxContentBodySize        = uint64(4096)
	DefaultMaxContentUriSize         = uint64(2048)
	DefaultMaxProtocolMetadataSize   = uint64(8192)
	DefaultGlobalMaxTrustCredit      = uint32(1)
	DefaultBridgeInactivityThreshold = uint64(100)
	DefaultIBCPort                   = PortID
	DefaultIBCChannelVersion         = Version
	DefaultMaxPrunePerBlock          = uint64(100)

	// Verification — network-independent (DREAM amounts in udream: 1 DREAM = 1e6 udream)
	DefaultMinVerifierTrustLevel  = uint32(2) // ESTABLISHED
	DefaultUpheldToResetOverturns = uint32(3)
	// MinVerifierBond / VerifierRecoveryThreshold / VerifierSlashAmount are
	// per-network and live in genesis_vals_*.go so testparams can set
	// aggressive values for fast RECOVERY auto-bond tests.
	//
	// Verifier PAY (min_epoch_verifications, min_verifier_accuracy, the DREAM
	// stipend and its mint cap, and the SPARK pool) is not here at all: it
	// lives in x/rep params alongside the other bonded roles' reward knobs,
	// because x/rep runs the distribution. See
	// x/rep/keeper/verifier_reward_distribution.go.
	// Bridge operator compensation. The pool cap matches x/rep's bonded-role
	// pools; the inflation share is deliberately an order of magnitude below
	// x/rep's 0.5 because this is ONE role, not four, and because federation
	// is the third module skimming the community pool ahead of x/split.
	DefaultOperatorRewardInflationShare       = math.LegacyNewDecWithPrec(5, 2) // 0.05
	DefaultMaxOperatorRewardPool              = math.NewInt(100_000_000_000)    // 100,000 SPARK
	DefaultOperatorRewardPoolOverflowBurnRate = math.LegacyNewDecWithPrec(5, 1) // 0.5
	DefaultMinEpochVerifiedSubmissions        = uint32(1)
	DefaultMaxUnverifiedRate                  = math.LegacyNewDecWithPrec(5, 1) // 0.5

	// Arbiter — network-independent
	DefaultArbiterQuorum = uint32(3)
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{}
}

// DefaultParams returns a default set of parameters.
// Network-specific values (TTLs, fees, cooldowns) come from build-tagged
// genesis_vals_*.go files via getFederationGenesisParams().
func DefaultParams() Params {
	gp := getFederationGenesisParams()

	return Params{
		// MinBridgeStake / BridgeRevocationCooldown / BridgeUnbondingPeriod
		// were dropped in the federation→service migration (Phase 1).
		// Equivalent knobs now live on x/service ServiceTypeConfig.
		MaxBridgesPerPeer:         DefaultMaxBridgesPerPeer,
		KnownContentTypes:         DefaultKnownContentTypes,
		MaxInboundPerBlock:        DefaultMaxInboundPerBlock,
		MaxOutboundPerBlock:       DefaultMaxOutboundPerBlock,
		MaxContentBodySize:        DefaultMaxContentBodySize,
		MaxContentUriSize:         DefaultMaxContentUriSize,
		MaxProtocolMetadataSize:   DefaultMaxProtocolMetadataSize,
		ContentTtl:                gp.ContentTTL,
		AttestationTtl:            gp.AttestationTTL,
		GlobalMaxTrustCredit:      DefaultGlobalMaxTrustCredit,
		MaxIdentityLinksPerUser:   gp.MaxIdentityLinksPerUser,
		UnverifiedLinkTtl:         gp.UnverifiedLinkTTL,
		ChallengeTtl:              gp.ChallengeTTL,
		BridgeInactivityThreshold: DefaultBridgeInactivityThreshold,
		IbcPort:                   DefaultIBCPort,
		IbcChannelVersion:         DefaultIBCChannelVersion,
		IbcPacketTimeout:          gp.IBCPacketTimeout,
		MaxPrunePerBlock:          DefaultMaxPrunePerBlock,
		RateLimitWindow:           gp.RateLimitWindow,

		// Verification
		MinVerifierTrustLevel:        DefaultMinVerifierTrustLevel,
		MinVerifierBond:              gp.MinVerifierBond,
		VerifierRecoveryThreshold:    gp.VerifierRecoveryThreshold,
		VerifierSlashAmount:          gp.VerifierSlashAmount,
		VerificationWindow:           gp.VerificationWindow,
		ChallengeWindow:              gp.ChallengeWindow,
		ChallengeFeeAmount:           gp.ChallengeFeeAmount,
		ChallengeJuryDeadline:        gp.ChallengeJuryDeadline,
		VerifierDemotionCooldown:     gp.VerifierDemotionCooldown,
		VerifierUnbondCooldown:       gp.VerifierUnbondCooldown,
		VerifierOverturnBaseCooldown: gp.VerifierOverturnBaseCooldown,
		UpheldToResetOverturns:       DefaultUpheldToResetOverturns,

		// Bridge operator compensation
		OperatorRewardInflationShare:        DefaultOperatorRewardInflationShare,
		MaxOperatorRewardPool:               DefaultMaxOperatorRewardPool,
		OperatorRewardPoolOverflowBurnRatio: DefaultOperatorRewardPoolOverflowBurnRate,
		OperatorRewardEpochBlocks:           gp.OperatorRewardEpochBlocks,
		MinEpochVerifiedSubmissions:         DefaultMinEpochVerifiedSubmissions,
		MaxUnverifiedRate:                   DefaultMaxUnverifiedRate,

		// Arbiter
		ArbiterQuorum:           DefaultArbiterQuorum,
		ArbiterResolutionWindow: gp.ArbiterResolutionWindow,
		ArbiterEscalationWindow: gp.ArbiterEscalationWindow,
		EscalationFeeAmount:     gp.EscalationFeeAmount,
		ChallengeCooldown:       gp.ChallengeCooldown,
	}
}

// Validate validates the set of params per the spec Section 4.13
// validation-ranges table. Catches malformed proposals before they
// land on chain and breaks the federation EndBlocker / message
// handlers in non-obvious ways (e.g. min_verifier_bond <=
// verifier_recovery_threshold corrupts bond_status math).
//
// Bounds use the spec defaults as guardrails — gov can land values at
// the edges but not outside them. The relational constraints
// (recovery_threshold < min_bond, slash_amount <= min_bond) are
// non-negotiable: violating them produces ill-defined runtime
// behavior.
// ParamFloors are the build-tagged minimums Params.Validate enforces on top
// of the spec §4.13 ceilings. Durations are lower bounds on the named params;
// fees are whole SPARK; zero means no floor beyond the positivity checks.
type ParamFloors struct {
	RetentionTTL                 time.Duration // content_ttl, attestation_ttl
	ChallengeTTL                 time.Duration
	RateLimitWindow              time.Duration
	VerificationWindow           time.Duration
	ChallengeWindow              time.Duration
	ChallengeJuryDeadline        time.Duration
	VerifierDemotionCooldown     time.Duration
	VerifierOverturnBaseCooldown time.Duration
	ArbiterResolutionWindow      time.Duration
	ArbiterEscalationWindow      time.Duration
	ChallengeCooldown            time.Duration
	ChallengeFeeSpark            int64
	EscalationFeeSpark           int64
	BridgeInactivityThreshold    uint64
}

// SpecParamFloors are the spec §4.13 minimums, enforced on mainnet and
// testnet builds.
var SpecParamFloors = ParamFloors{
	RetentionTTL:                 24 * time.Hour,
	ChallengeTTL:                 24 * time.Hour,
	RateLimitWindow:              time.Hour,
	VerificationWindow:           time.Hour,
	ChallengeWindow:              24 * time.Hour,
	ChallengeJuryDeadline:        3 * 24 * time.Hour,
	VerifierDemotionCooldown:     24 * time.Hour,
	VerifierOverturnBaseCooldown: time.Hour,
	ArbiterResolutionWindow:      time.Hour,
	ArbiterEscalationWindow:      time.Hour,
	ChallengeCooldown:            time.Hour,
	ChallengeFeeSpark:            50,
	EscalationFeeSpark:           25,
	BridgeInactivityThreshold:    10,
}

// DevnetParamFloors keep the devnet's short windows legal: devnet exists to
// exercise the federation flows within hours, so its floors sit at or below
// its genesis values (genesis_vals_devnet.go) while still refusing
// near-zero windows and fees.
var DevnetParamFloors = ParamFloors{
	RetentionTTL:                 time.Hour,
	ChallengeTTL:                 15 * time.Minute,
	RateLimitWindow:              15 * time.Minute,
	VerificationWindow:           15 * time.Minute,
	ChallengeWindow:              time.Hour,
	ChallengeJuryDeadline:        time.Hour,
	VerifierDemotionCooldown:     15 * time.Minute,
	VerifierOverturnBaseCooldown: 5 * time.Minute,
	ArbiterResolutionWindow:      15 * time.Minute,
	ArbiterEscalationWindow:      15 * time.Minute,
	ChallengeCooldown:            5 * time.Minute,
	ChallengeFeeSpark:            10,
	EscalationFeeSpark:           5,
	BridgeInactivityThreshold:    10,
}

// TestParamFloors apply to the untagged test build: no floors beyond
// positivity and a 1 s retention TTL.
var TestParamFloors = ParamFloors{RetentionTTL: time.Second}

func (p Params) Validate() error {
	return p.validateWithBounds(activeParamFloors, reptypes.MaxAnonymousActionWindow)
}

// ValidateWithFloors runs Validate with the given floors instead of the
// build's own (tests check every network's genesis against its floors) and
// without the build's anonymous-action-window ceiling, which is per network
// too (TestZkKeyDelayOutlastsArbiterWindow checks that one per genesis).
func (p Params) ValidateWithFloors(floors ParamFloors) error {
	return p.validateWithBounds(floors, 0)
}

func (p Params) validateWithBounds(floors ParamFloors, anonWindow time.Duration) error {
	// --- Required math.Int amounts: non-nil + non-negative ---
	intFields := map[string]math.Int{
		"min_verifier_bond":           p.MinVerifierBond,
		"verifier_recovery_threshold": p.VerifierRecoveryThreshold,
		"verifier_slash_amount":       p.VerifierSlashAmount,
		"max_operator_reward_pool":    p.MaxOperatorRewardPool,
		"challenge_fee_amount":        p.ChallengeFeeAmount,
		"escalation_fee_amount":       p.EscalationFeeAmount,
	}
	for name, v := range intFields {
		if v.IsNil() {
			return fmt.Errorf("%s must be set (got nil)", name)
		}
		if v.IsNegative() {
			return fmt.Errorf("%s must be non-negative (got %s)", name, v.String())
		}
	}

	// min_verifier_bond must be positive (non-zero); the others can be
	// zero if gov wants to disable that mechanism.
	if !p.MinVerifierBond.IsPositive() {
		return fmt.Errorf("min_verifier_bond must be positive")
	}

	// --- Relational: recovery_threshold < min_bond ---
	// Bond status compute reads min_bond > recovery_threshold;
	// inversion makes RECOVERY band empty and the demotion gate fires
	// on every slash.
	if p.VerifierRecoveryThreshold.GTE(p.MinVerifierBond) {
		return fmt.Errorf(
			"verifier_recovery_threshold (%s) must be less than min_verifier_bond (%s)",
			p.VerifierRecoveryThreshold.String(), p.MinVerifierBond.String())
	}

	// --- Relational: slash_amount <= min_bond ---
	// SlashBond caps the slash at current_bond, so a slash > min_bond
	// just drains the entire bond. The constraint catches likely typos
	// (e.g., extra zero on slash_amount) before they hit prod.
	if p.VerifierSlashAmount.GT(p.MinVerifierBond) {
		return fmt.Errorf(
			"verifier_slash_amount (%s) must not exceed min_verifier_bond (%s)",
			p.VerifierSlashAmount.String(), p.MinVerifierBond.String())
	}

	// --- LegacyDec ranges: [0, 1] (inclusive) ---
	decFields := map[string]math.LegacyDec{
		"operator_reward_inflation_share":          p.OperatorRewardInflationShare,
		"operator_reward_pool_overflow_burn_ratio": p.OperatorRewardPoolOverflowBurnRatio,
		"max_unverified_rate":                      p.MaxUnverifiedRate,
	}
	one := math.LegacyOneDec()
	zero := math.LegacyZeroDec()
	for name, v := range decFields {
		if v.IsNil() {
			return fmt.Errorf("%s must be set (got nil)", name)
		}
		if v.LT(zero) || v.GT(one) {
			return fmt.Errorf("%s must be in [0, 1] (got %s)", name, v.String())
		}
	}

	// --- Duration positivity (zero would disable the mechanism) ---
	durFields := map[string]time.Duration{
		"content_ttl":                     p.ContentTtl,
		"attestation_ttl":                 p.AttestationTtl,
		"unverified_link_ttl":             p.UnverifiedLinkTtl,
		"challenge_ttl":                   p.ChallengeTtl,
		"verification_window":             p.VerificationWindow,
		"challenge_window":                p.ChallengeWindow,
		"challenge_jury_deadline":         p.ChallengeJuryDeadline,
		"verifier_demotion_cooldown":      p.VerifierDemotionCooldown,
		"verifier_overturn_base_cooldown": p.VerifierOverturnBaseCooldown,
		"verifier_unbond_cooldown":        p.VerifierUnbondCooldown,
		"arbiter_resolution_window":       p.ArbiterResolutionWindow,
		"arbiter_escalation_window":       p.ArbiterEscalationWindow,
		"rate_limit_window":               p.RateLimitWindow,
		"ibc_packet_timeout":              p.IbcPacketTimeout,
		"challenge_cooldown":              p.ChallengeCooldown,
	}
	for name, v := range durFields {
		if v <= 0 {
			return fmt.Errorf("%s must be positive (got %s)", name, v)
		}
	}

	// --- Trust level + integer bounds ---
	if p.MinVerifierTrustLevel == 0 || p.MinVerifierTrustLevel > 4 {
		return fmt.Errorf("min_verifier_trust_level must be in [1, 4] (got %d)", p.MinVerifierTrustLevel)
	}
	if p.GlobalMaxTrustCredit > 4 {
		return fmt.Errorf("global_max_trust_credit must be <= 4 (got %d)", p.GlobalMaxTrustCredit)
	}
	if p.ArbiterQuorum < 2 {
		return fmt.Errorf("arbiter_quorum must be >= 2 (got %d)", p.ArbiterQuorum)
	}
	if p.UpheldToResetOverturns == 0 {
		return fmt.Errorf("upheld_to_reset_overturns must be > 0")
	}
	if p.OperatorRewardEpochBlocks == 0 {
		// Zero would divide by zero when deriving the epoch number.
		return fmt.Errorf("operator_reward_epoch_blocks must be > 0")
	}
	if p.MinEpochVerifiedSubmissions == 0 {
		// Zero would pay an operator who got nothing verified all epoch,
		// which is the entire thing this compensation is conditioned on.
		return fmt.Errorf("min_epoch_verified_submissions must be > 0")
	}
	if p.MaxPrunePerBlock == 0 {
		return fmt.Errorf("max_prune_per_block must be > 0")
	}
	if p.MaxInboundPerBlock == 0 {
		return fmt.Errorf("max_inbound_per_block must be > 0")
	}
	if p.MaxOutboundPerBlock == 0 {
		return fmt.Errorf("max_outbound_per_block must be > 0")
	}
	if p.MaxContentBodySize == 0 {
		return fmt.Errorf("max_content_body_size must be > 0")
	}
	if p.MaxContentUriSize == 0 {
		return fmt.Errorf("max_content_uri_size must be > 0")
	}
	// max_protocol_metadata_size = 0 is allowed (disables metadata)

	// --- Spec §4.13 range table (enforced on both MsgUpdateParams and
	// MsgUpdateOperationalParams, which validates the merged params). ---
	type uintRange struct {
		name     string
		v        uint64
		min, max uint64
	}
	for _, r := range []uintRange{
		{"max_inbound_per_block", p.MaxInboundPerBlock, 1, 10_000},
		{"max_outbound_per_block", p.MaxOutboundPerBlock, 1, 10_000},
		{"max_content_body_size", p.MaxContentBodySize, 256, 65_536},
		{"max_content_uri_size", p.MaxContentUriSize, 64, 2048},
		{"max_protocol_metadata_size", p.MaxProtocolMetadataSize, 0, 8192},
		{"max_prune_per_block", p.MaxPrunePerBlock, 10, 1000},
		{"arbiter_quorum", uint64(p.ArbiterQuorum), 2, 10},
	} {
		if r.v < r.min || r.v > r.max {
			return fmt.Errorf("%s must be in [%d, %d] (got %d)", r.name, r.min, r.max, r.v)
		}
	}
	// The retention TTLs: 365-day ceiling, build-tagged floor.
	const maxRetentionTTL = 365 * 24 * time.Hour
	for name, v := range map[string]time.Duration{
		"content_ttl":     p.ContentTtl,
		"attestation_ttl": p.AttestationTtl,
	} {
		if v < floors.RetentionTTL || v > maxRetentionTTL {
			return fmt.Errorf("%s must be in [%s, %s] (got %s)", name, floors.RetentionTTL, maxRetentionTTL, v)
		}
	}

	// Spec §4.13 ranges for the remaining durations and amounts. The
	// ceilings hold on every build; the floors are build-tagged
	// (activeParamFloors): the spec minimums on mainnet and testnet, the
	// devnet's own shorter ones on devnet, none in the test build, whose e2e
	// suites run second-scale windows, 1 SPARK fees and a threshold of 1.
	const day = 24 * time.Hour
	for _, r := range []struct {
		name     string
		v        time.Duration
		min, max time.Duration
	}{
		{"challenge_ttl", p.ChallengeTtl, floors.ChallengeTTL, 30 * day},
		{"rate_limit_window", p.RateLimitWindow, floors.RateLimitWindow, 7 * day},
		{"verification_window", p.VerificationWindow, floors.VerificationWindow, 7 * day},
		{"challenge_window", p.ChallengeWindow, floors.ChallengeWindow, 30 * day},
		{"challenge_jury_deadline", p.ChallengeJuryDeadline, floors.ChallengeJuryDeadline, 30 * day},
		{"verifier_demotion_cooldown", p.VerifierDemotionCooldown, floors.VerifierDemotionCooldown, 30 * day},
		{"verifier_unbond_cooldown", p.VerifierUnbondCooldown, 0, 90 * day},
		{"verifier_overturn_base_cooldown", p.VerifierOverturnBaseCooldown, floors.VerifierOverturnBaseCooldown, 7 * day},
		{"arbiter_resolution_window", p.ArbiterResolutionWindow, floors.ArbiterResolutionWindow, maxArbiterResolutionWindow(7*day, anonWindow)},
		{"arbiter_escalation_window", p.ArbiterEscalationWindow, floors.ArbiterEscalationWindow, 7 * day},
		{"challenge_cooldown", p.ChallengeCooldown, floors.ChallengeCooldown, 30 * day},
	} {
		if r.v < r.min {
			return fmt.Errorf("%s must be at least %s (got %s)", r.name, r.min, r.v)
		}
		if r.v > r.max {
			return fmt.Errorf("%s must be at most %s (got %s)", r.name, r.max, r.v)
		}
	}
	if p.BridgeInactivityThreshold < floors.BridgeInactivityThreshold || p.BridgeInactivityThreshold > 10_000 {
		return fmt.Errorf("bridge_inactivity_threshold must be in [%d, 10000] (got %d)",
			floors.BridgeInactivityThreshold, p.BridgeInactivityThreshold)
	}
	const microUnits = 1_000_000 // micro-DREAM per DREAM, uspark per SPARK
	for _, r := range []struct {
		name     string
		v        math.Int
		min, max int64 // whole DREAM / SPARK; max 0 = no ceiling
	}{
		{"min_verifier_bond", p.MinVerifierBond, 100, 10_000},
		{"verifier_recovery_threshold", p.VerifierRecoveryThreshold, 50, 0},
		{"verifier_slash_amount", p.VerifierSlashAmount, 10, 1000},
		{"challenge_fee_amount", p.ChallengeFeeAmount, floors.ChallengeFeeSpark, 5000},
		{"escalation_fee_amount", p.EscalationFeeAmount, floors.EscalationFeeSpark, 1000},
	} {
		lo := math.NewInt(r.min).MulRaw(microUnits)
		if r.v.LT(lo) {
			return fmt.Errorf("%s must be at least %s (got %s)", r.name, lo, r.v)
		}
		if r.max > 0 {
			if hi := math.NewInt(r.max).MulRaw(microUnits); r.v.GT(hi) {
				return fmt.Errorf("%s must be at most %s (got %s)", r.name, hi, r.v)
			}
		}
	}

	// --- IBC sanity ---
	if p.IbcPort == "" {
		return fmt.Errorf("ibc_port must not be empty")
	}
	if p.IbcChannelVersion == "" {
		return fmt.Errorf("ibc_channel_version must not be empty")
	}

	// Note on cross-field invariants: the two relational checks above
	// (verifier_recovery_threshold < min_verifier_bond, verifier_slash_amount
	// <= min_verifier_bond) are the only ones that hold. Apparently-related
	// timer pairs (arbiter_escalation_window vs challenge_jury_deadline;
	// challenge_window vs arbiter_resolution_window+arbiter_escalation_window;
	// verifier_unbond_cooldown vs verifier_demotion_cooldown) all turn out
	// to be sequential or independent state transitions, not nested
	// constraints — see commit history / spec §4.13 for the rationale.

	return nil
}

// maxArbiterResolutionWindow caps the arbiter vote window at the spec
// ceiling or, when lower, anonWindow (x/rep's MaxAnonymousActionWindow):
// arbiter votes are anonymous "one per content" actions against the live
// member tree, so the window must close before a replaced ZK key can
// activate and vote again. Zero anonWindow means no such bound.
func maxArbiterResolutionWindow(specCeiling, anonWindow time.Duration) time.Duration {
	if anonWindow > 0 && anonWindow < specCeiling {
		return anonWindow
	}
	return specCeiling
}
