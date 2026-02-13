package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:  DefaultParams(),
		PostMap: []Post{}, CategoryMap: []Category{}, TagMap: []Tag{}, ReservedTagMap: []ReservedTag{}, UserRateLimitMap: []UserRateLimit{}, UserReactionLimitMap: []UserReactionLimit{}, SentinelActivityMap: []SentinelActivity{}, HideRecordMap: []HideRecord{}, ThreadLockRecordMap: []ThreadLockRecord{}, ThreadMoveRecordMap: []ThreadMoveRecord{}, PostFlagMap: []PostFlag{}, BountyList: []Bounty{}, TagBudgetList: []TagBudget{}, TagBudgetAwardList: []TagBudgetAward{}, ThreadMetadataMap: []ThreadMetadata{}, ThreadFollowMap: []ThreadFollow{}, ThreadFollowCountMap: []ThreadFollowCount{}, ArchiveMetadataMap: []ArchiveMetadata{}, TagReportMap: []TagReport{}, MemberSalvationStatusMap: []MemberSalvationStatus{}, JuryParticipationMap: []JuryParticipation{}, MemberReportMap: []MemberReport{}, MemberWarningList: []MemberWarning{}, GovActionAppealList: []GovActionAppeal{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	postIndexMap := make(map[string]struct{})

	for _, elem := range gs.PostMap {
		index := fmt.Sprint(elem.PostId)
		if _, ok := postIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for post")
		}
		postIndexMap[index] = struct{}{}
	}
	categoryIndexMap := make(map[string]struct{})

	for _, elem := range gs.CategoryMap {
		index := fmt.Sprint(elem.CategoryId)
		if _, ok := categoryIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for category")
		}
		categoryIndexMap[index] = struct{}{}
	}
	tagIndexMap := make(map[string]struct{})

	for _, elem := range gs.TagMap {
		index := fmt.Sprint(elem.Name)
		if _, ok := tagIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tag")
		}
		tagIndexMap[index] = struct{}{}
	}
	reservedTagIndexMap := make(map[string]struct{})

	for _, elem := range gs.ReservedTagMap {
		index := fmt.Sprint(elem.Name)
		if _, ok := reservedTagIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for reservedTag")
		}
		reservedTagIndexMap[index] = struct{}{}
	}
	userRateLimitIndexMap := make(map[string]struct{})

	for _, elem := range gs.UserRateLimitMap {
		index := fmt.Sprint(elem.UserAddress)
		if _, ok := userRateLimitIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for userRateLimit")
		}
		userRateLimitIndexMap[index] = struct{}{}
	}
	userReactionLimitIndexMap := make(map[string]struct{})

	for _, elem := range gs.UserReactionLimitMap {
		index := fmt.Sprint(elem.UserAddress)
		if _, ok := userReactionLimitIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for userReactionLimit")
		}
		userReactionLimitIndexMap[index] = struct{}{}
	}
	sentinelActivityIndexMap := make(map[string]struct{})

	for _, elem := range gs.SentinelActivityMap {
		index := fmt.Sprint(elem.Address)
		if _, ok := sentinelActivityIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for sentinelActivity")
		}
		sentinelActivityIndexMap[index] = struct{}{}
	}
	hideRecordIndexMap := make(map[string]struct{})

	for _, elem := range gs.HideRecordMap {
		index := fmt.Sprint(elem.PostId)
		if _, ok := hideRecordIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for hideRecord")
		}
		hideRecordIndexMap[index] = struct{}{}
	}
	threadLockRecordIndexMap := make(map[string]struct{})

	for _, elem := range gs.ThreadLockRecordMap {
		index := fmt.Sprint(elem.RootId)
		if _, ok := threadLockRecordIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for threadLockRecord")
		}
		threadLockRecordIndexMap[index] = struct{}{}
	}
	threadMoveRecordIndexMap := make(map[string]struct{})

	for _, elem := range gs.ThreadMoveRecordMap {
		index := fmt.Sprint(elem.RootId)
		if _, ok := threadMoveRecordIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for threadMoveRecord")
		}
		threadMoveRecordIndexMap[index] = struct{}{}
	}
	postFlagIndexMap := make(map[string]struct{})

	for _, elem := range gs.PostFlagMap {
		index := fmt.Sprint(elem.PostId)
		if _, ok := postFlagIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for postFlag")
		}
		postFlagIndexMap[index] = struct{}{}
	}
	bountyIdMap := make(map[uint64]bool)
	bountyCount := gs.GetBountyCount()
	for _, elem := range gs.BountyList {
		if _, ok := bountyIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for bounty")
		}
		if elem.Id >= bountyCount {
			return fmt.Errorf("bounty id should be lower or equal than the last id")
		}
		bountyIdMap[elem.Id] = true
	}
	tagBudgetIdMap := make(map[uint64]bool)
	tagBudgetCount := gs.GetTagBudgetCount()
	for _, elem := range gs.TagBudgetList {
		if _, ok := tagBudgetIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for tagBudget")
		}
		if elem.Id >= tagBudgetCount {
			return fmt.Errorf("tagBudget id should be lower or equal than the last id")
		}
		tagBudgetIdMap[elem.Id] = true
	}
	tagBudgetAwardIdMap := make(map[uint64]bool)
	tagBudgetAwardCount := gs.GetTagBudgetAwardCount()
	for _, elem := range gs.TagBudgetAwardList {
		if _, ok := tagBudgetAwardIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for tagBudgetAward")
		}
		if elem.Id >= tagBudgetAwardCount {
			return fmt.Errorf("tagBudgetAward id should be lower or equal than the last id")
		}
		tagBudgetAwardIdMap[elem.Id] = true
	}
	threadMetadataIndexMap := make(map[string]struct{})

	for _, elem := range gs.ThreadMetadataMap {
		index := fmt.Sprint(elem.ThreadId)
		if _, ok := threadMetadataIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for threadMetadata")
		}
		threadMetadataIndexMap[index] = struct{}{}
	}
	threadFollowIndexMap := make(map[string]struct{})

	for _, elem := range gs.ThreadFollowMap {
		index := fmt.Sprint(elem.Follower)
		if _, ok := threadFollowIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for threadFollow")
		}
		threadFollowIndexMap[index] = struct{}{}
	}
	threadFollowCountIndexMap := make(map[string]struct{})

	for _, elem := range gs.ThreadFollowCountMap {
		index := fmt.Sprint(elem.ThreadId)
		if _, ok := threadFollowCountIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for threadFollowCount")
		}
		threadFollowCountIndexMap[index] = struct{}{}
	}
	archiveMetadataIndexMap := make(map[string]struct{})

	for _, elem := range gs.ArchiveMetadataMap {
		index := fmt.Sprint(elem.RootId)
		if _, ok := archiveMetadataIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for archiveMetadata")
		}
		archiveMetadataIndexMap[index] = struct{}{}
	}
	tagReportIndexMap := make(map[string]struct{})

	for _, elem := range gs.TagReportMap {
		index := fmt.Sprint(elem.TagName)
		if _, ok := tagReportIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tagReport")
		}
		tagReportIndexMap[index] = struct{}{}
	}
	memberSalvationStatusIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberSalvationStatusMap {
		index := fmt.Sprint(elem.Address)
		if _, ok := memberSalvationStatusIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for memberSalvationStatus")
		}
		memberSalvationStatusIndexMap[index] = struct{}{}
	}
	juryParticipationIndexMap := make(map[string]struct{})

	for _, elem := range gs.JuryParticipationMap {
		index := fmt.Sprint(elem.Juror)
		if _, ok := juryParticipationIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for juryParticipation")
		}
		juryParticipationIndexMap[index] = struct{}{}
	}
	memberReportIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberReportMap {
		index := fmt.Sprint(elem.Member)
		if _, ok := memberReportIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for memberReport")
		}
		memberReportIndexMap[index] = struct{}{}
	}
	memberWarningIdMap := make(map[uint64]bool)
	memberWarningCount := gs.GetMemberWarningCount()
	for _, elem := range gs.MemberWarningList {
		if _, ok := memberWarningIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for memberWarning")
		}
		if elem.Id >= memberWarningCount {
			return fmt.Errorf("memberWarning id should be lower or equal than the last id")
		}
		memberWarningIdMap[elem.Id] = true
	}
	govActionAppealIdMap := make(map[uint64]bool)
	govActionAppealCount := gs.GetGovActionAppealCount()
	for _, elem := range gs.GovActionAppealList {
		if _, ok := govActionAppealIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for govActionAppeal")
		}
		if elem.Id >= govActionAppealCount {
			return fmt.Errorf("govActionAppeal id should be lower or equal than the last id")
		}
		govActionAppealIdMap[elem.Id] = true
	}

	return gs.Params.Validate()
}
