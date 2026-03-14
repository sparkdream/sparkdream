package types

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// DefaultMaxTitleLength is the default maximum length for post titles
	DefaultMaxTitleLength uint64 = 200
	// DefaultMaxBodyLength is the default maximum length for post bodies
	DefaultMaxBodyLength uint64 = 10000
	// DefaultFeeDenom is the default fee coin denomination
	DefaultFeeDenom = "uspark"

	// DefaultMaxReplyLength is the default maximum reply body length in bytes
	DefaultMaxReplyLength uint64 = 2000
	// DefaultMaxReplyDepth is the default maximum nesting depth for replies
	DefaultMaxReplyDepth uint32 = 5

	// DefaultMaxPostsPerDay is the default max posts per address per day
	DefaultMaxPostsPerDay uint32 = 10
	// DefaultMaxRepliesPerDay is the default max replies per address per day
	DefaultMaxRepliesPerDay uint32 = 50
	// DefaultMaxReactionsPerDay is the default max reactions per address per day
	DefaultMaxReactionsPerDay uint32 = 100

	// DefaultEphemeralContentTTL is the default TTL in seconds for ephemeral content (7 days)
	DefaultEphemeralContentTTL int64 = 604800
	// DefaultPinMinTrustLevel is the default minimum trust level to pin ephemeral content (ESTABLISHED)
	DefaultPinMinTrustLevel uint32 = 2
	// DefaultMaxPinsPerDay is the default max pins per address per day
	DefaultMaxPinsPerDay uint32 = 20

	// DefaultMinEphemeralContentTTL is the governance-only floor for ephemeral_content_ttl (1 day)
	DefaultMinEphemeralContentTTL int64 = 86400

	// DefaultConvictionRenewalPeriod is the default conviction renewal period (7 days)
	DefaultConvictionRenewalPeriod int64 = 604800
)

var (
	// DefaultCostPerByteAmount is the default per-byte storage cost (100 uspark/byte)
	DefaultCostPerByteAmount = math.NewInt(100)
	// DefaultReactionFeeAmount is the default flat fee per reaction (50 uspark)
	DefaultReactionFeeAmount = math.NewInt(50)
	// DefaultMaxCostPerByteAmount is the governance-only ceiling for cost_per_byte (1000 uspark)
	DefaultMaxCostPerByteAmount = math.NewInt(1000)
	// DefaultMaxReactionFeeAmount is the governance-only ceiling for reaction_fee (500 uspark)
	DefaultMaxReactionFeeAmount = math.NewInt(500)
	// DefaultConvictionRenewalThreshold is the default min conviction score to renew anonymous content (100.0)
	DefaultConvictionRenewalThreshold = math.LegacyNewDec(100)
)

