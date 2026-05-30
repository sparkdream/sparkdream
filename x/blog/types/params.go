package types

import (
	"fmt"

	"cosmossdk.io/math"
)

const (
	// DefaultMaxTitleLength is the default maximum length for post titles
	DefaultMaxTitleLength uint64 = 200
	// DefaultMaxBodyLength is the default maximum length for post bodies
	DefaultMaxBodyLength uint64 = 10000

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
	// DefaultPinMinTrustLevel is the default minimum trust level to pin
	// permanent content (ESTABLISHED).
	DefaultPinMinTrustLevel uint32 = 2
	// DefaultMakePermanentMinTrustLevel is the default minimum trust level to
	// promote ephemeral content to permanent (PROVISIONAL). Lower than the pin
	// gate because preservation is a smaller curator action than featuring.
	DefaultMakePermanentMinTrustLevel uint32 = 1
	// DefaultMaxPinsPerDay is the default max pins per address per day
	DefaultMaxPinsPerDay uint32 = 20
	// DefaultMaxMakePermanentPerDay is the default per-address per-day cap on
	// MsgMakePostPermanent + MsgMakeReplyPermanent. Independent of MaxPinsPerDay
	// and MaxPostsPerDay — promotion is a distinct curator action.
	DefaultMaxMakePermanentPerDay uint32 = 10
	// DefaultMaxPromotionsPerBlock is the default per-block cap on the
	// EndBlocker membership-promotion drain. 50 is enough to drain a typical
	// new-member backlog within a few blocks without blowing block gas.
	DefaultMaxPromotionsPerBlock uint32 = 50

	// DefaultMinEphemeralContentTTL is the governance-only floor for ephemeral_content_ttl (1 day)
	DefaultMinEphemeralContentTTL int64 = 86400

	// DefaultConvictionRenewalPeriod is the default conviction renewal period (7 days)
	DefaultConvictionRenewalPeriod int64 = 604800

	// DefaultMaxTagsPerPost is the default maximum number of tags that may be
	// attached to a post. Mirrors x/forum's DefaultMaxTagsPerPost to keep the
	// two content modules in sync.
	DefaultMaxTagsPerPost uint32 = 5
	// DefaultMaxTagLength is the default maximum length in bytes of a single
	// tag name. Mirrors x/forum's DefaultMaxTagLength.
	DefaultMaxTagLength uint32 = 32
)

var (
	// DefaultCostPerByteAmount is the default per-byte storage cost in bond-denom micro-units.
	DefaultCostPerByteAmount = math.NewInt(100)
	// DefaultReactionFeeAmount is the default flat fee per reaction in bond-denom micro-units.
	DefaultReactionFeeAmount = math.NewInt(50)
	// DefaultMaxCostPerByteAmount is the governance-only ceiling for cost_per_byte_amount.
	DefaultMaxCostPerByteAmount = math.NewInt(1000)
	// DefaultMaxReactionFeeAmount is the governance-only ceiling for reaction_fee_amount.
	DefaultMaxReactionFeeAmount = math.NewInt(500)
	// DefaultConvictionRenewalThreshold is the default min conviction score to renew anonymous content (100.0)
	DefaultConvictionRenewalThreshold = math.LegacyNewDec(100)
)

