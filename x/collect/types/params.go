package types

import (
	"fmt"

	"cosmossdk.io/math"
)

var (
	DefaultMaxCollectionsBase            uint32 = 5
	DefaultMaxCollectionsPerTrustLevel   uint32 = 15
	DefaultMaxItemsPerCollection         uint32 = 500
	DefaultMaxTitleLength                uint32 = 128
	DefaultMaxNameLength                 uint32 = 128
	DefaultMaxDescriptionLength          uint32 = 1024
	DefaultMaxTagLength                  uint32 = 32
	DefaultMaxTagsPerCollection          uint32 = 10
	DefaultMaxAttributesPerItem          uint32 = 20
	DefaultMaxAttributeKeyLength         uint32 = 64
	DefaultMaxAttributeValueLength       uint32 = 256
	DefaultMaxReferenceFieldLength       uint32 = 256
	DefaultMaxEncryptedDataSize          uint32 = 4096
	DefaultMaxCollaboratorsPerCollection uint32 = 20
	DefaultMaxBatchSize                  uint32 = 50
	DefaultMaxTTLBlocks                  int64  = 0      // 0 = unlimited
	DefaultMaxNonMemberTTLBlocks         int64  = 432000 // ~30 days
	DefaultMaxPrunePerBlock              uint32 = 100

	DefaultBaseCollectionDeposit = math.NewInt(1000000) // 1 SPARK
	DefaultPerItemDeposit        = math.NewInt(100000)  // 0.1 SPARK
	DefaultPerItemSpamTax        = math.NewInt(500000)  // 0.5 SPARK

	DefaultSponsorFee                        = math.NewInt(1000000) // 1 SPARK
	DefaultMinSponsorTrustLevel              = "TRUST_LEVEL_ESTABLISHED"
	DefaultSponsorshipRequestTTLBlocks int64 = 100800 // ~7 days

	DefaultMinCuratorBond                  = math.NewInt(500) // 500 DREAM
	DefaultMinCuratorTrustLevel            = "TRUST_LEVEL_PROVISIONAL"
	DefaultMinCuratorAgeBlocks      int64  = 14400 // ~1 day
	DefaultMaxTagsPerReview         uint32 = 5
	DefaultMaxReviewCommentLength   uint32 = 512
	DefaultMaxReviewsPerCollection  uint32 = 20
	DefaultCuratorSlashFraction            = math.LegacyNewDecWithPrec(10, 2) // 10%
	DefaultChallengeRewardFraction         = math.LegacyNewDecWithPrec(80, 2) // 80%
	DefaultChallengeWindowBlocks    int64  = 100800                           // ~7 days
	DefaultChallengeDeposit                = math.NewInt(250)                 // 250 DREAM
	DefaultMaxChallengeReasonLength uint32 = 1024

	// Reaction defaults
																				DefaultDownvoteCost              = math.NewInt(25000000) // 25 SPARK
	DefaultMaxUpvotesPerDay   uint32 = 100
	DefaultMaxDownvotesPerDay uint32 = 20

	// Flagging defaults
	DefaultFlagReviewThreshold  uint32 = 5
	DefaultMaxFlagsPerDay       uint32 = 20
	DefaultMaxFlaggersPerTarget uint32 = 50
	DefaultFlagExpirationBlocks int64  = 100800 // ~7 days
	DefaultMaxFlagReasonLength  uint32 = 512

	// Sentinel moderation defaults
	DefaultSentinelCommitAmount       = math.NewInt(100)     // 100 DREAM
	DefaultHideExpiryBlocks     int64 = 100800               // ~7 days
	DefaultAppealFee                  = math.NewInt(5000000) // 5 SPARK
	DefaultAppealCooldownBlocks int64 = 600                  // ~1 hour
	DefaultAppealDeadlineBlocks int64 = 201600               // ~14 days

	// Endorsement defaults
	DefaultEndorsementCreationFee                = math.NewInt(10000000)            // 10 SPARK
	DefaultEndorsementDreamStake                 = math.NewInt(100)                 // 100 DREAM
	DefaultEndorsementStakeDuration        int64 = 432000                           // ~30 days
	DefaultEndorsementExpiryBlocks         int64 = 432000                           // ~30 days
	DefaultEndorsementFeeEndorserShare           = math.LegacyNewDecWithPrec(80, 2) // 80%
	DefaultEndorsementDeletionBurnFraction       = math.LegacyNewDecWithPrec(10, 2) // 10%
)

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return Params{
		MaxCollectionsBase:              DefaultMaxCollectionsBase,
		MaxCollectionsPerTrustLevel:     DefaultMaxCollectionsPerTrustLevel,
		MaxItemsPerCollection:           DefaultMaxItemsPerCollection,
		MaxTitleLength:                  DefaultMaxTitleLength,
		MaxNameLength:                   DefaultMaxNameLength,
		MaxDescriptionLength:            DefaultMaxDescriptionLength,
		MaxTagLength:                    DefaultMaxTagLength,
		MaxTagsPerCollection:            DefaultMaxTagsPerCollection,
		MaxAttributesPerItem:            DefaultMaxAttributesPerItem,
		MaxAttributeKeyLength:           DefaultMaxAttributeKeyLength,
		MaxAttributeValueLength:         DefaultMaxAttributeValueLength,
		MaxReferenceFieldLength:         DefaultMaxReferenceFieldLength,
		MaxEncryptedDataSize:            DefaultMaxEncryptedDataSize,
		MaxCollaboratorsPerCollection:   DefaultMaxCollaboratorsPerCollection,
		MaxBatchSize:                    DefaultMaxBatchSize,
		MaxTtlBlocks:                    DefaultMaxTTLBlocks,
		MaxNonMemberTtlBlocks:           DefaultMaxNonMemberTTLBlocks,
		MaxPrunePerBlock:                DefaultMaxPrunePerBlock,
		BaseCollectionDeposit:           DefaultBaseCollectionDeposit,
		PerItemDeposit:                  DefaultPerItemDeposit,
		PerItemSpamTax:                  DefaultPerItemSpamTax,
		SponsorFee:                      DefaultSponsorFee,
		MinSponsorTrustLevel:            DefaultMinSponsorTrustLevel,
		SponsorshipRequestTtlBlocks:     DefaultSponsorshipRequestTTLBlocks,
		MinCuratorBond:                  DefaultMinCuratorBond,
		MinCuratorTrustLevel:            DefaultMinCuratorTrustLevel,
		MinCuratorAgeBlocks:             DefaultMinCuratorAgeBlocks,
		MaxTagsPerReview:                DefaultMaxTagsPerReview,
		MaxReviewCommentLength:          DefaultMaxReviewCommentLength,
		MaxReviewsPerCollection:         DefaultMaxReviewsPerCollection,
		CuratorSlashFraction:            DefaultCuratorSlashFraction,
		ChallengeRewardFraction:         DefaultChallengeRewardFraction,
		ChallengeWindowBlocks:           DefaultChallengeWindowBlocks,
		ChallengeDeposit:                DefaultChallengeDeposit,
		MaxChallengeReasonLength:        DefaultMaxChallengeReasonLength,
		DownvoteCost:                    DefaultDownvoteCost,
		MaxUpvotesPerDay:                DefaultMaxUpvotesPerDay,
		MaxDownvotesPerDay:              DefaultMaxDownvotesPerDay,
		FlagReviewThreshold:             DefaultFlagReviewThreshold,
		MaxFlagsPerDay:                  DefaultMaxFlagsPerDay,
		MaxFlaggersPerTarget:            DefaultMaxFlaggersPerTarget,
		FlagExpirationBlocks:            DefaultFlagExpirationBlocks,
		MaxFlagReasonLength:             DefaultMaxFlagReasonLength,
		SentinelCommitAmount:            DefaultSentinelCommitAmount,
		HideExpiryBlocks:                DefaultHideExpiryBlocks,
		AppealFee:                       DefaultAppealFee,
		AppealCooldownBlocks:            DefaultAppealCooldownBlocks,
		AppealDeadlineBlocks:            DefaultAppealDeadlineBlocks,
		EndorsementCreationFee:          DefaultEndorsementCreationFee,
		EndorsementDreamStake:           DefaultEndorsementDreamStake,
		EndorsementStakeDuration:        DefaultEndorsementStakeDuration,
		EndorsementExpiryBlocks:         DefaultEndorsementExpiryBlocks,
		EndorsementFeeEndorserShare:     DefaultEndorsementFeeEndorserShare,
		EndorsementDeletionBurnFraction: DefaultEndorsementDeletionBurnFraction,
	}
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.MaxCollectionsBase == 0 {
		return fmt.Errorf("max_collections_base must be positive")
	}
	if p.MaxItemsPerCollection == 0 {
		return fmt.Errorf("max_items_per_collection must be positive")
	}
	if p.MaxTitleLength == 0 {
		return fmt.Errorf("max_title_length must be positive")
	}
	if p.MaxNameLength == 0 {
		return fmt.Errorf("max_name_length must be positive")
	}
	if p.MaxDescriptionLength == 0 {
		return fmt.Errorf("max_description_length must be positive")
	}
	if p.MaxTagLength == 0 {
		return fmt.Errorf("max_tag_length must be positive")
	}
	if p.MaxTagsPerCollection == 0 {
		return fmt.Errorf("max_tags_per_collection must be positive")
	}
	if p.MaxAttributesPerItem == 0 {
		return fmt.Errorf("max_attributes_per_item must be positive")
	}
	if p.MaxAttributeKeyLength == 0 {
		return fmt.Errorf("max_attribute_key_length must be positive")
	}
	if p.MaxAttributeValueLength == 0 {
		return fmt.Errorf("max_attribute_value_length must be positive")
	}
	if p.MaxReferenceFieldLength == 0 {
		return fmt.Errorf("max_reference_field_length must be positive")
	}
	if p.MaxEncryptedDataSize == 0 {
		return fmt.Errorf("max_encrypted_data_size must be positive")
	}
	if p.MaxCollaboratorsPerCollection == 0 {
		return fmt.Errorf("max_collaborators_per_collection must be positive")
	}
	if p.MaxBatchSize == 0 {
		return fmt.Errorf("max_batch_size must be positive")
	}
	if p.MaxTtlBlocks < 0 {
		return fmt.Errorf("max_ttl_blocks must be non-negative: %d", p.MaxTtlBlocks)
	}
	if p.MaxNonMemberTtlBlocks <= 0 {
		return fmt.Errorf("max_non_member_ttl_blocks must be positive: %d", p.MaxNonMemberTtlBlocks)
	}
	if p.MaxPrunePerBlock == 0 {
		return fmt.Errorf("max_prune_per_block must be positive")
	}
	if p.BaseCollectionDeposit.IsNil() || !p.BaseCollectionDeposit.IsPositive() {
		return fmt.Errorf("base_collection_deposit must be positive: %s", p.BaseCollectionDeposit)
	}
	if p.PerItemDeposit.IsNil() || !p.PerItemDeposit.IsPositive() {
		return fmt.Errorf("per_item_deposit must be positive: %s", p.PerItemDeposit)
	}
	if p.PerItemSpamTax.IsNil() || p.PerItemSpamTax.IsNegative() {
		return fmt.Errorf("per_item_spam_tax must be non-negative: %s", p.PerItemSpamTax)
	}
	if p.SponsorFee.IsNil() || !p.SponsorFee.IsPositive() {
		return fmt.Errorf("sponsor_fee must be positive: %s", p.SponsorFee)
	}
	if p.MinSponsorTrustLevel == "" {
		return fmt.Errorf("min_sponsor_trust_level must not be empty")
	}
	if p.SponsorshipRequestTtlBlocks <= 0 {
		return fmt.Errorf("sponsorship_request_ttl_blocks must be positive: %d", p.SponsorshipRequestTtlBlocks)
	}
	if p.MinCuratorBond.IsNil() || !p.MinCuratorBond.IsPositive() {
		return fmt.Errorf("min_curator_bond must be positive: %s", p.MinCuratorBond)
	}
	if p.MinCuratorTrustLevel == "" {
		return fmt.Errorf("min_curator_trust_level must not be empty")
	}
	if p.MinCuratorAgeBlocks < 0 {
		return fmt.Errorf("min_curator_age_blocks must be non-negative: %d", p.MinCuratorAgeBlocks)
	}
	if p.MaxTagsPerReview == 0 {
		return fmt.Errorf("max_tags_per_review must be positive")
	}
	if p.MaxReviewCommentLength == 0 {
		return fmt.Errorf("max_review_comment_length must be positive")
	}
	if p.MaxReviewsPerCollection == 0 {
		return fmt.Errorf("max_reviews_per_collection must be positive")
	}
	if p.CuratorSlashFraction.IsNil() || !p.CuratorSlashFraction.IsPositive() {
		return fmt.Errorf("curator_slash_fraction must be positive: %s", p.CuratorSlashFraction)
	}
	if p.CuratorSlashFraction.GT(math.LegacyOneDec()) {
		return fmt.Errorf("curator_slash_fraction must be <= 1: %s", p.CuratorSlashFraction)
	}
	if p.ChallengeRewardFraction.IsNil() || !p.ChallengeRewardFraction.IsPositive() {
		return fmt.Errorf("challenge_reward_fraction must be positive: %s", p.ChallengeRewardFraction)
	}
	if p.ChallengeRewardFraction.GT(math.LegacyOneDec()) {
		return fmt.Errorf("challenge_reward_fraction must be <= 1: %s", p.ChallengeRewardFraction)
	}
	if p.ChallengeWindowBlocks <= 0 {
		return fmt.Errorf("challenge_window_blocks must be positive: %d", p.ChallengeWindowBlocks)
	}
	if p.ChallengeDeposit.IsNil() || !p.ChallengeDeposit.IsPositive() {
		return fmt.Errorf("challenge_deposit must be positive: %s", p.ChallengeDeposit)
	}
	if p.MaxChallengeReasonLength == 0 {
		return fmt.Errorf("max_challenge_reason_length must be positive")
	}
	if p.DownvoteCost.IsNil() || !p.DownvoteCost.IsPositive() {
		return fmt.Errorf("downvote_cost must be positive: %s", p.DownvoteCost)
	}
	if p.FlagReviewThreshold == 0 {
		return fmt.Errorf("flag_review_threshold must be positive")
	}
	if p.MaxFlagsPerDay == 0 {
		return fmt.Errorf("max_flags_per_day must be positive")
	}
	if p.MaxFlaggersPerTarget == 0 {
		return fmt.Errorf("max_flaggers_per_target must be positive")
	}
	if p.FlagExpirationBlocks <= 0 {
		return fmt.Errorf("flag_expiration_blocks must be positive: %d", p.FlagExpirationBlocks)
	}
	if p.MaxFlagReasonLength == 0 {
		return fmt.Errorf("max_flag_reason_length must be positive")
	}
	if p.SentinelCommitAmount.IsNil() || !p.SentinelCommitAmount.IsPositive() {
		return fmt.Errorf("sentinel_commit_amount must be positive: %s", p.SentinelCommitAmount)
	}
	if p.HideExpiryBlocks <= 0 {
		return fmt.Errorf("hide_expiry_blocks must be positive: %d", p.HideExpiryBlocks)
	}
	if p.AppealFee.IsNil() || !p.AppealFee.IsPositive() {
		return fmt.Errorf("appeal_fee must be positive: %s", p.AppealFee)
	}
	if p.AppealCooldownBlocks <= 0 {
		return fmt.Errorf("appeal_cooldown_blocks must be positive: %d", p.AppealCooldownBlocks)
	}
	if p.AppealDeadlineBlocks <= 0 {
		return fmt.Errorf("appeal_deadline_blocks must be positive: %d", p.AppealDeadlineBlocks)
	}
	if p.EndorsementCreationFee.IsNil() || !p.EndorsementCreationFee.IsPositive() {
		return fmt.Errorf("endorsement_creation_fee must be positive: %s", p.EndorsementCreationFee)
	}
	if p.EndorsementDreamStake.IsNil() || !p.EndorsementDreamStake.IsPositive() {
		return fmt.Errorf("endorsement_dream_stake must be positive: %s", p.EndorsementDreamStake)
	}
	if p.EndorsementStakeDuration <= 0 {
		return fmt.Errorf("endorsement_stake_duration must be positive: %d", p.EndorsementStakeDuration)
	}
	if p.EndorsementExpiryBlocks <= 0 {
		return fmt.Errorf("endorsement_expiry_blocks must be positive: %d", p.EndorsementExpiryBlocks)
	}
	if p.EndorsementFeeEndorserShare.IsNil() || !p.EndorsementFeeEndorserShare.IsPositive() {
		return fmt.Errorf("endorsement_fee_endorser_share must be positive: %s", p.EndorsementFeeEndorserShare)
	}
	if p.EndorsementFeeEndorserShare.GT(math.LegacyOneDec()) {
		return fmt.Errorf("endorsement_fee_endorser_share must be <= 1: %s", p.EndorsementFeeEndorserShare)
	}
	if p.EndorsementDeletionBurnFraction.IsNil() || p.EndorsementDeletionBurnFraction.IsNegative() {
		return fmt.Errorf("endorsement_deletion_burn_fraction must be non-negative: %s", p.EndorsementDeletionBurnFraction)
	}
	if p.EndorsementDeletionBurnFraction.GT(math.LegacyOneDec()) {
		return fmt.Errorf("endorsement_deletion_burn_fraction must be <= 1: %s", p.EndorsementDeletionBurnFraction)
	}
	return nil
}

