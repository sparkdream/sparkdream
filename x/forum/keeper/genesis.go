package keeper

import (
	"context"

	"sparkdream/x/forum/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	for _, elem := range genState.PostMap {
		if err := k.Post.Set(ctx, elem.PostId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.CategoryMap {
		if err := k.Category.Set(ctx, elem.CategoryId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TagMap {
		if err := k.Tag.Set(ctx, elem.Name, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ReservedTagMap {
		if err := k.ReservedTag.Set(ctx, elem.Name, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.UserRateLimitMap {
		if err := k.UserRateLimit.Set(ctx, elem.UserAddress, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.UserReactionLimitMap {
		if err := k.UserReactionLimit.Set(ctx, elem.UserAddress, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.SentinelActivityMap {
		if err := k.SentinelActivity.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.HideRecordMap {
		if err := k.HideRecord.Set(ctx, elem.PostId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ThreadLockRecordMap {
		if err := k.ThreadLockRecord.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ThreadMoveRecordMap {
		if err := k.ThreadMoveRecord.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.PostFlagMap {
		if err := k.PostFlag.Set(ctx, elem.PostId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.BountyList {
		if err := k.Bounty.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.BountySeq.Set(ctx, genState.BountyCount); err != nil {
		return err
	}
	for _, elem := range genState.TagBudgetList {
		if err := k.TagBudget.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.TagBudgetSeq.Set(ctx, genState.TagBudgetCount); err != nil {
		return err
	}
	for _, elem := range genState.TagBudgetAwardList {
		if err := k.TagBudgetAward.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.TagBudgetAwardSeq.Set(ctx, genState.TagBudgetAwardCount); err != nil {
		return err
	}
	for _, elem := range genState.ThreadMetadataMap {
		if err := k.ThreadMetadata.Set(ctx, elem.ThreadId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ThreadFollowMap {
		if err := k.ThreadFollow.Set(ctx, elem.Follower, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ThreadFollowCountMap {
		if err := k.ThreadFollowCount.Set(ctx, elem.ThreadId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ArchivedThreadMap {
		if err := k.ArchivedThread.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ArchiveMetadataMap {
		if err := k.ArchiveMetadata.Set(ctx, elem.RootId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TagReportMap {
		if err := k.TagReport.Set(ctx, elem.TagName, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.MemberSalvationStatusMap {
		if err := k.MemberSalvationStatus.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.JuryParticipationMap {
		if err := k.JuryParticipation.Set(ctx, elem.Juror, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.MemberReportMap {
		if err := k.MemberReport.Set(ctx, elem.Member, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.MemberWarningList {
		if err := k.MemberWarning.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.MemberWarningSeq.Set(ctx, genState.MemberWarningCount); err != nil {
		return err
	}
	for _, elem := range genState.GovActionAppealList {
		if err := k.GovActionAppeal.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.GovActionAppealSeq.Set(ctx, genState.GovActionAppealCount); err != nil {
		return err
	}

	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Prime PostSeq and CategorySeq to start at 1 if not already advanced.
	// ID 0 is reserved (PostId=0 conflicts with ParentId=0 meaning "no parent",
	// and CategoryId=0 is confusing as a valid category).
	postSeqVal, err := k.PostSeq.Peek(ctx)
	if err != nil {
		return err
	}
	if postSeqVal == 0 && len(genState.PostMap) == 0 {
		if _, err := k.PostSeq.Next(ctx); err != nil {
			return err
		}
	}

	catSeqVal, err := k.CategorySeq.Peek(ctx)
	if err != nil {
		return err
	}
	if catSeqVal == 0 && len(genState.CategoryMap) == 0 {
		if _, err := k.CategorySeq.Next(ctx); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Post.Walk(ctx, nil, func(_ uint64, val types.Post) (stop bool, err error) {
		genesis.PostMap = append(genesis.PostMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Category.Walk(ctx, nil, func(_ uint64, val types.Category) (stop bool, err error) {
		genesis.CategoryMap = append(genesis.CategoryMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Tag.Walk(ctx, nil, func(_ string, val types.Tag) (stop bool, err error) {
		genesis.TagMap = append(genesis.TagMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ReservedTag.Walk(ctx, nil, func(_ string, val types.ReservedTag) (stop bool, err error) {
		genesis.ReservedTagMap = append(genesis.ReservedTagMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UserRateLimit.Walk(ctx, nil, func(_ string, val types.UserRateLimit) (stop bool, err error) {
		genesis.UserRateLimitMap = append(genesis.UserRateLimitMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.UserReactionLimit.Walk(ctx, nil, func(_ string, val types.UserReactionLimit) (stop bool, err error) {
		genesis.UserReactionLimitMap = append(genesis.UserReactionLimitMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.SentinelActivity.Walk(ctx, nil, func(_ string, val types.SentinelActivity) (stop bool, err error) {
		genesis.SentinelActivityMap = append(genesis.SentinelActivityMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.HideRecord.Walk(ctx, nil, func(_ uint64, val types.HideRecord) (stop bool, err error) {
		genesis.HideRecordMap = append(genesis.HideRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadLockRecord.Walk(ctx, nil, func(_ uint64, val types.ThreadLockRecord) (stop bool, err error) {
		genesis.ThreadLockRecordMap = append(genesis.ThreadLockRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadMoveRecord.Walk(ctx, nil, func(_ uint64, val types.ThreadMoveRecord) (stop bool, err error) {
		genesis.ThreadMoveRecordMap = append(genesis.ThreadMoveRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.PostFlag.Walk(ctx, nil, func(_ uint64, val types.PostFlag) (stop bool, err error) {
		genesis.PostFlagMap = append(genesis.PostFlagMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.Bounty.Walk(ctx, nil, func(key uint64, elem types.Bounty) (bool, error) {
		genesis.BountyList = append(genesis.BountyList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.BountyCount, err = k.BountySeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.TagBudget.Walk(ctx, nil, func(key uint64, elem types.TagBudget) (bool, error) {
		genesis.TagBudgetList = append(genesis.TagBudgetList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.TagBudgetCount, err = k.TagBudgetSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.TagBudgetAward.Walk(ctx, nil, func(key uint64, elem types.TagBudgetAward) (bool, error) {
		genesis.TagBudgetAwardList = append(genesis.TagBudgetAwardList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.TagBudgetAwardCount, err = k.TagBudgetAwardSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.ThreadMetadata.Walk(ctx, nil, func(_ uint64, val types.ThreadMetadata) (stop bool, err error) {
		genesis.ThreadMetadataMap = append(genesis.ThreadMetadataMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadFollow.Walk(ctx, nil, func(_ string, val types.ThreadFollow) (stop bool, err error) {
		genesis.ThreadFollowMap = append(genesis.ThreadFollowMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ThreadFollowCount.Walk(ctx, nil, func(_ uint64, val types.ThreadFollowCount) (stop bool, err error) {
		genesis.ThreadFollowCountMap = append(genesis.ThreadFollowCountMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ArchivedThread.Walk(ctx, nil, func(_ uint64, val types.ArchivedThread) (stop bool, err error) {
		genesis.ArchivedThreadMap = append(genesis.ArchivedThreadMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ArchiveMetadata.Walk(ctx, nil, func(_ uint64, val types.ArchiveMetadata) (stop bool, err error) {
		genesis.ArchiveMetadataMap = append(genesis.ArchiveMetadataMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TagReport.Walk(ctx, nil, func(_ string, val types.TagReport) (stop bool, err error) {
		genesis.TagReportMap = append(genesis.TagReportMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MemberSalvationStatus.Walk(ctx, nil, func(_ string, val types.MemberSalvationStatus) (stop bool, err error) {
		genesis.MemberSalvationStatusMap = append(genesis.MemberSalvationStatusMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.JuryParticipation.Walk(ctx, nil, func(_ string, val types.JuryParticipation) (stop bool, err error) {
		genesis.JuryParticipationMap = append(genesis.JuryParticipationMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MemberReport.Walk(ctx, nil, func(_ string, val types.MemberReport) (stop bool, err error) {
		genesis.MemberReportMap = append(genesis.MemberReportMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.MemberWarning.Walk(ctx, nil, func(key uint64, elem types.MemberWarning) (bool, error) {
		genesis.MemberWarningList = append(genesis.MemberWarningList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.MemberWarningCount, err = k.MemberWarningSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.GovActionAppeal.Walk(ctx, nil, func(key uint64, elem types.GovActionAppeal) (bool, error) {
		genesis.GovActionAppealList = append(genesis.GovActionAppealList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.GovActionAppealCount, err = k.GovActionAppealSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	return genesis, nil
}