// NewParams creates a new Params instance.
func NewParams(maxTitleLength, maxBodyLength uint64) Params {
	return Params{
		MaxTitleLength:             maxTitleLength,
		MaxBodyLength:              maxBodyLength,
		CostPerByteAmount:          DefaultCostPerByteAmount,
		CostPerByteExempt:          false,
		MaxReplyLength:             DefaultMaxReplyLength,
		MaxReplyDepth:              DefaultMaxReplyDepth,
		ReactionFeeAmount:          DefaultReactionFeeAmount,
		ReactionFeeExempt:          false,
		MaxPostsPerDay:             DefaultMaxPostsPerDay,
		MaxRepliesPerDay:           DefaultMaxRepliesPerDay,
		MaxReactionsPerDay:         DefaultMaxReactionsPerDay,
		EphemeralContentTtl:        DefaultEphemeralContentTTL,
		PinMinTrustLevel:           DefaultPinMinTrustLevel,
		MakePermanentMinTrustLevel: DefaultMakePermanentMinTrustLevel,
		MaxPinsPerDay:              DefaultMaxPinsPerDay,
		MaxMakePermanentPerDay:     DefaultMaxMakePermanentPerDay,
		MaxPromotionsPerBlock:      DefaultMaxPromotionsPerBlock,
		MinEphemeralContentTtl:     DefaultMinEphemeralContentTTL,
		MaxCostPerByteAmount:       DefaultMaxCostPerByteAmount,
		MaxReactionFeeAmount:       DefaultMaxReactionFeeAmount,
		ConvictionRenewalThreshold: DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:    DefaultConvictionRenewalPeriod,
		MaxTagsPerPost:             DefaultMaxTagsPerPost,
		MaxTagLength:               DefaultMaxTagLength,
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
		CostPerByteAmount:          DefaultCostPerByteAmount,
		CostPerByteExempt:          false,
		ReactionFeeAmount:          DefaultReactionFeeAmount,
		ReactionFeeExempt:          false,
		MaxPostsPerDay:             DefaultMaxPostsPerDay,
		MaxRepliesPerDay:           DefaultMaxRepliesPerDay,
		MaxReactionsPerDay:         DefaultMaxReactionsPerDay,
		EphemeralContentTtl:        DefaultEphemeralContentTTL,
		MaxPinsPerDay:              DefaultMaxPinsPerDay,
		MaxMakePermanentPerDay:     DefaultMaxMakePermanentPerDay,
		MaxPromotionsPerBlock:      DefaultMaxPromotionsPerBlock,
		ConvictionRenewalThreshold: DefaultConvictionRenewalThreshold,
		ConvictionRenewalPeriod:    DefaultConvictionRenewalPeriod,
	}
}

