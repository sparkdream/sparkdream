package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "rep"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_rep")

var (
	InvitationKey      = collections.NewPrefix("invitation/value/")
	InvitationCountKey = collections.NewPrefix("invitation/count/")
)

var (
	ProjectKey      = collections.NewPrefix("project/value/")
	ProjectCountKey = collections.NewPrefix("project/count/")
)

var (
	InitiativeKey      = collections.NewPrefix("initiative/value/")
	InitiativeCountKey = collections.NewPrefix("initiative/count/")
)

var (
	StakeKey      = collections.NewPrefix("stake/value/")
	StakeCountKey = collections.NewPrefix("stake/count/")
)

var (
	ChallengeKey      = collections.NewPrefix("challenge/value/")
	ChallengeCountKey = collections.NewPrefix("challenge/count/")
)

var (
	JuryReviewKey      = collections.NewPrefix("juryreview/value/")
	JuryReviewCountKey = collections.NewPrefix("juryreview/count/")
)

var (
	InterimKey      = collections.NewPrefix("interim/value/")
	InterimCountKey = collections.NewPrefix("interim/count/")
)

var (
	// GiftRecordKey: (sender, recipient) -> GiftRecord
	// Tracks last gift timestamp for cooldown enforcement
	GiftRecordKey = collections.NewPrefix("giftrecord/")
)

// Secondary indexes for efficient lookups
var (
	// InitiativesByStatus: status -> []initiativeID
	// Enables O(1) lookup of initiatives by status instead of full table scan
	InitiativesByStatusKey = collections.NewPrefix("initiative/by_status/")

	// InterimsByStatus: status -> []interimID
	// Enables O(1) lookup of interims by status instead of full table scan
	InterimsByStatusKey = collections.NewPrefix("interim/by_status/")

	// JuryReviewsByVerdict: verdict -> []reviewID
	// Enables O(1) lookup of jury reviews by verdict instead of full table scan
	JuryReviewsByVerdictKey = collections.NewPrefix("juryreview/by_verdict/")

	// StakesByTarget: (targetType, targetID) -> []stakeID
	// Enables O(1) lookup of stakes for a specific initiative/project/member
	StakesByTargetKey = collections.NewPrefix("stake/by_target/")

	// ChallengesByStatus: status -> []challengeID
	// Enables O(1) lookup of challenges by status instead of full table scan
	ChallengesByStatusKey = collections.NewPrefix("challenge/by_status/")
)

// Extended staking pool keys
var (
	// MemberStakePoolKey: member address -> MemberStakePool
	MemberStakePoolKey = collections.NewPrefix("stake/member_pool/")

	// TagStakePoolKey: tag name -> TagStakePool
	TagStakePoolKey = collections.NewPrefix("stake/tag_pool/")

	// ProjectStakeInfoKey: project ID -> ProjectStakeInfo
	ProjectStakeInfoKey = collections.NewPrefix("stake/project_info/")
)

// Content initiative links: (initiativeID, (targetType, targetID)) -> exists
// Enables prefix scan by initiativeID to find all linked content for conviction propagation
var ContentInitiativeLinksKey = collections.NewPrefix("content_initiative_links/")

// Seasonal staking reward pool state
var (
	// SeasonalPoolRemainingKey tracks remaining DREAM in this season's reward pool
	SeasonalPoolRemainingKey = collections.NewPrefix("seasonal_pool/remaining")
	// SeasonalPoolAccPerShareKey tracks the MasterChef accumulator for initiative/project stakers
	SeasonalPoolAccPerShareKey = collections.NewPrefix("seasonal_pool/acc_per_share")
	// SeasonalPoolTotalStakedKey tracks total DREAM staked in initiatives + projects
	SeasonalPoolTotalStakedKey = collections.NewPrefix("seasonal_pool/total_staked")
	// SeasonalPoolSeasonKey tracks which season the pool was initialized for
	SeasonalPoolSeasonKey = collections.NewPrefix("seasonal_pool/season")
)

// Treasury and economic tracking
var (
	// TreasuryBalanceKey tracks the x/rep module treasury DREAM balance
	TreasuryBalanceKey = collections.NewPrefix("treasury/balance")
	// SeasonMintedKey tracks total DREAM minted this season (for MintBurnRatio query)
	SeasonMintedKey = collections.NewPrefix("econ/season_minted")
	// SeasonBurnedKey tracks total DREAM burned this season (for MintBurnRatio query)
	SeasonBurnedKey = collections.NewPrefix("econ/season_burned")
	// SeasonInitiativeRewardsMintedKey tracks DREAM minted via initiative completion this season
	SeasonInitiativeRewardsMintedKey = collections.NewPrefix("econ/season_initiative_rewards")
)

// Content challenge keys
var (
	ContentChallengeKey      = collections.NewPrefix("contentchallenge/value/")
	ContentChallengeCountKey = collections.NewPrefix("contentchallenge/count/")

	// ContentChallengesByStatusKey: (status, id) - allows iteration by status
	ContentChallengesByStatusKey = collections.NewPrefix("contentchallenge/by_status/")

	// ContentChallengesByTargetKey: (targetType, targetID) -> challengeID
	// Enforces one active challenge per content item
	ContentChallengesByTargetKey = collections.NewPrefix("contentchallenge/by_target/")
)
