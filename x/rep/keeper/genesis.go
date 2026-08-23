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
		// Rebuild the by-status index from the project list. The index is
		// derived state (not separately exported in genesis), so importing the
		// primary collection alone would leave the EndBlocker expiry sweep
		// blind to existing PROPOSED projects.
		if err := k.AddProjectToStatusIndex(ctx, elem); err != nil {
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
		// Derived state, not separately exported — rebuild it or the EndBlocker
		// completion and challenge-period sweeps go blind after a restart.
		if err := k.AddInitiativeToStatusIndex(ctx, elem); err != nil {
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
		// Critical: HasActiveChallenges reads this index, and CanCompleteInitiative
		// reads that. An unpopulated index reports "no active challenges", which
		// would let a challenged initiative pay out after a genesis restart.
		if err := k.AddChallengeToStatusIndex(ctx, elem); err != nil {
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
		if err := k.AddJuryReviewToVerdictIndex(ctx, elem); err != nil {
			return err
		}
		// Rebuild the by-juror index too, so a juror's client can still find
		// their outstanding summons across a restart.
		if err := k.AddJuryReviewToJurorIndex(ctx, elem); err != nil {
			return err
		}
	}

	if err := k.JuryReviewSeq.Set(ctx, genState.JuryReviewCount); err != nil {
		return err
	}
	for _, elem := range genState.InitiativeReviewList {
		if err := k.InitiativeReview.Set(ctx,
			collections.Join3(elem.InitiativeId, elem.Round, elem.Reviewer), elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.InterimList {
		if err := k.Interim.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.InterimSeq.Set(ctx, genState.InterimCount); err != nil {
		return err
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

	// Content initiative links, plus the InitiativesByContent reverse index.
	// The reverse index is derived state and is not exported, so it has to be
	// rebuilt here or content stakes would stop reaching the initiatives they
	// propagate into.
	for _, link := range genState.ContentInitiativeLinks {
		key := collections.Join(link.InitiativeId, collections.Join(link.TargetType, link.TargetId))
		if err := k.ContentInitiativeLinks.Set(ctx, key); err != nil {
			return err
		}
		reverseKey := collections.Join(collections.Join(link.TargetType, link.TargetId), link.InitiativeId)
		if err := k.InitiativesByContent.Set(ctx, reverseKey); err != nil {
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
		// Validate numeric string fields at genesis time so downstream reads can
		// trust that parseIntOrZero will not surface corruption mid-block.
		_ = mustParseIntOrZero(cfg.MinBond)
		_ = mustParseIntOrZero(cfg.DemotionThreshold)
		if err := k.BondedRoleConfigs.Set(ctx, int32(cfg.RoleType), cfg); err != nil {
			return err
		}
	}
	for _, br := range genState.BondedRoleList {
		_ = mustParseIntOrZero(br.CurrentBond)
		_ = mustParseIntOrZero(br.TotalCommittedBond)
		_ = mustParseIntOrZero(br.CumulativeRewards)
		if err := k.BondedRoles.Set(ctx, collections.Join(int32(br.RoleType), br.Address), br); err != nil {
			return err
		}
	}
	for _, ra := range genState.RoleActivityList {
		if err := k.RoleActivities.Set(ctx, collections.Join(int32(ra.RoleType), ra.Address), ra); err != nil {
			return err
		}
	}

	// If there are members, trigger a full trust tree rebuild on next EndBlock.
	// The tree is derived state (not exported in genesis) and will be populated
	// from member records + voter registrations.
	if len(genState.MemberMap) > 0 {
		k.MarkTrustTreeDirty(ctx)
	}

	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Seed the seasonal staking reward pool. Without this the pool's remaining
	// budget stays unset, DistributeEpochStakingRewardsFromPool returns early
	// every epoch, and the accumulator never leaves zero — which is why no
	// initiative or project staker could earn from it. Params must already be
	// set: InitSeasonalPool reads MaxStakingRewardsPerSeason.
	//
	// Only seeds an uninitialised pool, so re-importing an exported chain does
	// not silently refill a season's budget or reset its economic counters.
	remaining, err := k.getSeasonalPoolRemaining(ctx)
	if err != nil {
		return err
	}
	if remaining.IsZero() {
		if err := k.InitSeasonalPool(ctx, 1); err != nil {
			return err
		}
	}

	// Arm the conviction queue for every imported initiative that can still
	// accrue. The queue is derived state and is not exported, so without this an
	// imported chain would never refresh conviction until the next stake
	// mutation.
	if err := k.RearmConvictionQueue(ctx); err != nil {
		return err
	}

	// Recompute every staked denominator from the imported stakes. The pool
	// records are exported verbatim, so this both rebuilds SeasonalPoolTotalStaked
	// (which is derived state and not exported at all) and heals member, tag and
	// project totals written before the decrement paths existed.
	return k.ReconcileStakePoolTotals(ctx)
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
	err = k.InitiativeReview.Walk(ctx, nil, func(_ collections.Triple[uint64, uint32, string], elem types.InitiativeReview) (bool, error) {
		genesis.InitiativeReviewList = append(genesis.InitiativeReviewList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.InterimCount, err = k.InterimSeq.Peek(ctx)
	if err != nil {
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

	// Reset the default-seeded list before walking — DefaultGenesis()
	// pre-populates BondedRoleConfigList with seed entries so a freshly-init'd
	// chain boots coherently, but ExportGenesis is the source of truth for the
	// current live state. Appending without resetting would duplicate every
	// seeded role on every export/import roundtrip.
	genesis.BondedRoleConfigList = nil
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
	if err := k.RoleActivities.Walk(ctx, nil, func(_ collections.Pair[int32, string], val types.RoleActivity) (stop bool, err error) {
		genesis.RoleActivityList = append(genesis.RoleActivityList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}