// Validate validates the CollectOperationalParams subset.
func (op CollectOperationalParams) Validate() error {
	if !op.BaseCollectionDeposit.IsNil() && !op.BaseCollectionDeposit.IsPositive() {
		return fmt.Errorf("base_collection_deposit must be positive: %s", op.BaseCollectionDeposit)
	}
	if !op.PerItemDeposit.IsNil() && !op.PerItemDeposit.IsPositive() {
		return fmt.Errorf("per_item_deposit must be positive: %s", op.PerItemDeposit)
	}
	if !op.PerItemSpamTax.IsNil() && op.PerItemSpamTax.IsNegative() {
		return fmt.Errorf("per_item_spam_tax must be non-negative: %s", op.PerItemSpamTax)
	}
	if !op.SponsorFee.IsNil() && !op.SponsorFee.IsPositive() {
		return fmt.Errorf("sponsor_fee must be positive: %s", op.SponsorFee)
	}
	if !op.MinCuratorBond.IsNil() && !op.MinCuratorBond.IsPositive() {
		return fmt.Errorf("min_curator_bond must be positive: %s", op.MinCuratorBond)
	}
	if !op.ChallengeDeposit.IsNil() && !op.ChallengeDeposit.IsPositive() {
		return fmt.Errorf("challenge_deposit must be positive: %s", op.ChallengeDeposit)
	}
	if !op.CuratorSlashFraction.IsNil() && (op.CuratorSlashFraction.IsNegative() || op.CuratorSlashFraction.GT(math.LegacyOneDec())) {
		return fmt.Errorf("curator_slash_fraction must be in [0, 1]: %s", op.CuratorSlashFraction)
	}
	if !op.ChallengeRewardFraction.IsNil() && (op.ChallengeRewardFraction.IsNegative() || op.ChallengeRewardFraction.GT(math.LegacyOneDec())) {
		return fmt.Errorf("challenge_reward_fraction must be in [0, 1]: %s", op.ChallengeRewardFraction)
	}
	if !op.DownvoteCost.IsNil() && !op.DownvoteCost.IsPositive() {
		return fmt.Errorf("downvote_cost must be positive: %s", op.DownvoteCost)
	}
	if !op.SentinelCommitAmount.IsNil() && !op.SentinelCommitAmount.IsPositive() {
		return fmt.Errorf("sentinel_commit_amount must be positive: %s", op.SentinelCommitAmount)
	}
	if !op.AppealFee.IsNil() && !op.AppealFee.IsPositive() {
		return fmt.Errorf("appeal_fee must be positive: %s", op.AppealFee)
	}
	if !op.EndorsementCreationFee.IsNil() && !op.EndorsementCreationFee.IsPositive() {
		return fmt.Errorf("endorsement_creation_fee must be positive: %s", op.EndorsementCreationFee)
	}
	if !op.EndorsementDreamStake.IsNil() && !op.EndorsementDreamStake.IsPositive() {
		return fmt.Errorf("endorsement_dream_stake must be positive: %s", op.EndorsementDreamStake)
	}
	if !op.EndorsementFeeEndorserShare.IsNil() && (op.EndorsementFeeEndorserShare.IsNegative() || op.EndorsementFeeEndorserShare.GT(math.LegacyOneDec())) {
		return fmt.Errorf("endorsement_fee_endorser_share must be in [0, 1]: %s", op.EndorsementFeeEndorserShare)
	}
	if !op.EndorsementDeletionBurnFraction.IsNil() && (op.EndorsementDeletionBurnFraction.IsNegative() || op.EndorsementDeletionBurnFraction.GT(math.LegacyOneDec())) {
		return fmt.Errorf("endorsement_deletion_burn_fraction must be in [0, 1]: %s", op.EndorsementDeletionBurnFraction)
	}
	return nil
}

