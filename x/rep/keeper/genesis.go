package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	for _, elem := range genState.MemberMap {
		if err := k.Member.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.InvitationList {
		if err := k.Invitation.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InvitationSeq.Set(ctx, genState.InvitationCount); err != nil {
		return err
	}
	for _, elem := range genState.ProjectList {
		if err := k.Project.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.ProjectSeq.Set(ctx, genState.ProjectCount); err != nil {
		return err
	}
	for _, elem := range genState.InitiativeList {
		if err := k.Initiative.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InitiativeSeq.Set(ctx, genState.InitiativeCount); err != nil {
		return err
	}
	for _, elem := range genState.StakeList {
		if err := k.Stake.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.StakeSeq.Set(ctx, genState.StakeCount); err != nil {
		return err
	}
	for _, elem := range genState.ChallengeList {
		if err := k.Challenge.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.ChallengeSeq.Set(ctx, genState.ChallengeCount); err != nil {
		return err
	}
	for _, elem := range genState.JuryReviewList {
		if err := k.JuryReview.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.JuryReviewSeq.Set(ctx, genState.JuryReviewCount); err != nil {
		return err
	}
	for _, elem := range genState.InterimList {
		if err := k.Interim.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InterimSeq.Set(ctx, genState.InterimCount); err != nil {
		return err
	}
	for _, elem := range genState.InterimTemplateMap {
		if err := k.InterimTemplate.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	// Content challenges
	for _, elem := range genState.ContentChallengeList {
		if err := k.ContentChallenge.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.ContentChallengeSeq.Set(ctx, genState.ContentChallengeCount); err != nil {
		return err
	}

	// Content initiative links
	for _, link := range genState.ContentInitiativeLinks {
		key := collections.Join(link.InitiativeId, collections.Join(link.TargetType, link.TargetId))
		if err := k.ContentInitiativeLinks.Set(ctx, key); err != nil {
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
	for _, elem := range genState.TagReportMap {
		if err := k.TagReport.Set(ctx, elem.TagName, elem); err != nil {
			return err
		}
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

	// Accountability
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

	// Bonded-role configs and records.
	for _, cfg := range genState.BondedRoleConfigList {
		if err := k.BondedRoleConfigs.Set(ctx, int32(cfg.RoleType), cfg); err != nil {
			return err
		}
	}
	for _, br := range genState.BondedRoleList {
		if err := k.BondedRoles.Set(ctx, collections.Join(int32(br.RoleType), br.Address), br); err != nil {
			return err
		}
	}

	// If there are members, trigger a full trust tree rebuild on next EndBlock.
	// The tree is derived state (not exported in genesis) and will be populated
	// from member records + voter registrations.
	if len(genState.MemberMap) > 0 {
		k.MarkTrustTreeDirty(ctx)
	}

	return k.Params.Set(ctx, genState.Params)
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Member.Walk(ctx, nil, func(_ string, val types.Member) (stop bool, err error) {
		genesis.MemberMap = append(genesis.MemberMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.Invitation.Walk(ctx, nil, func(key uint64, elem types.Invitation) (bool, error) {
		genesis.InvitationList = append(genesis.InvitationList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InvitationCount, err = k.InvitationSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Project.Walk(ctx, nil, func(key uint64, elem types.Project) (bool, error) {
		genesis.ProjectList = append(genesis.ProjectList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.ProjectCount, err = k.ProjectSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Initiative.Walk(ctx, nil, func(key uint64, elem types.Initiative) (bool, error) {
		genesis.InitiativeList = append(genesis.InitiativeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InitiativeCount, err = k.InitiativeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Stake.Walk(ctx, nil, func(key uint64, elem types.Stake) (bool, error) {
		genesis.StakeList = append(genesis.StakeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.StakeCount, err = k.StakeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Challenge.Walk(ctx, nil, func(key uint64, elem types.Challenge) (bool, error) {
		genesis.ChallengeList = append(genesis.ChallengeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.ChallengeCount, err = k.ChallengeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.JuryReview.Walk(ctx, nil, func(key uint64, elem types.JuryReview) (bool, error) {
		genesis.JuryReviewList = append(genesis.JuryReviewList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.JuryReviewCount, err = k.JuryReviewSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.Interim.Walk(ctx, nil, func(key uint64, elem types.Interim) (bool, error) {
		genesis.InterimList = append(genesis.InterimList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InterimCount, err = k.InterimSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.InterimTemplate.Walk(ctx, nil, func(_ string, val types.InterimTemplate) (stop bool, err error) {
		genesis.InterimTemplateMap = append(genesis.InterimTemplateMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Content challenges
	err = k.ContentChallenge.Walk(ctx, nil, func(key uint64, elem types.ContentChallenge) (bool, error) {
		genesis.ContentChallengeList = append(genesis.ContentChallengeList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.ContentChallengeCount, err = k.ContentChallengeSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	// Content initiative links
	err = k.ContentInitiativeLinks.Walk(ctx, nil, func(key collections.Pair[uint64, collections.Pair[int32, uint64]]) (bool, error) {
		genesis.ContentInitiativeLinks = append(genesis.ContentInitiativeLinks, types.ContentInitiativeLink{
			InitiativeId: key.K1(),
			TargetType:   key.K2().K1(),
			TargetId:     key.K2().K2(),
		})
		return false, nil
	})
	if err != nil {
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
	if err := k.TagReport.Walk(ctx, nil, func(_ string, val types.TagReport) (stop bool, err error) {
		genesis.TagReportMap = append(genesis.TagReportMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	if err := k.TagBudget.Walk(ctx, nil, func(_ uint64, val types.TagBudget) (bool, error) {
		genesis.TagBudgetList = append(genesis.TagBudgetList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.TagBudgetCount, err = k.TagBudgetSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	if err := k.TagBudgetAward.Walk(ctx, nil, func(_ uint64, val types.TagBudgetAward) (bool, error) {
		genesis.TagBudgetAwardList = append(genesis.TagBudgetAwardList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.TagBudgetAwardCount, err = k.TagBudgetAwardSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	// Accountability
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
	if err := k.MemberWarning.Walk(ctx, nil, func(_ uint64, val types.MemberWarning) (stop bool, err error) {
		genesis.MemberWarningList = append(genesis.MemberWarningList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.MemberWarningCount, err = k.MemberWarningSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.GovActionAppeal.Walk(ctx, nil, func(_ uint64, val types.GovActionAppeal) (stop bool, err error) {
		genesis.GovActionAppealList = append(genesis.GovActionAppealList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.GovActionAppealCount, err = k.GovActionAppealSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	if err := k.BondedRoleConfigs.Walk(ctx, nil, func(_ int32, val types.BondedRoleConfig) (stop bool, err error) {
		genesis.BondedRoleConfigList = append(genesis.BondedRoleConfigList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.BondedRoles.Walk(ctx, nil, func(_ collections.Pair[int32, string], val types.BondedRole) (stop bool, err error) {
		genesis.BondedRoleList = append(genesis.BondedRoleList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}
