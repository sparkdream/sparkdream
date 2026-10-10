package types

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
)

// uspark per SPARK.
var oneSpark = math.NewInt(1_000_000)

func spark(n int64) math.Int { return oneSpark.MulRaw(n) }

// Compiled hard bounds (docs/x-artifact-spec.md §3.13). Neither governance
// nor the Operations Committee can set values outside them; only a chain
// upgrade can change them. Bounds that depend on time scale come from the
// build-tagged params_vals_*.go files.
const (
	MaxRoyaltyBpsCeiling     uint32 = 2500
	MaxSaleFeeBpsCeiling     uint32 = 1000
	MinPendingPerPair        uint32 = 1
	MaxPendingPerPairCeiling uint32 = 50
	MinInboxPerRecipient     uint32 = 10
	MaxInboxPerRecipientCap  uint32 = 1000
	MinExpirationsPerBlock   uint32 = 10
	MaxExpirationsPerBlock   uint32 = 1000
	MinBatchSize             uint32 = 1
	MaxBatchSizeCeiling      uint32 = 100
	MinHidesPerSentinelDay   uint32 = 1
	MaxHidesPerSentinelDay   uint32 = 500
	HardMaxNameLength        uint32 = 256
	HardMaxSymbolLength      uint32 = 32
	HardMaxDescriptionLength uint32 = 8192
	HardMaxURILength         uint32 = 2048
	HardMaxAttributes        uint32 = 64
	HardMaxAttrKeyLength     uint32 = 128
	HardMaxAttrValueLength   uint32 = 1024
	HardMaxMintersPerClass   uint32 = 50
	HardMaxTrustLevel        uint32 = 4 // TRUST_LEVEL_CORE
	// RevokeReasonMaxLength bounds MsgRevoke.reason; HideReasonMaxLength bounds
	// MsgHideContent.reason_text.
	RevokeReasonMaxLength = 256
	HideReasonMaxLength   = 1024
)

var (
	MinClassCreationFee = spark(1)
	MaxClassCreationFee = spark(10_000)
	MinTokenDeposit     = oneSpark.QuoRaw(100) // 0.01 SPARK
	MaxTokenDeposit     = spark(100)
	MinInboxFee         = oneSpark.QuoRaw(1000) // 0.001 SPARK
	MaxInboxFee         = spark(10)
	// MaxSentinelCommitDream bounds the DREAM reserved per hide.
	MaxSentinelCommitDream = spark(100_000)
)

// forbiddenURISchemes may never appear in allowed_uri_schemes.
var forbiddenURISchemes = map[string]bool{
	"data": true, "javascript": true, "file": true, "blob": true, "http": true, "vbscript": true,
}

// NewParams creates a new Params instance.
func NewParams() Params {
	return DefaultParams()
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return Params{
		MinTrustLevelCreateClass: 1, // TRUST_LEVEL_PROVISIONAL
		MaxClassesPerCreator:     50,
		DefaultReceivePolicy:     ReceivePolicy_RECEIVE_POLICY_MEMBERS,

		MaxNameLength:           128,
		MaxSymbolLength:         16,
		MaxDescriptionLength:    2048,
		MaxUriLength:            512,
		MaxAttributes:           16,
		MaxAttributeKeyLength:   64,
		MaxAttributeValueLength: 256,
		MaxMintersPerClass:      10,
		MaxBatchSize:            50,
		AllowedUriSchemes:       []string{"ipfs", "ar", "https"},

		MaxRoyaltyBps: 1000,
		SaleFeeBps:    100,

		ClassCreationFee:       spark(10),
		TokenDeposit:           oneSpark.QuoRaw(10), // 0.1 SPARK
		InboxFee:               oneSpark.QuoRaw(50), // 0.02 SPARK
		PendingTtl:             defaultPendingTTL(),
		MaxListingDuration:     defaultMaxListingDuration(),
		MaxPendingPerPair:      5,
		MaxInboxPerRecipient:   100,
		MaxExpirationsPerBlock: 200,
		MarketEnabled:          true,
		PublicMintEnabled:      true,

		MaxHidesPerSentinelPerDay:  50,
		SentinelUnhideWindowBlocks: defaultUnhideWindowBlocks(),
		HideExpiryBlocks:           defaultHideExpiryBlocks(),
		SentinelHideCommitDream:    spark(100),
	}
}

