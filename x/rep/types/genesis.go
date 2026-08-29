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
		InitiativeReviewList:   []InitiativeReview{},
		InterimCount:           1,
		ContentChallengeList:   []ContentChallenge{},
		ContentChallengeCount:  1,
		ContentInitiativeLinks: []ContentInitiativeLink{},
		TagBudgetList:          []TagBudget{},
		TagBudgetCount:         1,
		TagBudgetAwardList:     []TagBudgetAward{},
		TagBudgetAwardCount:    1,
		JuryParticipationMap:   []JuryParticipation{},
		MemberReportMap:        []MemberReport{},
		MemberWarningList:      []MemberWarning{},
		GovActionAppealList:    []GovActionAppeal{},
		BondedRoleList:         []BondedRole{},
		BondedRoleConfigList:   DefaultBondedRoleConfigs(),
	}
}

// DefaultBondedRoleConfigs seeds the per-role policy configs at chain start.
// Owning modules (x/forum for SENTINEL, x/collect for COLLECT_CURATOR) are
// expected to overwrite these via SetBondedRoleConfig during their own
// InitGenesis to keep module operational params in sync with rep's enforcement
// state. The seed values below mirror the constants previously hardcoded in
// x/rep/keeper/msg_server_bond_sentinel.go and the curator defaults from
// x/collect/types/params.go so the chain boots coherently even if a module
// fails to write-through.
func DefaultBondedRoleConfigs() []BondedRoleConfig {
	return []BondedRoleConfig{
		{
			// Seed for the initiative reviewer. x/rep owns this role itself and
			// writes it through from its own params on InitGenesis and on every
			// operational param update (SyncReviewerBondedRoleConfig), so these
			// values are a stand-in like the ones below, not the operative config.
			// Keep them in step with DefaultParams anyway: they are what a chain
			// sees if the write-through is ever skipped.
			//
			// 500 DREAM, chosen as a low barrier to entry so reviewing is something
			// an ordinary member can take up. An earlier 5,000 was justified here as
			// "half the maximum single-approval harm", which misread the mechanism:
			// SlashReviewersOnOverturn charges r.BondReserved, the per-verdict
			// reserve of reviewer_bond_reserve_rate x budget, never the floor.
			// Liability already scales with what the review could mint, so the floor
			// only has to price entry and give demotion a threshold to sit under.
			// Reviewers who want larger initiatives raise their own ceiling with a
			// further MsgBondRole on the same record.
			//
			// Denominated in udream like every other bond on the chain (x/forum's
			// min_sentinel_bond and x/collect's min_curator_bond happen to be
			// 500_000_000 too, but their per-action commit is flat where this one
			// scales with budget, so the match is a coincidence, not a rationale).
			// The whole-DREAM stand-ins below never take effect.
			RoleType: RoleType_ROLE_TYPE_INITIATIVE_REVIEWER,
			MinBond:  "500000000", // 500 DREAM
			// Eligibility is the trust level, not a separate rep tier. Every other
			// bonded role gates on min_trust_level alone and leaves this at 0 — the
			// sentinel's seed below reads 3 but x/forum writes 0 through on
			// InitGenesis, so 3 was only ever operative here, back when no module
			// wrote this role through. That made the reviewer the one role nobody
			// could qualify for on a fresh chain: trust levels are seeded in
			// genesis, reputation scores are left empty in every genesis we ship,
			// so seeded members sat at ESTABLISHED with tier 0 and no way to bond.
			// The trust ladder already encodes reputation, so the tier check was a
			// second, stricter copy of the same gate rather than an independent one.
			MinRepTier:        0,
			MinTrustLevel:     "TRUST_LEVEL_ESTABLISHED",
			MinAgeBlocks:      0,
			DemotionCooldown:  604800,      // 7 days
			DemotionThreshold: "250000000", // 250 DREAM: half the floor
			UnbondCooldown:    1209600,     // 14 days: bond stays slashable while open verdicts age out
		},
		{
			RoleType:          RoleType_ROLE_TYPE_CONTENT_SENTINEL,
			MinBond:           "1000",
			MinRepTier:        3,
			MinTrustLevel:     "",
			MinAgeBlocks:      0,
			DemotionCooldown:  604800, // 7 days
			DemotionThreshold: "500",
			UnbondCooldown:    1209600, // 14 days (overwritten by SyncSentinelBondedRoleConfig)
		},
		{
			RoleType:          RoleType_ROLE_TYPE_COLLECT_CURATOR,
			MinBond:           "500",
			MinRepTier:        0,
			MinTrustLevel:     "TRUST_LEVEL_PROVISIONAL",
			MinAgeBlocks:      0, // No cooldown; collect overwrites this via SyncCuratorBondedRoleConfig on InitGenesis
			DemotionCooldown:  604800,
			DemotionThreshold: "250",
			UnbondCooldown:    604800, // 7 days (overwritten by SyncCuratorBondedRoleConfig)
		},
		{
			// Seeds for federation verifier (see x/federation params for the
			// source-of-truth values; federation writes through on InitGenesis).
			RoleType:          RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			MinBond:           "500",
			MinRepTier:        0,
			MinTrustLevel:     "TRUST_LEVEL_ESTABLISHED",
			MinAgeBlocks:      0,
			DemotionCooldown:  604800, // 7 days
			DemotionThreshold: "250",
			UnbondCooldown:    1209600, // 14 days (overwritten by federation sync if present)
		},
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

	bondedRoleConfigIndex := make(map[int32]struct{})
	for _, cfg := range gs.BondedRoleConfigList {
		if cfg.RoleType == RoleType_ROLE_TYPE_UNSPECIFIED {
			return fmt.Errorf("bonded role config has unspecified role_type")
		}
		if _, ok := bondedRoleConfigIndex[int32(cfg.RoleType)]; ok {
			return fmt.Errorf("duplicated bonded role config for role_type %s", cfg.RoleType.String())
		}
		bondedRoleConfigIndex[int32(cfg.RoleType)] = struct{}{}
	}

	bondedRoleIndex := make(map[string]struct{})
	for _, br := range gs.BondedRoleList {
		if br.RoleType == RoleType_ROLE_TYPE_UNSPECIFIED {
			return fmt.Errorf("bonded role has unspecified role_type")
		}
		key := fmt.Sprintf("%d/%s", int32(br.RoleType), br.Address)
		if _, ok := bondedRoleIndex[key]; ok {
			return fmt.Errorf("duplicated bonded role for %s", key)
		}
		bondedRoleIndex[key] = struct{}{}
	}

	roleActivityIndex := make(map[string]struct{})
	for _, ra := range gs.RoleActivityList {
		if ra.RoleType == RoleType_ROLE_TYPE_UNSPECIFIED {
			return fmt.Errorf("role activity has unspecified role_type")
		}
		key := fmt.Sprintf("%d/%s", int32(ra.RoleType), ra.Address)
		if _, ok := roleActivityIndex[key]; ok {
			return fmt.Errorf("duplicated role activity for %s", key)
		}
		roleActivityIndex[key] = struct{}{}
	}

	escalatedIndex := make(map[uint64]struct{})
	for _, id := range gs.EscalatedReviewList {
		if _, ok := escalatedIndex[id]; ok {
			return fmt.Errorf("duplicated escalated review for initiative %d", id)
		}
		escalatedIndex[id] = struct{}{}
	}

	dayFundingIndex := make(map[uint64]struct{})
	for _, df := range gs.RoleRewardDayFundingList {
		if _, ok := dayFundingIndex[df.Day]; ok {
			// A duplicate would be silently collapsed on import, under-reporting
			// the day's draw and handing back part of an allowance already spent.
			return fmt.Errorf("duplicated role reward day funding for day %d", df.Day)
		}
		if df.AmountFunded.IsNil() || df.AmountFunded.IsNegative() {
			return fmt.Errorf("role reward day funding for day %d must be non-negative", df.Day)
		}
		dayFundingIndex[df.Day] = struct{}{}
	}

	return gs.Params.Validate()
}
