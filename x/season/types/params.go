package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// Default parameter values from spec
var (
	DefaultEpochBlocks                  int64  = 17280 // ~1 day at 5s blocks
	DefaultSeasonDurationEpochs         int64  = 150   // ~5 months
	DefaultSeasonTransitionEpochs       int64  = 7     // ~1 week for transition
	DefaultXpVoteCast                   uint64 = 5
	DefaultXpProposalCreated            uint64 = 10
	DefaultXpForumReplyReceived         uint64 = 2
	DefaultXpForumMarkedHelpful         uint64 = 5
	DefaultXpInviteeFirstInitiative     uint64 = 20
	DefaultXpInviteeEstablished         uint64 = 50
	DefaultMaxVoteXpPerEpoch            uint32 = 10
	DefaultMaxForumXpPerEpoch           uint64 = 50
	DefaultMaxXpPerEpoch                uint64 = 200
	DefaultLevelThresholds                     = []uint64{0, 100, 300, 600, 1000, 1500, 2100, 2800, 3600, 4500}
	DefaultBaselineReputation                  = math.LegacyMustNewDecFromStr("0.5")
	DefaultMinGuildMembers              uint32 = 3
	DefaultMaxGuildMembers              uint32 = 100
	DefaultMaxGuildOfficers             uint32 = 5
	DefaultGuildCreationCost                   = math.NewInt(100) // 100 DREAM
	DefaultGuildHopCooldownEpochs       uint64 = 30               // ~1 month
	DefaultMaxGuildsPerSeason           uint32 = 3
	DefaultMinGuildAgeEpochs            uint64 = 7 // ~1 week
	DefaultMaxPendingInvites            uint32 = 20
	DefaultDisplayNameMinLength         uint32 = 1
	DefaultDisplayNameMaxLength         uint32 = 50
	DefaultDisplayNameChangeCooldown    uint64 = 1 // 1 epoch
	DefaultUsernameMinLength            uint32 = 3
	DefaultUsernameMaxLength            uint32 = 20
	DefaultUsernameChangeCooldown       uint64 = 30              // 30 epochs
	DefaultUsernameCostDream                   = math.NewInt(10) // 10 DREAM
	DefaultMaxTransitionEpochs          uint64 = 7
	DefaultTransitionBatchSize          uint32 = 100
	DefaultMaxSeasonExtensions          uint32 = 3
	DefaultMaxExtensionEpochs           uint64 = 14 // ~2 weeks
	DefaultGuildDescriptionMaxLength    uint32 = 500
	DefaultGuildInviteTtlEpochs         uint64 = 30 // ~1 month
	DefaultMaxQuestObjectives           uint32 = 5
	DefaultMaxQuestXpReward             uint64 = 100
	DefaultMaxActiveQuestsPerMember     uint32 = 10
	DefaultMaxObjectiveDescLength       uint32 = 200
	DefaultSnapshotRetentionSeasons     uint32 = 10
	DefaultEpochTrackerRetentionEpochs  uint32 = 30
	DefaultVoteXpRecordRetentionSeasons uint32 = 2
	DefaultForumCooldownRetentionEpochs uint32 = 30
	DefaultForumXpMinAccountAgeEpochs   uint64 = 7
	DefaultForumXpReciprocalCooldown    uint64 = 1
	DefaultForumXpSelfReplyCooldown     uint64 = 3
	DefaultTransitionGracePeriod        uint32 = 50400 // ~1 week in blocks
	DefaultTransitionMaxRetries         uint32 = 3
	DefaultDisplayNameReportStake              = math.NewInt(50)  // 50 DREAM
	DefaultDisplayNameAppealStake              = math.NewInt(100) // 100 DREAM
	DefaultDisplayNameAppealPeriod      uint64 = 100800           // ~7 days in blocks
	DefaultMaxDisplayableTitles         uint32 = 50
	DefaultMaxArchivedTitles            uint32 = 200
	DefaultInviteCleanupInterval        uint32 = 100
	DefaultInviteCleanupBatchSize       uint32 = 50
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{
		EpochBlocks:                     DefaultEpochBlocks,
		SeasonDurationEpochs:            DefaultSeasonDurationEpochs,
		SeasonTransitionEpochs:          DefaultSeasonTransitionEpochs,
		XpVoteCast:                      DefaultXpVoteCast,
		XpProposalCreated:               DefaultXpProposalCreated,
		XpForumReplyReceived:            DefaultXpForumReplyReceived,
		XpForumMarkedHelpful:            DefaultXpForumMarkedHelpful,
		XpInviteeFirstInitiative:        DefaultXpInviteeFirstInitiative,
		XpInviteeEstablished:            DefaultXpInviteeEstablished,
		MaxVoteXpPerEpoch:               DefaultMaxVoteXpPerEpoch,
		MaxForumXpPerEpoch:              DefaultMaxForumXpPerEpoch,
		MaxXpPerEpoch:                   DefaultMaxXpPerEpoch,
		LevelThresholds:                 DefaultLevelThresholds,
		BaselineReputation:              DefaultBaselineReputation,
		MinGuildMembers:                 DefaultMinGuildMembers,
		MaxGuildMembers:                 DefaultMaxGuildMembers,
		MaxGuildOfficers:                DefaultMaxGuildOfficers,
		GuildCreationCost:               DefaultGuildCreationCost,
		GuildHopCooldownEpochs:          DefaultGuildHopCooldownEpochs,
		MaxGuildsPerSeason:              DefaultMaxGuildsPerSeason,
		MinGuildAgeEpochs:               DefaultMinGuildAgeEpochs,
		MaxPendingInvites:               DefaultMaxPendingInvites,
		DisplayNameMinLength:            DefaultDisplayNameMinLength,
		DisplayNameMaxLength:            DefaultDisplayNameMaxLength,
		DisplayNameChangeCooldownEpochs: DefaultDisplayNameChangeCooldown,
		UsernameMinLength:               DefaultUsernameMinLength,
		UsernameMaxLength:               DefaultUsernameMaxLength,
		UsernameChangeCooldownEpochs:    DefaultUsernameChangeCooldown,
		UsernameCostDream:               DefaultUsernameCostDream,
		MaxTransitionEpochs:             DefaultMaxTransitionEpochs,
		TransitionBatchSize:             DefaultTransitionBatchSize,
		MaxSeasonExtensions:             DefaultMaxSeasonExtensions,
		MaxExtensionEpochs:              DefaultMaxExtensionEpochs,
		GuildDescriptionMaxLength:       DefaultGuildDescriptionMaxLength,
		GuildInviteTtlEpochs:            DefaultGuildInviteTtlEpochs,
		MaxQuestObjectives:              DefaultMaxQuestObjectives,
		MaxQuestXpReward:                DefaultMaxQuestXpReward,
		MaxActiveQuestsPerMember:        DefaultMaxActiveQuestsPerMember,
		MaxObjectiveDescriptionLength:   DefaultMaxObjectiveDescLength,
		SnapshotRetentionSeasons:        DefaultSnapshotRetentionSeasons,
		EpochTrackerRetentionEpochs:     DefaultEpochTrackerRetentionEpochs,
		VoteXpRecordRetentionSeasons:    DefaultVoteXpRecordRetentionSeasons,
		ForumCooldownRetentionEpochs:    DefaultForumCooldownRetentionEpochs,
		ForumXpMinAccountAgeEpochs:      DefaultForumXpMinAccountAgeEpochs,
		ForumXpReciprocalCooldownEpochs: DefaultForumXpReciprocalCooldown,
		ForumXpSelfReplyCooldownEpochs:  DefaultForumXpSelfReplyCooldown,
		TransitionGracePeriod:           DefaultTransitionGracePeriod,
		TransitionMaxRetries:            DefaultTransitionMaxRetries,
		DisplayNameReportStakeDream:     DefaultDisplayNameReportStake,
		DisplayNameAppealStakeDream:     DefaultDisplayNameAppealStake,
		DisplayNameAppealPeriodBlocks:   DefaultDisplayNameAppealPeriod,
		MaxDisplayableTitles:            DefaultMaxDisplayableTitles,
		MaxArchivedTitles:               DefaultMaxArchivedTitles,
		InviteCleanupIntervalBlocks:     DefaultInviteCleanupInterval,
		InviteCleanupBatchSize:          DefaultInviteCleanupBatchSize,
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

// DefaultSeasonOperationalParams returns default operational parameters.
func DefaultSeasonOperationalParams() SeasonOperationalParams {
	return SeasonOperationalParams{
		EpochBlocks:                       DefaultEpochBlocks,
		SeasonDurationEpochs:              DefaultSeasonDurationEpochs,
		SeasonTransitionEpochs:            DefaultSeasonTransitionEpochs,
		XpVoteCast:                        DefaultXpVoteCast,
		XpProposalCreated:                 DefaultXpProposalCreated,
		XpForumReplyReceived:              DefaultXpForumReplyReceived,
		XpForumMarkedHelpful:              DefaultXpForumMarkedHelpful,
		XpInviteeFirstInitiative:          DefaultXpInviteeFirstInitiative,
		XpInviteeEstablished:              DefaultXpInviteeEstablished,
		MaxVoteXpPerEpoch:                 DefaultMaxVoteXpPerEpoch,
		MaxForumXpPerEpoch:                DefaultMaxForumXpPerEpoch,
		MaxXpPerEpoch:                     DefaultMaxXpPerEpoch,
		MinGuildMembers:                   DefaultMinGuildMembers,
		MaxGuildOfficers:                  DefaultMaxGuildOfficers,
		GuildCreationCost:                 DefaultGuildCreationCost,
		GuildHopCooldownEpochs:            DefaultGuildHopCooldownEpochs,
		MaxGuildsPerSeason:                DefaultMaxGuildsPerSeason,
		MinGuildAgeEpochs:                 DefaultMinGuildAgeEpochs,
		MaxPendingInvites:                 DefaultMaxPendingInvites,
		DisplayNameMinLength:              DefaultDisplayNameMinLength,
		DisplayNameMaxLength:              DefaultDisplayNameMaxLength,
		DisplayNameChangeCooldownEpochs:   DefaultDisplayNameChangeCooldown,
		TransitionBatchSize:               DefaultTransitionBatchSize,
		MaxSeasonExtensions:               DefaultMaxSeasonExtensions,
		MaxExtensionEpochs:                DefaultMaxExtensionEpochs,
		GuildDescriptionMaxLength:         DefaultGuildDescriptionMaxLength,
		GuildInviteTtlEpochs:             DefaultGuildInviteTtlEpochs,
		MaxQuestObjectives:                DefaultMaxQuestObjectives,
		ForumXpMinAccountAgeEpochs:        DefaultForumXpMinAccountAgeEpochs,
		ForumXpReciprocalCooldownEpochs:   DefaultForumXpReciprocalCooldown,
		ForumXpSelfReplyCooldownEpochs:    DefaultForumXpSelfReplyCooldown,
		TransitionGracePeriod:             DefaultTransitionGracePeriod,
		MaxQuestXpReward:                  DefaultMaxQuestXpReward,
		UsernameMinLength:                 DefaultUsernameMinLength,
		UsernameMaxLength:                 DefaultUsernameMaxLength,
		UsernameChangeCooldownEpochs:      DefaultUsernameChangeCooldown,
		UsernameCostDream:                 DefaultUsernameCostDream,
		MaxActiveQuestsPerMember:          DefaultMaxActiveQuestsPerMember,
		DisplayNameReportStakeDream:       DefaultDisplayNameReportStake,
		MaxDisplayableTitles:              DefaultMaxDisplayableTitles,
		InviteCleanupIntervalBlocks:       DefaultInviteCleanupInterval,
		InviteCleanupBatchSize:            DefaultInviteCleanupBatchSize,
		MaxObjectiveDescriptionLength:     DefaultMaxObjectiveDescLength,
		DisplayNameAppealStakeDream:       DefaultDisplayNameAppealStake,
		DisplayNameAppealPeriodBlocks:     DefaultDisplayNameAppealPeriod,
		MaxArchivedTitles:                 DefaultMaxArchivedTitles,
	}
}

// Validate validates the operational parameters.
func (op SeasonOperationalParams) Validate() error {
	if op.EpochBlocks <= 0 {
		return fmt.Errorf("epoch_blocks must be positive: %d", op.EpochBlocks)
	}
	if op.SeasonDurationEpochs <= 0 {
		return fmt.Errorf("season_duration_epochs must be positive: %d", op.SeasonDurationEpochs)
	}
	if op.SeasonTransitionEpochs <= 0 {
		return fmt.Errorf("season_transition_epochs must be positive: %d", op.SeasonTransitionEpochs)
	}
	if op.DisplayNameMinLength > op.DisplayNameMaxLength {
		return fmt.Errorf("display_name_min_length (%d) must be <= display_name_max_length (%d)", op.DisplayNameMinLength, op.DisplayNameMaxLength)
	}
	if op.UsernameMinLength > op.UsernameMaxLength {
		return fmt.Errorf("username_min_length (%d) must be <= username_max_length (%d)", op.UsernameMinLength, op.UsernameMaxLength)
	}
	if op.MinGuildMembers < 1 {
		return fmt.Errorf("min_guild_members must be >= 1: %d", op.MinGuildMembers)
	}
	return nil
}

// ApplyOperationalParams copies all operational fields from SeasonOperationalParams
// onto the full Params, preserving governance-only fields (level_thresholds,
// baseline_reputation, max_guild_members, retention settings, max_transition_epochs,
// transition_max_retries).
func (p Params) ApplyOperationalParams(op SeasonOperationalParams) Params {
	p.EpochBlocks = op.EpochBlocks
	p.SeasonDurationEpochs = op.SeasonDurationEpochs
	p.SeasonTransitionEpochs = op.SeasonTransitionEpochs
	p.XpVoteCast = op.XpVoteCast
	p.XpProposalCreated = op.XpProposalCreated
	p.XpForumReplyReceived = op.XpForumReplyReceived
	p.XpForumMarkedHelpful = op.XpForumMarkedHelpful
	p.XpInviteeFirstInitiative = op.XpInviteeFirstInitiative
	p.XpInviteeEstablished = op.XpInviteeEstablished
	p.MaxVoteXpPerEpoch = op.MaxVoteXpPerEpoch
	p.MaxForumXpPerEpoch = op.MaxForumXpPerEpoch
	p.MaxXpPerEpoch = op.MaxXpPerEpoch
	p.MinGuildMembers = op.MinGuildMembers
	p.MaxGuildOfficers = op.MaxGuildOfficers
	p.GuildCreationCost = op.GuildCreationCost
	p.GuildHopCooldownEpochs = op.GuildHopCooldownEpochs
	p.MaxGuildsPerSeason = op.MaxGuildsPerSeason
	p.MinGuildAgeEpochs = op.MinGuildAgeEpochs
	p.MaxPendingInvites = op.MaxPendingInvites
	p.DisplayNameMinLength = op.DisplayNameMinLength
	p.DisplayNameMaxLength = op.DisplayNameMaxLength
	p.DisplayNameChangeCooldownEpochs = op.DisplayNameChangeCooldownEpochs
	p.TransitionBatchSize = op.TransitionBatchSize
	p.MaxSeasonExtensions = op.MaxSeasonExtensions
	p.MaxExtensionEpochs = op.MaxExtensionEpochs
	p.GuildDescriptionMaxLength = op.GuildDescriptionMaxLength
	p.GuildInviteTtlEpochs = op.GuildInviteTtlEpochs
	p.MaxQuestObjectives = op.MaxQuestObjectives
	p.ForumXpMinAccountAgeEpochs = op.ForumXpMinAccountAgeEpochs
	p.ForumXpReciprocalCooldownEpochs = op.ForumXpReciprocalCooldownEpochs
	p.ForumXpSelfReplyCooldownEpochs = op.ForumXpSelfReplyCooldownEpochs
	p.TransitionGracePeriod = op.TransitionGracePeriod
	p.MaxQuestXpReward = op.MaxQuestXpReward
	p.UsernameMinLength = op.UsernameMinLength
	p.UsernameMaxLength = op.UsernameMaxLength
	p.UsernameChangeCooldownEpochs = op.UsernameChangeCooldownEpochs
	p.UsernameCostDream = op.UsernameCostDream
	p.MaxActiveQuestsPerMember = op.MaxActiveQuestsPerMember
	p.DisplayNameReportStakeDream = op.DisplayNameReportStakeDream
	p.MaxDisplayableTitles = op.MaxDisplayableTitles
	p.InviteCleanupIntervalBlocks = op.InviteCleanupIntervalBlocks
	p.InviteCleanupBatchSize = op.InviteCleanupBatchSize
	p.MaxObjectiveDescriptionLength = op.MaxObjectiveDescriptionLength
	p.DisplayNameAppealStakeDream = op.DisplayNameAppealStakeDream
	p.DisplayNameAppealPeriodBlocks = op.DisplayNameAppealPeriodBlocks
	p.MaxArchivedTitles = op.MaxArchivedTitles
	return p
}

// ExtractOperationalParams extracts the operational fields from Params into SeasonOperationalParams.
func (p Params) ExtractOperationalParams() SeasonOperationalParams {
	return SeasonOperationalParams{
		EpochBlocks:                       p.EpochBlocks,
		SeasonDurationEpochs:              p.SeasonDurationEpochs,
		SeasonTransitionEpochs:            p.SeasonTransitionEpochs,
		XpVoteCast:                        p.XpVoteCast,
		XpProposalCreated:                 p.XpProposalCreated,
		XpForumReplyReceived:              p.XpForumReplyReceived,
		XpForumMarkedHelpful:              p.XpForumMarkedHelpful,
		XpInviteeFirstInitiative:          p.XpInviteeFirstInitiative,
		XpInviteeEstablished:              p.XpInviteeEstablished,
		MaxVoteXpPerEpoch:                 p.MaxVoteXpPerEpoch,
		MaxForumXpPerEpoch:                p.MaxForumXpPerEpoch,
		MaxXpPerEpoch:                     p.MaxXpPerEpoch,
		MinGuildMembers:                   p.MinGuildMembers,
		MaxGuildOfficers:                  p.MaxGuildOfficers,
		GuildCreationCost:                 p.GuildCreationCost,
		GuildHopCooldownEpochs:            p.GuildHopCooldownEpochs,
		MaxGuildsPerSeason:                p.MaxGuildsPerSeason,
		MinGuildAgeEpochs:                 p.MinGuildAgeEpochs,
		MaxPendingInvites:                 p.MaxPendingInvites,
		DisplayNameMinLength:              p.DisplayNameMinLength,
		DisplayNameMaxLength:              p.DisplayNameMaxLength,
		DisplayNameChangeCooldownEpochs:   p.DisplayNameChangeCooldownEpochs,
		TransitionBatchSize:               p.TransitionBatchSize,
		MaxSeasonExtensions:               p.MaxSeasonExtensions,
		MaxExtensionEpochs:                p.MaxExtensionEpochs,
		GuildDescriptionMaxLength:         p.GuildDescriptionMaxLength,
		GuildInviteTtlEpochs:             p.GuildInviteTtlEpochs,
		MaxQuestObjectives:                p.MaxQuestObjectives,
		ForumXpMinAccountAgeEpochs:        p.ForumXpMinAccountAgeEpochs,
		ForumXpReciprocalCooldownEpochs:   p.ForumXpReciprocalCooldownEpochs,
		ForumXpSelfReplyCooldownEpochs:    p.ForumXpSelfReplyCooldownEpochs,
		TransitionGracePeriod:             p.TransitionGracePeriod,
		MaxQuestXpReward:                  p.MaxQuestXpReward,
		UsernameMinLength:                 p.UsernameMinLength,
		UsernameMaxLength:                 p.UsernameMaxLength,
		UsernameChangeCooldownEpochs:      p.UsernameChangeCooldownEpochs,
		UsernameCostDream:                 p.UsernameCostDream,
		MaxActiveQuestsPerMember:          p.MaxActiveQuestsPerMember,
		DisplayNameReportStakeDream:       p.DisplayNameReportStakeDream,
		MaxDisplayableTitles:              p.MaxDisplayableTitles,
		InviteCleanupIntervalBlocks:       p.InviteCleanupIntervalBlocks,
		InviteCleanupBatchSize:            p.InviteCleanupBatchSize,
		MaxObjectiveDescriptionLength:     p.MaxObjectiveDescriptionLength,
		DisplayNameAppealStakeDream:       p.DisplayNameAppealStakeDream,
		DisplayNameAppealPeriodBlocks:     p.DisplayNameAppealPeriodBlocks,
		MaxArchivedTitles:                 p.MaxArchivedTitles,
	}
}
