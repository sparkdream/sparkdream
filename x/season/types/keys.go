package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "season"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_season")

var (
	SeasonKey = collections.NewPrefix("season/value/")

	// TransitionAbortsKey counts MsgAbortSeasonTransition per season number
	// (capped by MaxTransitionAbortsPerSeason).
	TransitionAbortsKey = collections.NewPrefix("season/aborts/")

	// PruneCursorKey holds the rolling history-pruning cursor per
	// collection name (SEA-11).
	PruneCursorKey = collections.NewPrefix("season/pruneCursor/")
)

var (
	SeasonTransitionStateKey = collections.NewPrefix("seasonTransitionState/value/")
)

var (
	TransitionRecoveryStateKey = collections.NewPrefix("transitionRecoveryState/value/")
)

var (
	NextSeasonInfoKey = collections.NewPrefix("nextSeasonInfo/value/")
)

var (
	GuildKey            = collections.NewPrefix("guild/value/")
	GuildCountKey       = collections.NewPrefix("guild/count/")
	GuildMemberCountKey = collections.NewPrefix("guild/memberCount/")
)

var (
	NominationKey        = collections.NewPrefix("nomination/value/")
	NominationCountKey   = collections.NewPrefix("nomination/count/")
	NominationStakeKey   = collections.NewPrefix("nominationStake/value/")
	RetroRewardRecordKey = collections.NewPrefix("retroRewardRecord/value/")

	// NominationsBySeasonKey indexes nomination ids by season:
	// (season, nominationID). Rebuilt at InitGenesis, kept in step by
	// Nominate and the CLEANUP-phase pruning.
	NominationsBySeasonKey = collections.NewPrefix("nomination/bySeason/")

	// ModerationQueueKey orders unappealed, active display-name moderations
	// by (moderated_at, member) so the BeginBlocker auto-resolves the oldest
	// first without rescanning resolved or appealed records. Rebuilt at
	// InitGenesis.
	ModerationQueueKey = collections.NewPrefix("displayNameModeration/queue/")
)

var (
	// AppealQueueKey orders appealed, active display-name moderations by
	// (appealed_at, member) so the BeginBlocker times out appeals nobody
	// resolved (SEA-23). Rebuilt at InitGenesis.
	AppealQueueKey = collections.NewPrefix("displayNameModeration/appealQueue/")

	// RetroPayoutRetryKey queues retro-PGF payouts that failed:
	// (season, nominationID). Rebuilt at InitGenesis from the nominations
	// (SEA-21).
	RetroPayoutRetryKey = collections.NewPrefix("nomination/payoutRetry/")

	// PendingSeasonalPoolInitKey holds the season number whose x/rep
	// seasonal pool initialisation is still owed (SEA-22).
	PendingSeasonalPoolInitKey = collections.NewPrefix("season/pendingPoolInit/")
)

var (
	// UsernameOwnerKey maps a lowercase username to the member holding it,
	// so SetUsername checks uniqueness in one read (SEA-28). Rebuilt at
	// InitGenesis from the profiles.
	UsernameOwnerKey = collections.NewPrefix("memberProfile/byUsername/")

	// GuildsByNameKey indexes guilds by lowercase name; dissolved guilds
	// index under "" (SEA-28).
	GuildsByNameKey = collections.NewPrefix("guild/byName/")
)

var (
	// NominationsByRefKey marks (season, content_ref) as nominated, so
	// Nominate's duplicate check is one read (SEA-20). Rebuilt at
	// InitGenesis.
	NominationsByRefKey = collections.NewPrefix("nomination/byRef/")

	// NominationCountKey counts nominations per (season, nominator) for
	// the max_nominations_per_member cap (SEA-20). Rebuilt at InitGenesis.
	NominationCountByNominatorKey = collections.NewPrefix("nomination/countBy/")
)

var (
	// ModerationRetryQueueKey orders unappealed moderations whose automatic
	// resolution failed by (retry_at, member) (SEA-09). Rebuilt at
	// InitGenesis from auto_resolve_retry_at.
	ModerationRetryQueueKey = collections.NewPrefix("displayNameModeration/retryQueue/")

	// SeasonStatsCountersKey holds the running per-season aggregates the
	// SeasonStats query reads (SEA-35). Exported in genesis.
	SeasonStatsCountersKey = collections.NewPrefix("seasonStats/counters/")

	// CurrentSeasonStatsKey holds the current season's running aggregates;
	// the season finalisation archives them under SeasonStatsCountersKey
	// (SEA-35). A single item keeps the XP-grant hot path to one read and
	// one write.
	CurrentSeasonStatsKey = collections.NewPrefix("seasonStats/current/")

	// GuildActiveCountKey holds the number of ACTIVE guilds, maintained by
	// an index on the Guild map (SEA-35). Rebuilt at InitGenesis.
	GuildActiveCountKey = collections.NewPrefix("guild/activeCount/")
)