// OperationalParams extracts the operational subset.
func (p Params) OperationalParams() ArtifactOperationalParams {
	return ArtifactOperationalParams{
		ClassCreationFee:           p.ClassCreationFee,
		TokenDeposit:               p.TokenDeposit,
		InboxFee:                   p.InboxFee,
		PendingTtl:                 p.PendingTtl,
		MaxListingDuration:         p.MaxListingDuration,
		MaxPendingPerPair:          p.MaxPendingPerPair,
		MaxInboxPerRecipient:       p.MaxInboxPerRecipient,
		MaxExpirationsPerBlock:     p.MaxExpirationsPerBlock,
		MarketEnabled:              p.MarketEnabled,
		PublicMintEnabled:          p.PublicMintEnabled,
		MaxHidesPerSentinelPerDay:  p.MaxHidesPerSentinelPerDay,
		SentinelUnhideWindowBlocks: p.SentinelUnhideWindowBlocks,
		HideExpiryBlocks:           p.HideExpiryBlocks,
		SentinelHideCommitDream:    p.SentinelHideCommitDream,
	}
}

// ApplyOperationalParams returns a copy of p with the operational subset
// replaced by op.
func (p Params) ApplyOperationalParams(op ArtifactOperationalParams) Params {
	p.ClassCreationFee = op.ClassCreationFee
	p.TokenDeposit = op.TokenDeposit
	p.InboxFee = op.InboxFee
	p.PendingTtl = op.PendingTtl
	p.MaxListingDuration = op.MaxListingDuration
	p.MaxPendingPerPair = op.MaxPendingPerPair
	p.MaxInboxPerRecipient = op.MaxInboxPerRecipient
	p.MaxExpirationsPerBlock = op.MaxExpirationsPerBlock
	p.MarketEnabled = op.MarketEnabled
	p.PublicMintEnabled = op.PublicMintEnabled
	p.MaxHidesPerSentinelPerDay = op.MaxHidesPerSentinelPerDay
	p.SentinelUnhideWindowBlocks = op.SentinelUnhideWindowBlocks
	p.HideExpiryBlocks = op.HideExpiryBlocks
	p.SentinelHideCommitDream = op.SentinelHideCommitDream
	return p
}

func checkU32(name string, v, lo, hi uint32) error {
	if v < lo || v > hi {
		return fmt.Errorf("%s must be in [%d, %d], got %d", name, lo, hi, v)
	}
	return nil
}

func checkI64(name string, v, lo, hi int64) error {
	if v < lo || v > hi {
		return fmt.Errorf("%s must be in [%d, %d], got %d", name, lo, hi, v)
	}
	return nil
}

func checkInt(name string, v, lo, hi math.Int) error {
	if v.IsNil() {
		return fmt.Errorf("%s must be set", name)
	}
	if v.LT(lo) || v.GT(hi) {
		return fmt.Errorf("%s must be in [%s, %s], got %s", name, lo, hi, v)
	}
	return nil
}

