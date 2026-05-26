package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
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

	VerificationWindow           time.Duration
	ChallengeWindow              time.Duration
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
	MinVerifierBond              math.Int
	VerifierRecoveryThreshold    math.Int
	VerifierSlashAmount          math.Int
	MinEpochVerifications        uint32
	MinVerifierAccuracy          math.LegacyDec
	VerifierDreamReward          math.Int
	MaxVerifierDreamMintPerEpoch math.Int
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
	DefaultTrustDiscountRate         = math.LegacyNewDecWithPrec(5, 1) // 0.5
	DefaultBridgeInactivityThreshold = uint64(100)
	DefaultIBCPort                   = PortID
	DefaultIBCChannelVersion         = Version
	DefaultMaxPrunePerBlock          = uint64(100)

	// Verification — network-independent (DREAM amounts in udream: 1 DREAM = 1e6 udream)
	DefaultMinVerifierTrustLevel  = uint32(2) // ESTABLISHED
	DefaultUpheldToResetOverturns = uint32(3)
	// MinVerifierBond / VerifierRecoveryThreshold / VerifierSlashAmount /
	// MinEpochVerifications / MinVerifierAccuracy / VerifierDreamReward /
	// MaxVerifierDreamMintPerEpoch are per-network and live in
	// genesis_vals_*.go so testparams can set aggressive values for fast
	// RECOVERY auto-bond + cap-scaling tests.
	DefaultOperatorRewardShare = math.LegacyNewDecWithPrec(6, 1) // 0.6

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
		TrustDiscountRate:         DefaultTrustDiscountRate,
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
		MinEpochVerifications:        gp.MinEpochVerifications,
		MinVerifierAccuracy:          gp.MinVerifierAccuracy,
		OperatorRewardShare:          DefaultOperatorRewardShare,
		VerifierDreamReward:          gp.VerifierDreamReward,
		MaxVerifierDreamMintPerEpoch: gp.MaxVerifierDreamMintPerEpoch,

		// Arbiter
		ArbiterQuorum:           DefaultArbiterQuorum,
		ArbiterResolutionWindow: gp.ArbiterResolutionWindow,
		ArbiterEscalationWindow: gp.ArbiterEscalationWindow,
		EscalationFeeAmount:     gp.EscalationFeeAmount,
		ChallengeCooldown:       gp.ChallengeCooldown,
	}
}

// GetVerifierRewardEpochBlocks returns the Phase 10 reward-distribution
// cadence for the active build (testparams/devnet/testnet/mainnet). Defined
// here as the exported entry point so keeper code can read it without
// importing build-tagged unexported helpers.
func GetVerifierRewardEpochBlocks() uint64 {
	return getVerifierRewardEpochBlocks()
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
func (p Params) Validate() error {
	// --- Required math.Int amounts: non-nil + non-negative ---
	intFields := map[string]math.Int{
		"min_verifier_bond":                p.MinVerifierBond,
		"verifier_recovery_threshold":      p.VerifierRecoveryThreshold,
		"verifier_slash_amount":            p.VerifierSlashAmount,
		"verifier_dream_reward":            p.VerifierDreamReward,
		"max_verifier_dream_mint_per_epoch": p.MaxVerifierDreamMintPerEpoch,
		"challenge_fee_amount":             p.ChallengeFeeAmount,
		"escalation_fee_amount":            p.EscalationFeeAmount,
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
		"trust_discount_rate":   p.TrustDiscountRate,
		"min_verifier_accuracy": p.MinVerifierAccuracy,
		"operator_reward_share": p.OperatorRewardShare,
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
	if p.MinEpochVerifications == 0 {
		return fmt.Errorf("min_epoch_verifications must be > 0")
	}
	if p.UpheldToResetOverturns == 0 {
		return fmt.Errorf("upheld_to_reset_overturns must be > 0")
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