// NewParams creates a new Params instance.
func NewParams(maxTitleLength, maxBodyLength uint64) Params {
	return Params{
		MaxTitleLength:             maxTitleLength,
		MaxBodyLength:              maxBodyLength,
		CostPerByte:                sdk.NewCoin(DefaultFeeDenom, DefaultCostPerByteAmount),
		CostPerByteExempt:          false,
		MaxReplyLength:             DefaultMaxReplyLength,
		MaxReplyDepth:              DefaultMaxReplyDepth,
		ReactionFee:                sdk.NewCoin(DefaultFeeDenom, DefaultReactionFeeAmount),
		ReactionFeeExempt:          false,
		MaxPostsPerDay:             DefaultMaxPostsPerDay,
		MaxRepliesPerDay:           DefaultMaxRepliesPerDay,
		MaxReactionsPerDay:         DefaultMaxReactionsPerDay,
		EphemeralContentTtl:        DefaultEphemeralContentTTL,
		PinMinTrustLevel:           DefaultPinMinTrustLevel,
		MaxPinsPerDay:              DefaultMaxPinsPerDay,
		MinEphemeralContentTtl:     DefaultMinEphemeralContentTTL,
		MaxCostPerByte:             sdk.NewCoin(DefaultFeeDenom, DefaultMaxCostPerByteAmount),
		MaxReactionFee:             sdk.NewCoin(DefaultFeeDenom, DefaultMaxReactionFeeAmount),
		ConvictionRenewalThreshold: DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:    DefaultConvictionRenewalPeriod,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(DefaultMaxTitleLength, DefaultMaxBodyLength)
}

// DefaultBlogOperationalParams returns BlogOperationalParams with defaults
// matching the full Params defaults for all operational fields.
func DefaultBlogOperationalParams() BlogOperationalParams {
	return BlogOperationalParams{
		CostPerByte:                sdk.NewCoin(DefaultFeeDenom, DefaultCostPerByteAmount),
		CostPerByteExempt:          false,
		ReactionFee:                sdk.NewCoin(DefaultFeeDenom, DefaultReactionFeeAmount),
		ReactionFeeExempt:          false,
		MaxPostsPerDay:             DefaultMaxPostsPerDay,
		MaxRepliesPerDay:           DefaultMaxRepliesPerDay,
		MaxReactionsPerDay:         DefaultMaxReactionsPerDay,
		EphemeralContentTtl:        DefaultEphemeralContentTTL,
		MaxPinsPerDay:              DefaultMaxPinsPerDay,
		ConvictionRenewalThreshold: DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:    DefaultConvictionRenewalPeriod,
	}
}

// Validate validates the operational params.
func (op BlogOperationalParams) Validate() error {
	if !op.CostPerByte.Amount.IsNil() && op.CostPerByte.IsNegative() {
		return fmt.Errorf("cost_per_byte cannot be negative: %s", op.CostPerByte)
	}
	if !op.ReactionFee.Amount.IsNil() && op.ReactionFee.IsNegative() {
		return fmt.Errorf("reaction_fee cannot be negative: %s", op.ReactionFee)
	}
	if op.MaxPostsPerDay == 0 {
		return fmt.Errorf("max_posts_per_day must be positive, got %d", op.MaxPostsPerDay)
	}
	if op.MaxRepliesPerDay == 0 {
		return fmt.Errorf("max_replies_per_day must be positive, got %d", op.MaxRepliesPerDay)
	}
	if op.MaxReactionsPerDay == 0 {
		return fmt.Errorf("max_reactions_per_day must be positive, got %d", op.MaxReactionsPerDay)
	}
	if op.MaxPinsPerDay == 0 {
		return fmt.Errorf("max_pins_per_day must be positive, got %d", op.MaxPinsPerDay)
	}
	if op.EphemeralContentTtl < 0 {
		return fmt.Errorf("ephemeral_content_ttl must be >= 0, got %d", op.EphemeralContentTtl)
	}

	if op.ConvictionRenewalThreshold.IsNegative() {
		return fmt.Errorf("conviction_renewal_threshold must be >= 0, got %s", op.ConvictionRenewalThreshold)
	}
	if op.ConvictionRenewalPeriod < 0 {
		return fmt.Errorf("conviction_renewal_period must be >= 0, got %d", op.ConvictionRenewalPeriod)
	}

	return nil
}

// ApplyOperationalParams copies all operational fields from op into p,
// preserving governance-only fields (MaxTitleLength, MaxBodyLength,
// MinEphemeralContentTtl, MaxCostPerByte, MaxReactionFee,
// MaxReplyLength, MaxReplyDepth, PinMinTrustLevel).
func (p Params) ApplyOperationalParams(op BlogOperationalParams) Params {
	p.CostPerByte = op.CostPerByte
	p.CostPerByteExempt = op.CostPerByteExempt
	p.ReactionFee = op.ReactionFee
	p.ReactionFeeExempt = op.ReactionFeeExempt
	p.MaxPostsPerDay = op.MaxPostsPerDay
	p.MaxRepliesPerDay = op.MaxRepliesPerDay
	p.MaxReactionsPerDay = op.MaxReactionsPerDay
	p.EphemeralContentTtl = op.EphemeralContentTtl
	p.MaxPinsPerDay = op.MaxPinsPerDay
	p.ConvictionRenewalThreshold = op.ConvictionRenewalThreshold
	p.ConvictionRenewalPeriod = op.ConvictionRenewalPeriod
	return p
}

// ExtractOperationalParams extracts the operational fields from the full params.
func (p Params) ExtractOperationalParams() BlogOperationalParams {
	return BlogOperationalParams{
		CostPerByte:                p.CostPerByte,
		CostPerByteExempt:          p.CostPerByteExempt,
		ReactionFee:                p.ReactionFee,
		ReactionFeeExempt:          p.ReactionFeeExempt,
		MaxPostsPerDay:             p.MaxPostsPerDay,
		MaxRepliesPerDay:           p.MaxRepliesPerDay,
		MaxReactionsPerDay:         p.MaxReactionsPerDay,
		EphemeralContentTtl:        p.EphemeralContentTtl,
		MaxPinsPerDay:              p.MaxPinsPerDay,
		ConvictionRenewalThreshold: p.ConvictionRenewalThreshold,
		ConvictionRenewalPeriod:    p.ConvictionRenewalPeriod,
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.MaxTitleLength == 0 {
		return fmt.Errorf("max title length must be positive, got %d", p.MaxTitleLength)
	}

	if p.MaxBodyLength == 0 {
		return fmt.Errorf("max body length must be positive, got %d", p.MaxBodyLength)
	}

	// Sanity check: title should be shorter than body
	if p.MaxTitleLength > p.MaxBodyLength {
		return fmt.Errorf("max title length (%d) cannot exceed max body length (%d)",
			p.MaxTitleLength, p.MaxBodyLength)
	}

	if !p.CostPerByte.Amount.IsNil() && p.CostPerByte.IsNegative() {
		return fmt.Errorf("cost_per_byte cannot be negative: %s", p.CostPerByte)
	}

	if p.MaxReplyLength == 0 {
		return fmt.Errorf("max_reply_length must be positive, got %d", p.MaxReplyLength)
	}

	if p.MaxReplyDepth == 0 || p.MaxReplyDepth > 20 {
		return fmt.Errorf("max_reply_depth must be between 1 and 20, got %d", p.MaxReplyDepth)
	}

	if !p.ReactionFee.Amount.IsNil() && p.ReactionFee.IsNegative() {
		return fmt.Errorf("reaction_fee cannot be negative: %s", p.ReactionFee)
	}

	if p.MaxPostsPerDay == 0 {
		return fmt.Errorf("max_posts_per_day must be positive, got %d", p.MaxPostsPerDay)
	}

	if p.MaxRepliesPerDay == 0 {
		return fmt.Errorf("max_replies_per_day must be positive, got %d", p.MaxRepliesPerDay)
	}

	if p.MaxReactionsPerDay == 0 {
		return fmt.Errorf("max_reactions_per_day must be positive, got %d", p.MaxReactionsPerDay)
	}

	if p.MaxPinsPerDay == 0 {
		return fmt.Errorf("max_pins_per_day must be positive, got %d", p.MaxPinsPerDay)
	}

	if p.PinMinTrustLevel > 4 {
		return fmt.Errorf("pin_min_trust_level must be 0-4, got %d", p.PinMinTrustLevel)
	}

	if p.EphemeralContentTtl < 0 {
		return fmt.Errorf("ephemeral_content_ttl must be >= 0, got %d", p.EphemeralContentTtl)
	}

	if p.MinEphemeralContentTtl <= 0 {
		return fmt.Errorf("min_ephemeral_content_ttl must be > 0, got %d", p.MinEphemeralContentTtl)
	}

	// Cross-field: if ephemeral_content_ttl > 0, it must be >= min_ephemeral_content_ttl
	if p.EphemeralContentTtl > 0 && p.MinEphemeralContentTtl > 0 && p.EphemeralContentTtl < p.MinEphemeralContentTtl {
		return fmt.Errorf("ephemeral_content_ttl (%d) must be >= min_ephemeral_content_ttl (%d)",
			p.EphemeralContentTtl, p.MinEphemeralContentTtl)
	}

	// Cross-field: cost_per_byte must not exceed max_cost_per_byte (if both non-zero)
	if !p.CostPerByte.Amount.IsNil() && !p.MaxCostPerByte.Amount.IsNil() &&
		!p.CostPerByte.Amount.IsZero() && !p.MaxCostPerByte.Amount.IsZero() &&
		p.CostPerByte.Amount.GT(p.MaxCostPerByte.Amount) {
		return fmt.Errorf("cost_per_byte (%s) must not exceed max_cost_per_byte (%s)",
			p.CostPerByte, p.MaxCostPerByte)
	}

	// Cross-field: reaction_fee must not exceed max_reaction_fee (if both non-zero)
	if !p.ReactionFee.Amount.IsNil() && !p.MaxReactionFee.Amount.IsNil() &&
		!p.ReactionFee.Amount.IsZero() && !p.MaxReactionFee.Amount.IsZero() &&
		p.ReactionFee.Amount.GT(p.MaxReactionFee.Amount) {
		return fmt.Errorf("reaction_fee (%s) must not exceed max_reaction_fee (%s)",
			p.ReactionFee, p.MaxReactionFee)
	}

	if p.MaxCostPerByte.Amount.IsNil() || !p.MaxCostPerByte.IsPositive() {
		return fmt.Errorf("max_cost_per_byte must be positive, got %s", p.MaxCostPerByte)
	}

	if p.MaxReactionFee.Amount.IsNil() || !p.MaxReactionFee.IsPositive() {
		return fmt.Errorf("max_reaction_fee must be positive, got %s", p.MaxReactionFee)
	}

	// Conviction renewal validation
	if p.ConvictionRenewalThreshold.IsNegative() {
		return fmt.Errorf("conviction_renewal_threshold must be >= 0, got %s", p.ConvictionRenewalThreshold)
	}
	if p.ConvictionRenewalPeriod < 0 {
		return fmt.Errorf("conviction_renewal_period must be >= 0, got %d", p.ConvictionRenewalPeriod)
	}
	// Cross-field: if conviction_renewal_threshold > 0, period must be > 0
	if p.ConvictionRenewalThreshold.IsPositive() && p.ConvictionRenewalPeriod <= 0 {
		return fmt.Errorf("conviction_renewal_period must be > 0 when conviction_renewal_threshold is positive (threshold=%s, period=%d)",
			p.ConvictionRenewalThreshold, p.ConvictionRenewalPeriod)
	}

	return nil
}