// ApplyOperationalParams merges operational fields from op onto the receiver,
// preserving non-operational fields (max lengths, limits, etc.).
func (p Params) ApplyOperationalParams(op CollectOperationalParams) Params {
	if !op.BaseCollectionDeposit.IsNil() {
		p.BaseCollectionDeposit = op.BaseCollectionDeposit
	}
	if !op.PerItemDeposit.IsNil() {
		p.PerItemDeposit = op.PerItemDeposit
	}
	if !op.PerItemSpamTax.IsNil() {
		p.PerItemSpamTax = op.PerItemSpamTax
	}
	if !op.SponsorFee.IsNil() {
		p.SponsorFee = op.SponsorFee
	}
	if op.MinSponsorTrustLevel != "" {
		p.MinSponsorTrustLevel = op.MinSponsorTrustLevel
	}
	if op.SponsorshipRequestTtlBlocks > 0 {
		p.SponsorshipRequestTtlBlocks = op.SponsorshipRequestTtlBlocks
	}
	if !op.MinCuratorBond.IsNil() {
		p.MinCuratorBond = op.MinCuratorBond
	}
	if op.MinCuratorTrustLevel != "" {
		p.MinCuratorTrustLevel = op.MinCuratorTrustLevel
	}
	if op.MinCuratorAgeBlocks > 0 {
		p.MinCuratorAgeBlocks = op.MinCuratorAgeBlocks
	}
	if op.MaxTagsPerReview > 0 {
		p.MaxTagsPerReview = op.MaxTagsPerReview
	}
	if op.MaxReviewCommentLength > 0 {
		p.MaxReviewCommentLength = op.MaxReviewCommentLength
	}
	if op.MaxReviewsPerCollection > 0 {
		p.MaxReviewsPerCollection = op.MaxReviewsPerCollection
	}
	if !op.CuratorSlashFraction.IsNil() {
		p.CuratorSlashFraction = op.CuratorSlashFraction
	}
	if op.ChallengeWindowBlocks > 0 {
		p.ChallengeWindowBlocks = op.ChallengeWindowBlocks
	}
	if !op.ChallengeDeposit.IsNil() {
		p.ChallengeDeposit = op.ChallengeDeposit
	}
	if op.MaxChallengeReasonLength > 0 {
		p.MaxChallengeReasonLength = op.MaxChallengeReasonLength
	}
	if !op.ChallengeRewardFraction.IsNil() {
		p.ChallengeRewardFraction = op.ChallengeRewardFraction
	}
	if !op.DownvoteCost.IsNil() {
		p.DownvoteCost = op.DownvoteCost
	}
	if op.MaxUpvotesPerDay > 0 {
		p.MaxUpvotesPerDay = op.MaxUpvotesPerDay
	}
	if op.MaxDownvotesPerDay > 0 {
		p.MaxDownvotesPerDay = op.MaxDownvotesPerDay
	}
	if op.FlagReviewThreshold > 0 {
		p.FlagReviewThreshold = op.FlagReviewThreshold
	}
	if op.MaxFlagsPerDay > 0 {
		p.MaxFlagsPerDay = op.MaxFlagsPerDay
	}
	if op.MaxFlaggersPerTarget > 0 {
		p.MaxFlaggersPerTarget = op.MaxFlaggersPerTarget
	}
	if op.FlagExpirationBlocks > 0 {
		p.FlagExpirationBlocks = op.FlagExpirationBlocks
	}
	if op.MaxFlagReasonLength > 0 {
		p.MaxFlagReasonLength = op.MaxFlagReasonLength
	}
	if !op.SentinelCommitAmount.IsNil() {
		p.SentinelCommitAmount = op.SentinelCommitAmount
	}
	if op.HideExpiryBlocks > 0 {
		p.HideExpiryBlocks = op.HideExpiryBlocks
	}
	if !op.AppealFee.IsNil() {
		p.AppealFee = op.AppealFee
	}
	if op.AppealCooldownBlocks > 0 {
		p.AppealCooldownBlocks = op.AppealCooldownBlocks
	}
	if op.AppealDeadlineBlocks > 0 {
		p.AppealDeadlineBlocks = op.AppealDeadlineBlocks
	}
	if !op.EndorsementCreationFee.IsNil() {
		p.EndorsementCreationFee = op.EndorsementCreationFee
	}
	if !op.EndorsementDreamStake.IsNil() {
		p.EndorsementDreamStake = op.EndorsementDreamStake
	}
	if op.EndorsementStakeDuration > 0 {
		p.EndorsementStakeDuration = op.EndorsementStakeDuration
	}
	if op.EndorsementExpiryBlocks > 0 {
		p.EndorsementExpiryBlocks = op.EndorsementExpiryBlocks
	}
	if !op.EndorsementFeeEndorserShare.IsNil() {
		p.EndorsementFeeEndorserShare = op.EndorsementFeeEndorserShare
	}
	if !op.EndorsementDeletionBurnFraction.IsNil() {
		p.EndorsementDeletionBurnFraction = op.EndorsementDeletionBurnFraction
	}
	return p
}
