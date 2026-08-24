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
	InvitationKey           = collections.NewPrefix("invitation/value/")
	InvitationCountKey      = collections.NewPrefix("invitation/count/")
	InvitationsByInviteeKey = collections.NewPrefix("invitation/by_invitee/")
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
	JuryReviewKey = collections.NewPrefix("juryreview/value/")
	// InitiativeReview is keyed (initiative_id, round, reviewer) so a rejection
	// can send the work back for another round without colliding with the
	// verdicts already filed on the previous one.
	InitiativeReviewKey = collections.NewPrefix("initiativereview/value/")
	// EscalatedReviews tracks review rounds handed to the committee, so the
	// timeout sweep does not have to rescan every open initiative.
	EscalatedReviewsKey = collections.NewPrefix("escalatedreviews/")
	// RoleRewardDayFundingKey ledgers how much has been skimmed from the
	// community pool on a given UTC day, so the daily cap survives restarts.
	RoleRewardDayFundingKey = collections.NewPrefix("rolerewarddayfunding/")
	// ReviewBountyKey holds escrowed per-initiative review bounties.
	ReviewBountyKey    = collections.NewPrefix("reviewbounty/")
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
	// JuryReviewsByJurorKey indexes seatings by juror address so a juror (or
	// their monitoring client) can find their outstanding summons without
	// scanning every review ever created.
	JuryReviewsByJurorKey = collections.NewPrefix("juryreview/by_juror/")

	// StakesByTarget: (targetType, targetID) -> []stakeID
	// Enables O(1) lookup of stakes for a specific initiative/project/member
	StakesByTargetKey = collections.NewPrefix("stake/by_target/")

	// ChallengesByStatus: status -> []challengeID
	// Enables O(1) lookup of challenges by status instead of full table scan
	ChallengesByStatusKey = collections.NewPrefix("challenge/by_status/")

	// ProjectsByStatus: status -> []projectID
	// Enables O(1) lookup of projects by status — currently consumed by the
	// EndBlocker expiry sweep over PROPOSED projects.
	ProjectsByStatusKey = collections.NewPrefix("project/by_status/")
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

// Conviction refresh scheduling. The EndBlocker recomputes initiative
// conviction from a bounded work queue rather than sweeping every stake of
// every active initiative on every block.
var (
	// ConvictionQueueKey orders pending conviction recomputes by due time:
	// (due_at_unix, initiative_id) -> exists. The drainer in EndBlocker scans
	// only the due prefix and removes what it processes, so the queue is its own
	// cursor across blocks. Drained by Keeper.DrainConvictionQueue.
	ConvictionQueueKey = collections.NewPrefix("conviction/queue/")
	// ConvictionScheduledAtKey maps initiative_id -> its current due time in the
	// queue. It exists purely so a reschedule can find and delete the initiative's
	// existing queue entry; without it, rescheduling would leave duplicates behind.
	ConvictionScheduledAtKey = collections.NewPrefix("conviction/scheduled_at/")
	// InitiativesByContentKey is the reverse of ContentInitiativeLinks:
	// ((target_type, target_id), initiative_id) -> exists. A content stake
	// mutation prefix-scans this to reschedule exactly the initiatives whose
	// propagated conviction it changed, instead of relying on a full sweep to
	// eventually notice.
	InitiativesByContentKey = collections.NewPrefix("conviction/initiatives_by_content/")
)

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
	// SeasonTreasuryInflowKey tracks DREAM credited to the module treasury this season.
	// Drives the TreasuryStatus query's season_inflow field (true inflow, distinct from
	// global SeasonMinted which mixes treasury and non-treasury mints).
	SeasonTreasuryInflowKey = collections.NewPrefix("econ/season_treasury_inflow")
	// SeasonTreasuryOutflowKey tracks DREAM spent from the module treasury this season.
	// Drives the TreasuryStatus query's season_outflow field.
	SeasonTreasuryOutflowKey = collections.NewPrefix("econ/season_treasury_outflow")
	// EpochMintedEpochKey / EpochMintedAmountKey track the global DREAM mint total
	// for the current epoch — the pair enforces max_dream_mint_per_epoch.
	EpochMintedEpochKey  = collections.NewPrefix("econ/epoch_minted_epoch")
	EpochMintedAmountKey = collections.NewPrefix("econ/epoch_minted_amount")
	// DecayLastProcessedEpochKey tracks the most recent epoch for which bulk
	// DREAM decay has been applied to every member (eager pass in EndBlocker).
	// Any lazy ApplyPendingDecay call is a no-op once this pass has run for
	// the current epoch, so same-epoch reads remain consistent.
	DecayLastProcessedEpochKey = collections.NewPrefix("econ/decay_last_processed_epoch")
)

// Bonded-role primitive (generalization of sentinel / curator / verifier).
var (
	// BondedRoleKey: (role_type, address) -> BondedRole. Compound key lets a
	// single address hold multiple roles; prefix iteration on role_type gives
	// the list-all-of-role-type query for free.
	BondedRoleKey = collections.NewPrefix("bondedrole/value/")

	// RoleActivityKey: (role_type, address) -> RoleActivity. Shared
	// accountability record (streaks, cooldown, accuracy ring, per-kind
	// action counters) reported into by the role's surfaces.
	RoleActivityKey = collections.NewPrefix("bondedrole/activity/")

	// BondedRoleConfigKey: role_type -> BondedRoleConfig. Policy snapshot
	// written through by the owning module on operational-params update.
	BondedRoleConfigKey = collections.NewPrefix("bondedrole/config/")
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
