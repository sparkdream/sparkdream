package types

import (
	"cosmossdk.io/math"
)

// Default parameter values from spec
var (
	DefaultEpochBlocks             int64  = 17280 // ~1 day at 5s blocks
	DefaultSeasonDurationEpochs    int64  = 150   // ~5 months
	DefaultSeasonTransitionEpochs  int64  = 7     // ~1 week for transition
	DefaultXpVoteCast              uint64 = 5
	DefaultXpProposalCreated       uint64 = 10
	DefaultXpForumReplyReceived    uint64 = 2
	DefaultXpForumMarkedHelpful    uint64 = 5
	DefaultXpInviteeFirstInitiative uint64 = 20
	DefaultXpInviteeEstablished    uint64 = 50
	DefaultMaxVoteXpPerEpoch       uint32 = 10
	DefaultMaxForumXpPerEpoch      uint64 = 50
	DefaultMaxXpPerEpoch           uint64 = 200
	DefaultLevelThresholds                = []uint64{0, 100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500}
	DefaultBaselineReputation             = math.LegacyMustNewDecFromStr("0.5")
	DefaultMinGuildMembers         uint32 = 3
	DefaultMaxGuildMembers         uint32 = 100
	DefaultMaxGuildOfficers        uint32 = 5
	DefaultGuildCreationCost              = math.NewInt(100) // 100 DREAM
	DefaultGuildHopCooldownEpochs  uint64 = 30              // ~1 month
	DefaultMaxGuildsPerSeason      uint32 = 3
	DefaultMinGuildAgeEpochs       uint64 = 7 // ~1 week
	DefaultMaxPendingInvites       uint32 = 20
	DefaultDisplayNameMinLength    uint32 = 1
	DefaultDisplayNameMaxLength    uint32 = 50
	DefaultDisplayNameChangeCooldown uint64 = 1 // 1 epoch
	DefaultUsernameMinLength       uint32 = 3
	DefaultUsernameMaxLength       uint32 = 20
	DefaultUsernameChangeCooldown  uint64 = 30  // 30 epochs
	DefaultUsernameCostDream              = math.NewInt(10) // 10 DREAM
	DefaultMaxTransitionEpochs     uint64 = 7
	DefaultTransitionBatchSize     uint32 = 100
	DefaultMaxSeasonExtensions     uint32 = 3
	DefaultMaxExtensionEpochs      uint64 = 14  // ~2 weeks
	DefaultGuildDescriptionMaxLength uint32 = 500
	DefaultGuildInviteTtlEpochs    uint64 = 30  // ~1 month
	DefaultMaxQuestObjectives      uint32 = 5
	DefaultMaxQuestXpReward        uint64 = 100
	DefaultMaxActiveQuestsPerMember uint32 = 10
	DefaultMaxObjectiveDescLength  uint32 = 200
	DefaultSnapshotRetentionSeasons uint32 = 10
	DefaultEpochTrackerRetentionEpochs uint32 = 30
	DefaultVoteXpRecordRetentionSeasons uint32 = 2
	DefaultForumCooldownRetentionEpochs uint32 = 30
	DefaultForumXpMinAccountAgeEpochs uint64 = 7
	DefaultForumXpReciprocalCooldown uint64 = 1
	DefaultForumXpSelfReplyCooldown uint64 = 3
	DefaultTransitionGracePeriod   uint32 = 50400 // ~1 week in blocks
	DefaultTransitionMaxRetries    uint32 = 3
	DefaultDisplayNameReportStake         = math.NewInt(50)  // 50 DREAM
	DefaultDisplayNameAppealStake         = math.NewInt(100) // 100 DREAM
	DefaultDisplayNameAppealPeriod uint64 = 100800           // ~7 days in blocks
	DefaultMaxDisplayableTitles    uint32 = 50
	DefaultMaxArchivedTitles       uint32 = 200
	DefaultInviteCleanupInterval   uint32 = 100
	DefaultInviteCleanupBatchSize  uint32 = 50
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{
		EpochBlocks:                       DefaultEpochBlocks,
		SeasonDurationEpochs:              DefaultSeasonDurationEpochs,
		SeasonTransitionEpochs:            DefaultSeasonTransitionEpochs,
		XpVoteCast:                         DefaultXpVoteCast,
		XpProposalCreated:                  DefaultXpProposalCreated,
		XpForumReplyReceived:               DefaultXpForumReplyReceived,
		XpForumMarkedHelpful:               DefaultXpForumMarkedHelpful,
		XpInviteeFirstInitiative:           DefaultXpInviteeFirstInitiative,
		XpInviteeEstablished:               DefaultXpInviteeEstablished,
		MaxVoteXpPerEpoch:                  DefaultMaxVoteXpPerEpoch,
		MaxForumXpPerEpoch:                 DefaultMaxForumXpPerEpoch,
		MaxXpPerEpoch:                      DefaultMaxXpPerEpoch,
		LevelThresholds:                    DefaultLevelThresholds,
		BaselineReputation:                 DefaultBaselineReputation,
		MinGuildMembers:                    DefaultMinGuildMembers,
		MaxGuildMembers:                    DefaultMaxGuildMembers,
		MaxGuildOfficers:                   DefaultMaxGuildOfficers,
		GuildCreationCost:                  DefaultGuildCreationCost,
		GuildHopCooldownEpochs:             DefaultGuildHopCooldownEpochs,
		MaxGuildsPerSeason:                 DefaultMaxGuildsPerSeason,
		MinGuildAgeEpochs:                  DefaultMinGuildAgeEpochs,
		MaxPendingInvites:                  DefaultMaxPendingInvites,
		DisplayNameMinLength:               DefaultDisplayNameMinLength,
		DisplayNameMaxLength:               DefaultDisplayNameMaxLength,
		DisplayNameChangeCooldownEpochs:    DefaultDisplayNameChangeCooldown,
		UsernameMinLength:                  DefaultUsernameMinLength,
		UsernameMaxLength:                  DefaultUsernameMaxLength,
		UsernameChangeCooldownEpochs:       DefaultUsernameChangeCooldown,
		UsernameCostDream:                  DefaultUsernameCostDream,
		MaxTransitionEpochs:                DefaultMaxTransitionEpochs,
		TransitionBatchSize:                DefaultTransitionBatchSize,
		MaxSeasonExtensions:                DefaultMaxSeasonExtensions,
		MaxExtensionEpochs:                 DefaultMaxExtensionEpochs,
		GuildDescriptionMaxLength:          DefaultGuildDescriptionMaxLength,
		GuildInviteTtlEpochs:               DefaultGuildInviteTtlEpochs,
		MaxQuestObjectives:                 DefaultMaxQuestObjectives,
		MaxQuestXpReward:                   DefaultMaxQuestXpReward,
		MaxActiveQuestsPerMember:           DefaultMaxActiveQuestsPerMember,
		MaxObjectiveDescriptionLength:      DefaultMaxObjectiveDescLength,
		SnapshotRetentionSeasons:           DefaultSnapshotRetentionSeasons,
		EpochTrackerRetentionEpochs:        DefaultEpochTrackerRetentionEpochs,
		VoteXpRecordRetentionSeasons:       DefaultVoteXpRecordRetentionSeasons,
		ForumCooldownRetentionEpochs:       DefaultForumCooldownRetentionEpochs,
		ForumXpMinAccountAgeEpochs:         DefaultForumXpMinAccountAgeEpochs,
		ForumXpReciprocalCooldownEpochs:    DefaultForumXpReciprocalCooldown,
		ForumXpSelfReplyCooldownEpochs:     DefaultForumXpSelfReplyCooldown,
		TransitionGracePeriod:              DefaultTransitionGracePeriod,
		TransitionMaxRetries:               DefaultTransitionMaxRetries,
		DisplayNameReportStakeDream:        DefaultDisplayNameReportStake,
		DisplayNameAppealStakeDream:        DefaultDisplayNameAppealStake,
		DisplayNameAppealPeriodBlocks:      DefaultDisplayNameAppealPeriod,
		MaxDisplayableTitles:               DefaultMaxDisplayableTitles,
		MaxArchivedTitles:                  DefaultMaxArchivedTitles,
		InviteCleanupIntervalBlocks:        DefaultInviteCleanupInterval,
		InviteCleanupBatchSize:             DefaultInviteCleanupBatchSize,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams()
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.EpochBlocks <= 0 {
		return ErrInvalidSigner // Using existing error, should create specific validation errors
	}
	if p.SeasonDurationEpochs <= 0 {
		return ErrInvalidSigner
	}
	if p.MinGuildMembers < 1 {
		return ErrInvalidSigner
	}
	if p.MaxGuildMembers < p.MinGuildMembers {
		return ErrInvalidSigner
	}
	if p.DisplayNameMinLength > p.DisplayNameMaxLength {
		return ErrInvalidSigner
	}
	if p.UsernameMinLength > p.UsernameMaxLength {
		return ErrInvalidSigner
	}
	return nil
}