// Validate validates the operational params.
func (op BlogOperationalParams) Validate() error {
	if !op.CostPerByteAmount.IsNil() && op.CostPerByteAmount.IsNegative() {
		return fmt.Errorf("cost_per_byte_amount cannot be negative: %s", op.CostPerByteAmount)
	}
	if !op.ReactionFeeAmount.IsNil() && op.ReactionFeeAmount.IsNegative() {
		return fmt.Errorf("reaction_fee_amount cannot be negative: %s", op.ReactionFeeAmount)
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
	if op.MaxMakePermanentPerDay == 0 {
		return fmt.Errorf("max_make_permanent_per_day must be positive, got %d", op.MaxMakePermanentPerDay)
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
// MinEphemeralContentTtl, MaxCostPerByteAmount, MaxReactionFeeAmount,
// MaxReplyLength, MaxReplyDepth, PinMinTrustLevel).
func (p Params) ApplyOperationalParams(op BlogOperationalParams) Params {
	p.CostPerByteAmount = op.CostPerByteAmount
	p.CostPerByteExempt = op.CostPerByteExempt
	p.ReactionFeeAmount = op.ReactionFeeAmount
	p.ReactionFeeExempt = op.ReactionFeeExempt
	p.MaxPostsPerDay = op.MaxPostsPerDay
	p.MaxRepliesPerDay = op.MaxRepliesPerDay
	p.MaxReactionsPerDay = op.MaxReactionsPerDay
	p.EphemeralContentTtl = op.EphemeralContentTtl
	p.MaxPinsPerDay = op.MaxPinsPerDay
	p.MaxMakePermanentPerDay = op.MaxMakePermanentPerDay
	p.MaxPromotionsPerBlock = op.MaxPromotionsPerBlock
	p.ConvictionRenewalThreshold = op.ConvictionRenewalThreshold
	p.ConvictionRenewalPeriod = op.ConvictionRenewalPeriod
	return p
}

// ExtractOperationalParams extracts the operational fields from the full params.
func (p Params) ExtractOperationalParams() BlogOperationalParams {
	return BlogOperationalParams{
		CostPerByteAmount:          p.CostPerByteAmount,
		CostPerByteExempt:          p.CostPerByteExempt,
		ReactionFeeAmount:          p.ReactionFeeAmount,
		ReactionFeeExempt:          p.ReactionFeeExempt,
		MaxPostsPerDay:             p.MaxPostsPerDay,
		MaxRepliesPerDay:           p.MaxRepliesPerDay,
		MaxReactionsPerDay:         p.MaxReactionsPerDay,
		EphemeralContentTtl:        p.EphemeralContentTtl,
		MaxPinsPerDay:              p.MaxPinsPerDay,
		MaxMakePermanentPerDay:     p.MaxMakePermanentPerDay,
		MaxPromotionsPerBlock:      p.MaxPromotionsPerBlock,
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

	if !p.CostPerByteAmount.IsNil() && p.CostPerByteAmount.IsNegative() {
		return fmt.Errorf("cost_per_byte_amount cannot be negative: %s", p.CostPerByteAmount)
	}

	if p.MaxReplyLength == 0 {
		return fmt.Errorf("max_reply_length must be positive, got %d", p.MaxReplyLength)
	}

	if p.MaxReplyDepth == 0 || p.MaxReplyDepth > 20 {
		return fmt.Errorf("max_reply_depth must be between 1 and 20, got %d", p.MaxReplyDepth)
	}

	if !p.ReactionFeeAmount.IsNil() && p.ReactionFeeAmount.IsNegative() {
		return fmt.Errorf("reaction_fee_amount cannot be negative: %s", p.ReactionFeeAmount)
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

	if p.MaxMakePermanentPerDay == 0 {
		return fmt.Errorf("max_make_permanent_per_day must be positive, got %d", p.MaxMakePermanentPerDay)
	}

	if p.PinMinTrustLevel > 4 {
		return fmt.Errorf("pin_min_trust_level must be 0-4, got %d", p.PinMinTrustLevel)
	}

	if p.MakePermanentMinTrustLevel > 4 {
		return fmt.Errorf("make_permanent_min_trust_level must be 0-4, got %d", p.MakePermanentMinTrustLevel)
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

	// Cross-field: cost_per_byte_amount must not exceed max_cost_per_byte_amount (if both non-zero)
	if !p.CostPerByteAmount.IsNil() && !p.MaxCostPerByteAmount.IsNil() &&
		!p.CostPerByteAmount.IsZero() && !p.MaxCostPerByteAmount.IsZero() &&
		p.CostPerByteAmount.GT(p.MaxCostPerByteAmount) {
		return fmt.Errorf("cost_per_byte_amount (%s) must not exceed max_cost_per_byte_amount (%s)",
			p.CostPerByteAmount, p.MaxCostPerByteAmount)
	}

	// Cross-field: reaction_fee_amount must not exceed max_reaction_fee_amount (if both non-zero)
	if !p.ReactionFeeAmount.IsNil() && !p.MaxReactionFeeAmount.IsNil() &&
		!p.ReactionFeeAmount.IsZero() && !p.MaxReactionFeeAmount.IsZero() &&
		p.ReactionFeeAmount.GT(p.MaxReactionFeeAmount) {
		return fmt.Errorf("reaction_fee_amount (%s) must not exceed max_reaction_fee_amount (%s)",
			p.ReactionFeeAmount, p.MaxReactionFeeAmount)
	}

	if p.MaxCostPerByteAmount.IsNil() || !p.MaxCostPerByteAmount.IsPositive() {
		return fmt.Errorf("max_cost_per_byte_amount must be positive, got %s", p.MaxCostPerByteAmount)
	}

	if p.MaxReactionFeeAmount.IsNil() || !p.MaxReactionFeeAmount.IsPositive() {
		return fmt.Errorf("max_reaction_fee_amount must be positive, got %s", p.MaxReactionFeeAmount)
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

	if p.MaxTagsPerPost == 0 {
		return fmt.Errorf("max_tags_per_post must be positive, got %d", p.MaxTagsPerPost)
	}
	if p.MaxTagLength == 0 {
		return fmt.Errorf("max_tag_length must be positive, got %d", p.MaxTagLength)
	}

	return nil
}
