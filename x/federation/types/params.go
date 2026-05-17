package types

import (
	"fmt"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// federationGenesisParams holds the network-specific parameter values
// that vary across testparams/devnet/testnet/mainnet builds.
// Provided by genesis_vals_*.go files via build tags.
type federationGenesisParams struct {
	MinBridgeStake           sdk.Coin
	BridgeRevocationCooldown time.Duration
	BridgeUnbondingPeriod    time.Duration

	ContentTTL     time.Duration
	AttestationTTL time.Duration

	MaxIdentityLinksPerUser uint32
	UnverifiedLinkTTL       time.Duration
	ChallengeTTL            time.Duration

	VerificationWindow           time.Duration
	ChallengeWindow              time.Duration
	ChallengeFee                 sdk.Coin
	ChallengeJuryDeadline        time.Duration
	VerifierDemotionCooldown     time.Duration
	VerifierUnbondCooldown       time.Duration
	VerifierOverturnBaseCooldown time.Duration
	ChallengeCooldown            time.Duration

	ArbiterResolutionWindow time.Duration
	ArbiterEscalationWindow time.Duration
	EscalationFee           sdk.Coin

	RateLimitWindow  time.Duration
	IBCPacketTimeout time.Duration
}

// Default parameter values — network-independent constants.
// Network-specific values (TTLs, fees, cooldowns) come from
// getFederationGenesisParams() in genesis_vals_*.go files.
var (
	DefaultMaxBridgesPerPeer         = uint64(5)
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
	DefaultMinVerifierTrustLevel        = uint32(2)                // ESTABLISHED
	DefaultMinVerifierBond              = math.NewInt(500_000_000) // 500 DREAM
	DefaultVerifierRecoveryThreshold    = math.NewInt(250_000_000) // 250 DREAM
	DefaultVerifierSlashAmount          = math.NewInt(50_000_000)  // 50 DREAM
	DefaultUpheldToResetOverturns       = uint32(3)
	DefaultMinEpochVerifications        = uint32(3)
	DefaultMinVerifierAccuracy          = math.LegacyNewDecWithPrec(8, 1) // 0.8
	DefaultOperatorRewardShare          = math.LegacyNewDecWithPrec(6, 1) // 0.6
	DefaultVerifierDreamReward          = math.NewInt(5_000_000)          // 5 DREAM
	DefaultMaxVerifierDreamMintPerEpoch = math.NewInt(100_000_000)        // 100 DREAM

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
		MinBridgeStake:            gp.MinBridgeStake,
		MaxBridgesPerPeer:         DefaultMaxBridgesPerPeer,
		BridgeRevocationCooldown:  gp.BridgeRevocationCooldown,
		BridgeUnbondingPeriod:     gp.BridgeUnbondingPeriod,
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
		MinVerifierBond:              DefaultMinVerifierBond,
		VerifierRecoveryThreshold:    DefaultVerifierRecoveryThreshold,
		VerifierSlashAmount:          DefaultVerifierSlashAmount,
		VerificationWindow:           gp.VerificationWindow,
		ChallengeWindow:              gp.ChallengeWindow,
		ChallengeFee:                 gp.ChallengeFee,
		ChallengeJuryDeadline:        gp.ChallengeJuryDeadline,
		VerifierDemotionCooldown:     gp.VerifierDemotionCooldown,
		VerifierUnbondCooldown:       gp.VerifierUnbondCooldown,
		VerifierOverturnBaseCooldown: gp.VerifierOverturnBaseCooldown,
		UpheldToResetOverturns:       DefaultUpheldToResetOverturns,
		MinEpochVerifications:        DefaultMinEpochVerifications,
		MinVerifierAccuracy:          DefaultMinVerifierAccuracy,
		OperatorRewardShare:          DefaultOperatorRewardShare,
		VerifierDreamReward:          DefaultVerifierDreamReward,
		MaxVerifierDreamMintPerEpoch: DefaultMaxVerifierDreamMintPerEpoch,

		// Arbiter
		ArbiterQuorum:           DefaultArbiterQuorum,
		ArbiterResolutionWindow: gp.ArbiterResolutionWindow,
		ArbiterEscalationWindow: gp.ArbiterEscalationWindow,
		EscalationFee:           gp.EscalationFee,
		ChallengeCooldown:       gp.ChallengeCooldown,
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.MinVerifierBond.IsNil() || !p.MinVerifierBond.IsPositive() {
		return fmt.Errorf("min_verifier_bond must be positive")
	}
	// Validation ranges from spec Section 4.13 will be implemented
	// during the param validation phase. For now, accept all other values.
	return nil
}
