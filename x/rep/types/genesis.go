package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:                 DefaultParams(),
		MemberMap:              []Member{},
		InvitationList:         []Invitation{},
		InvitationCount:        1, // Start at 1 so first ID is 1 (0 is reserved for "unset")
		ProjectList:            []Project{},
		ProjectCount:           1,
		InitiativeList:         []Initiative{},
		InitiativeCount:        1,
		StakeList:              []Stake{},
		StakeCount:             1,
		ChallengeList:          []Challenge{},
		ChallengeCount:         1,
		JuryReviewList:         []JuryReview{},
		JuryReviewCount:        1,
		InterimList:            []Interim{},
		InterimCount:           1,
		InterimTemplateMap:     []InterimTemplate{},
		ContentChallengeList:   []ContentChallenge{},
		ContentChallengeCount:  1,
		ContentInitiativeLinks: []ContentInitiativeLink{},
		TagBudgetList:          []TagBudget{},
		TagBudgetCount:         1,
		TagBudgetAwardList:     []TagBudgetAward{},
		TagBudgetAwardCount:    1,
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	memberIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberMap {
		index := fmt.Sprint(elem.Address)
		if _, ok := memberIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for member")
		}
		memberIndexMap[index] = struct{}{}
	}
	invitationIdMap := make(map[uint64]bool)
	invitationCount := gs.GetInvitationCount()
	for _, elem := range gs.InvitationList {
		if _, ok := invitationIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for invitation")
		}
		if elem.Id >= invitationCount {
			return fmt.Errorf("invitation id should be lower or equal than the last id")
		}
		invitationIdMap[elem.Id] = true
	}
	projectIdMap := make(map[uint64]bool)
	projectCount := gs.GetProjectCount()
	for _, elem := range gs.ProjectList {
		if _, ok := projectIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for project")
		}
		if elem.Id >= projectCount {
			return fmt.Errorf("project id should be lower or equal than the last id")
		}
		projectIdMap[elem.Id] = true
	}
	initiativeIdMap := make(map[uint64]bool)
	initiativeCount := gs.GetInitiativeCount()
	for _, elem := range gs.InitiativeList {
		if _, ok := initiativeIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for initiative")
		}
		if elem.Id >= initiativeCount {
			return fmt.Errorf("initiative id should be lower or equal than the last id")
		}
		initiativeIdMap[elem.Id] = true
	}
	stakeIdMap := make(map[uint64]bool)
	stakeCount := gs.GetStakeCount()
	for _, elem := range gs.StakeList {
		if _, ok := stakeIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for stake")
		}
		if elem.Id >= stakeCount {
			return fmt.Errorf("stake id should be lower or equal than the last id")
		}
		stakeIdMap[elem.Id] = true
	}
	challengeIdMap := make(map[uint64]bool)
	challengeCount := gs.GetChallengeCount()
	for _, elem := range gs.ChallengeList {
		if _, ok := challengeIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for challenge")
		}
		if elem.Id >= challengeCount {
			return fmt.Errorf("challenge id should be lower or equal than the last id")
		}
		challengeIdMap[elem.Id] = true
	}
	juryReviewIdMap := make(map[uint64]bool)
	juryReviewCount := gs.GetJuryReviewCount()
	for _, elem := range gs.JuryReviewList {
		if _, ok := juryReviewIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for juryReview")
		}
		if elem.Id >= juryReviewCount {
			return fmt.Errorf("juryReview id should be lower or equal than the last id")
		}
		juryReviewIdMap[elem.Id] = true
	}
	interimIdMap := make(map[uint64]bool)
	interimCount := gs.GetInterimCount()
	for _, elem := range gs.InterimList {
		if _, ok := interimIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for interim")
		}
		if elem.Id >= interimCount {
			return fmt.Errorf("interim id should be lower or equal than the last id")
		}
		interimIdMap[elem.Id] = true
	}
	interimTemplateIndexMap := make(map[string]struct{})

	for _, elem := range gs.InterimTemplateMap {
		index := fmt.Sprint(elem.Id)
		if _, ok := interimTemplateIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for interimTemplate")
		}
		interimTemplateIndexMap[index] = struct{}{}
	}

	// Content challenge validation
	contentChallengeIdMap := make(map[uint64]bool)
	contentChallengeCount := gs.GetContentChallengeCount()
	for _, elem := range gs.ContentChallengeList {
		if _, ok := contentChallengeIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for contentChallenge")
		}
		if elem.Id >= contentChallengeCount {
			return fmt.Errorf("contentChallenge id should be lower or equal than the last id")
		}
		contentChallengeIdMap[elem.Id] = true
	}

	// Content initiative link validation
	linkKeyMap := make(map[string]bool)
	for _, link := range gs.ContentInitiativeLinks {
		if link.InitiativeId == 0 {
			return fmt.Errorf("content initiative link has zero initiative_id")
		}
		key := fmt.Sprintf("%d-%d-%d", link.InitiativeId, link.TargetType, link.TargetId)
		if linkKeyMap[key] {
			return fmt.Errorf("duplicated content initiative link: %s", key)
		}
		linkKeyMap[key] = true
	}

	tagReportIndexMap := make(map[string]struct{})
	for _, elem := range gs.TagReportMap {
		index := fmt.Sprint(elem.TagName)
		if _, ok := tagReportIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for tagReport")
		}
		tagReportIndexMap[index] = struct{}{}
	}

	tagBudgetIDMap := make(map[uint64]bool)
	tagBudgetCount := gs.GetTagBudgetCount()
	for _, elem := range gs.TagBudgetList {
		if _, ok := tagBudgetIDMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for tagBudget")
		}
		if elem.Id >= tagBudgetCount {
			return fmt.Errorf("tagBudget id should be lower or equal than the last id")
		}
		tagBudgetIDMap[elem.Id] = true
	}

	tagBudgetAwardIDMap := make(map[uint64]bool)
	tagBudgetAwardCount := gs.GetTagBudgetAwardCount()
	for _, elem := range gs.TagBudgetAwardList {
		if _, ok := tagBudgetAwardIDMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for tagBudgetAward")
		}
		if elem.Id >= tagBudgetAwardCount {
			return fmt.Errorf("tagBudgetAward id should be lower or equal than the last id")
		}
		tagBudgetAwardIDMap[elem.Id] = true
	}

	return gs.Params.Validate()
}
