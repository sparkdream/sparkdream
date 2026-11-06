package keeper

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	"sparkdream/x/season/types"
)

// NominationStakeIndexes are the secondary indexes of NominationStake.
type NominationStakeIndexes struct {
	// ByStaker: (staker, "nominationId/staker"), so the zeroing hook reads
	// one member's stakes instead of the whole table (SEA-104).
	ByStaker *indexes.Multi[string, string, types.NominationStake]
}

func (i NominationStakeIndexes) IndexesList() []collections.Index[string, types.NominationStake] {
	return []collections.Index[string, types.NominationStake]{i.ByStaker}
}

func newNominationStakeIndexes(sb *collections.SchemaBuilder) NominationStakeIndexes {
	return NominationStakeIndexes{
		ByStaker: indexes.NewMulti(sb, types.NominationStakesByStakerKey, "nomination_stake_by_staker",
			collections.StringKey, collections.StringKey,
			func(_ string, v types.NominationStake) (string, error) {
				return v.Staker, nil
			}),
	}
}

// DisplayNameReportStakeIndexes are the secondary indexes of
// DisplayNameReportStake.
type DisplayNameReportStakeIndexes struct {
	// ByReporter: (reporter, challenge id), read by the zeroing hook
	// (SEA-101).
	ByReporter *indexes.Multi[string, string, types.DisplayNameReportStake]
}

func (i DisplayNameReportStakeIndexes) IndexesList() []collections.Index[string, types.DisplayNameReportStake] {
	return []collections.Index[string, types.DisplayNameReportStake]{i.ByReporter}
}

func newDisplayNameReportStakeIndexes(sb *collections.SchemaBuilder) DisplayNameReportStakeIndexes {
	return DisplayNameReportStakeIndexes{
		ByReporter: indexes.NewMulti(sb, types.DisplayNameReportStakesByReporterKey, "display_name_report_stake_by_reporter",
			collections.StringKey, collections.StringKey,
			func(_ string, v types.DisplayNameReportStake) (string, error) {
				return v.Reporter, nil
			}),
	}
}

// DisplayNameAppealStakeIndexes are the secondary indexes of
// DisplayNameAppealStake.
type DisplayNameAppealStakeIndexes struct {
	// ByAppellant: (appellant, challenge id), read by the zeroing hook
	// (SEA-101).
	ByAppellant *indexes.Multi[string, string, types.DisplayNameAppealStake]
}

func (i DisplayNameAppealStakeIndexes) IndexesList() []collections.Index[string, types.DisplayNameAppealStake] {
	return []collections.Index[string, types.DisplayNameAppealStake]{i.ByAppellant}
}

func newDisplayNameAppealStakeIndexes(sb *collections.SchemaBuilder) DisplayNameAppealStakeIndexes {
	return DisplayNameAppealStakeIndexes{
		ByAppellant: indexes.NewMulti(sb, types.DisplayNameAppealStakesByAppellantKey, "display_name_appeal_stake_by_appellant",
			collections.StringKey, collections.StringKey,
			func(_ string, v types.DisplayNameAppealStake) (string, error) {
				return v.Appellant, nil
			}),
	}
}