// Validate validates the operational subset against the hard bounds.
func (op ArtifactOperationalParams) Validate() error {
	checks := []error{
		checkInt("class_creation_fee", op.ClassCreationFee, MinClassCreationFee, MaxClassCreationFee),
		checkInt("token_deposit", op.TokenDeposit, MinTokenDeposit, MaxTokenDeposit),
		checkInt("inbox_fee", op.InboxFee, MinInboxFee, MaxInboxFee),
		checkI64("pending_ttl", op.PendingTtl, MinPendingTTL, MaxPendingTTL),
		checkI64("max_listing_duration", op.MaxListingDuration, MinMaxListingDuration, MaxMaxListingDuration),
		checkU32("max_pending_per_pair", op.MaxPendingPerPair, MinPendingPerPair, MaxPendingPerPairCeiling),
		checkU32("max_inbox_per_recipient", op.MaxInboxPerRecipient, MinInboxPerRecipient, MaxInboxPerRecipientCap),
		checkU32("max_expirations_per_block", op.MaxExpirationsPerBlock, MinExpirationsPerBlock, MaxExpirationsPerBlock),
		checkU32("max_hides_per_sentinel_per_day", op.MaxHidesPerSentinelPerDay, MinHidesPerSentinelDay, MaxHidesPerSentinelDay),
		checkI64("sentinel_unhide_window_blocks", op.SentinelUnhideWindowBlocks, 1, MaxModerationBlocks),
		checkI64("hide_expiry_blocks", op.HideExpiryBlocks, MinModerationBlocks, MaxModerationBlocks),
		checkInt("sentinel_hide_commit_dream", op.SentinelHideCommitDream, math.ZeroInt(), MaxSentinelCommitDream),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if err := p.OperationalParams().Validate(); err != nil {
		return err
	}
	if p.MinTrustLevelCreateClass > HardMaxTrustLevel {
		return fmt.Errorf("min_trust_level_create_class must be <= %d", HardMaxTrustLevel)
	}
	if p.MaxClassesPerCreator == 0 {
		return fmt.Errorf("max_classes_per_creator must be positive")
	}
	if p.DefaultReceivePolicy == ReceivePolicy_RECEIVE_POLICY_UNSPECIFIED {
		return fmt.Errorf("default_receive_policy must not be UNSPECIFIED")
	}
	if _, ok := ReceivePolicy_name[int32(p.DefaultReceivePolicy)]; !ok {
		return fmt.Errorf("unknown default_receive_policy %d", p.DefaultReceivePolicy)
	}
	limits := []error{
		checkU32("max_name_length", p.MaxNameLength, 1, HardMaxNameLength),
		checkU32("max_symbol_length", p.MaxSymbolLength, 1, HardMaxSymbolLength),
		checkU32("max_description_length", p.MaxDescriptionLength, 1, HardMaxDescriptionLength),
		checkU32("max_uri_length", p.MaxUriLength, 16, HardMaxURILength),
		checkU32("max_attributes", p.MaxAttributes, 0, HardMaxAttributes),
		checkU32("max_attribute_key_length", p.MaxAttributeKeyLength, 1, HardMaxAttrKeyLength),
		checkU32("max_attribute_value_length", p.MaxAttributeValueLength, 1, HardMaxAttrValueLength),
		checkU32("max_minters_per_class", p.MaxMintersPerClass, 0, HardMaxMintersPerClass),
		checkU32("max_batch_size", p.MaxBatchSize, MinBatchSize, MaxBatchSizeCeiling),
		checkU32("max_royalty_bps", p.MaxRoyaltyBps, 0, MaxRoyaltyBpsCeiling),
		checkU32("sale_fee_bps", p.SaleFeeBps, 0, MaxSaleFeeBpsCeiling),
	}
	for _, err := range limits {
		if err != nil {
			return err
		}
	}
	if len(p.AllowedUriSchemes) == 0 {
		return fmt.Errorf("allowed_uri_schemes must not be empty")
	}
	seen := map[string]bool{}
	for _, s := range p.AllowedUriSchemes {
		if s == "" || s != strings.ToLower(s) || strings.ContainsAny(s, ":/ ") {
			return fmt.Errorf("invalid uri scheme %q (lowercase, no ':' or '/')", s)
		}
		if forbiddenURISchemes[s] {
			return fmt.Errorf("uri scheme %q is forbidden", s)
		}
		if seen[s] {
			return fmt.Errorf("duplicate uri scheme %q", s)
		}
		seen[s] = true
	}
	return nil
}
