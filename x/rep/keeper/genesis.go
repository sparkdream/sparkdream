package keeper

import (
	"context"
	"errors"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		// Rebuild the invitee lookup. Without it the duplicate-invitation guard
		// fails open after a restart, and ProcessInviterAccountability — which
		// resolves the invitation through this index — can never find the
		// invitation that makes an inviter accountable for their invitee.
		//
		// Only live invitations hold the invitee slot: PENDING (reserved) and
		// ACCEPTED (the accountability link). A closed invitation released it,
		// and rebuilding it would leave that address uninvitable after import.
		live := elem.Status == types.InvitationStatus_INVITATION_STATUS_PENDING ||
			elem.Status == types.InvitationStatus_INVITATION_STATUS_ACCEPTED
		if elem.InviteeAddress != "" && live {
			if err := k.InvitationsByInvitee.Set(ctx, elem.InviteeAddress, elem.Id); err != nil {
				return err
			}
		}
		// PENDING invitations by creation time, for the expiry sweep.
		if elem.Status == types.InvitationStatus_INVITATION_STATUS_PENDING {
			if err := k.addPendingInvitation(ctx, elem); err != nil {
				return err
			}
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
		// Rebuild the by-target index. This is the most consequential of the
		// four: conviction is recomputed from GetInitiativeStakes, which reads
		// this index, so an unrebuilt one makes every imported stake invisible
		// — initiatives can never reach their conviction threshold and
		// CompleteInitiative settles nothing, stranding the principal.
		if err := k.AddStakeToTargetIndex(ctx, elem); err != nil {
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
	// Content-challenge and appeal reviews stay out of the verdict index (the
	// deadline sweep must not resolve them); the export lists them.
	outsideVerdictIndex := make(map[uint64]struct{}, len(genState.JuryReviewsOutsideVerdictIndex))
	for _, id := range genState.JuryReviewsOutsideVerdictIndex {
		outsideVerdictIndex[id] = struct{}{}
	}
	for _, elem := range genState.JuryReviewList {
		if err := k.JuryReview.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
		if _, outside := outsideVerdictIndex[elem.Id]; !outside {
			if err := k.AddJuryReviewToVerdictIndex(ctx, elem); err != nil {
				return err
			}
		}
		// Rebuild the by-juror index too, so a juror's client can still find
		// their outstanding summons across a restart.
		if err := k.AddJuryReviewToJurorIndex(ctx, elem); err != nil {
			return err
		}
		// Seats opened by another module's EndBlocker after x/rep's draw wait
		// for the next block's draw; rebuild that queue from the reviews
		// (block-hash seed), then let pending_jury_draw_list restore the
		// beacon epoch of each draw it names.
		if elem.PendingSeats > 0 && elem.Verdict == types.Verdict_VERDICT_PENDING {
			if err := k.PendingJuryDraws.Set(ctx, elem.Id, types.PendingJuryDraw{JuryReviewId: elem.Id}); err != nil {
				return err
			}
		}
	}
	for _, draw := range genState.PendingJuryDrawList {
		queued, err := k.PendingJuryDraws.Has(ctx, draw.JuryReviewId)
		if err != nil {
			return err
		}
		if !queued {
			// A draw for a review with no pending seats would seat nobody;
			// seatPendingJury returns at PendingSeats == 0 anyway.
			continue
		}
		// Heights and epochs ahead of this chain are clamped so the draw
		// cannot wait for a height the exporting chain had reached (REP-R-206).
		if err := k.PendingJuryDraws.Set(ctx, draw.JuryReviewId, k.clampImportedJuryDraw(ctx, draw)); err != nil {
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
		// Rebuild the by-status index; the EndBlocker's interim expiry sweep
		// walks it, so without this no imported interim ever expires.
		if err := k.AddInterimToStatusIndex(ctx, elem); err != nil {
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
		// Rebuild both derived indexes: the status index the unanswered-challenge
		// sweep walks, and the per-target index that enforces one live challenge
		// per content item (which otherwise fails open after a restart).
		if err := k.AddContentChallengeToStatusIndex(ctx, elem); err != nil {
			return err
		}
		if isLiveContentChallenge(elem.Status) {
			if err := k.ContentChallengesByTarget.Set(ctx,
				collections.Join(int32(elem.TargetType), elem.TargetId), elem.Id); err != nil {
				return err
			}
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
	// Registry size counter (REP-E-09), derived from the imported tags.
	if err := k.TagCount.Set(ctx, uint64(len(genState.TagMap))); err != nil {
		return err
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

	// The per-(budget, post) award cooldown (REP-E-15) is exported with its
	// heights. An award the genesis gives no cooldown entry (a hand-written
	// genesis: the award records carry a timestamp, not a height) restarts
	// its cooldown at the import height: that can delay an award, never allow
	// a repeated one.
	// The per-awarder epoch spend ledger keeps each awarder's epoch cap
	// across an import.
	for _, e := range genState.TagBudgetAwarderEpochList {
		if err := k.TagBudgetAwarderEpoch.Set(ctx, collections.Join3(e.BudgetId, e.Awarder, e.Epoch), e.Amount); err != nil {
			return err
		}
	}
	for _, c := range genState.TagBudgetAwardCooldownList {
		if err := k.TagBudgetAwardByPost.Set(ctx, collections.Join(c.BudgetId, c.PostId), c.Height); err != nil {
			return err
		}
	}
	importHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	for _, elem := range genState.TagBudgetAwardList {
		if err := k.TagBudgetAward.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
		key := collections.Join(elem.BudgetId, elem.PostId)
		has, err := k.TagBudgetAwardByPost.Has(ctx, key)
		if err != nil {
			return err
		}
		if !has {
			if err := k.TagBudgetAwardByPost.Set(ctx, key, importHeight); err != nil {
				return err
			}
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
	// The filing-window expiry index is derived (setMemberActionRestoration
	// writes it for records with no appeal filed).
	for _, elem := range genState.MemberActionRestorationList {
		if err := k.setMemberActionRestoration(ctx, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.GovActionAppealList {
		if err := k.SetGovActionAppeal(ctx, elem); err != nil {
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
		if err := k.setBondedRoleAt(ctx, collections.Join(int32(br.RoleType), br.Address), br); err != nil {
			return err
		}
	}
	for _, ra := range genState.RoleActivityList {
		if err := k.RoleActivities.Set(ctx, collections.Join(int32(ra.RoleType), ra.Address), ra); err != nil {
			return err
		}
	}
	// EscalatedReviews is the only marker for "already with the committee" —
	// ReviewEscalation is reset to NONE when a round escalates — so an import
	// that drops it re-escalates every open round and extends its deadline
	// again, and leaves silent escalations with nothing to resolve them.
	for _, id := range genState.EscalatedReviewList {
		if err := k.EscalatedReviews.Set(ctx, id); err != nil {
			return err
		}
	}
	// Without the day ledger an import hands the chain a fresh daily allowance,
	// so role_reward_daily_funding would bound a day only until the next export.
	for _, b := range genState.ReviewBountyList {
		if err := k.ReviewBounty.Set(ctx, b.InitiativeId, b); err != nil {
			return err
		}
	}
	for _, b := range genState.InitiativeBountyList {
		if err := k.InitiativeBounty.Set(ctx, b.InitiativeId, b); err != nil {
			return err
		}
	}
	for _, df := range genState.RoleRewardDayFundingList {
		amount := df.AmountFunded
		if amount.IsNil() {
			amount = math.ZeroInt()
		}
		if err := k.RoleRewardDayFunding.Set(ctx, df.Day, amount.String()); err != nil {
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

	// Write the reviewer bonded-role policy through from params, after the
	// BondedRoleConfigList loop above so params win over whatever the genesis
	// file seeded. x/forum and x/collect do the same for their own roles from
	// their InitGenesis; the reviewer's owner is rep itself.
	if err := k.SyncReviewerBondedRoleConfig(ctx, genState.Params); err != nil {
		return err
	}

	// Restore the module's own token ledger BEFORE the pool-seeding guard
	// below, which reads SeasonalPoolRemaining to decide whether this is a
	// fresh chain or a restored one. Nothing populated the ledger on export
	// until now, so that guard could never see a restored pool: it read zero on
	// every import and refilled a whole season budget over the top of whatever
	// the exporting chain had left, resetting the caps mid-season.
	poolRestored, err := k.importEconomicState(ctx, genState.EconomicState)
	if err != nil {
		return err
	}

	// The four stake pools. These carry the member/tag accumulators that
	// RebaseStakeRewardDebt deliberately does NOT rebase, on the stated grounds
	// that the pools are exported — which only became true once the export
	// above was written. Importing them is what makes that skip correct rather
	// than a silent forfeiture of every member and tag staker's pending
	// rewards.
	for _, pool := range genState.MemberStakePoolList {
		if err := k.MemberStakePool.Set(ctx, pool.Member, pool); err != nil {
			return err
		}
	}
	for _, pool := range genState.TagStakePoolList {
		if err := k.TagStakePool.Set(ctx, pool.Tag, pool); err != nil {
			return err
		}
	}
	for _, info := range genState.ProjectStakeInfoList {
		if err := k.ProjectStakeInfo.Set(ctx, info.ProjectId, info); err != nil {
			return err
		}
	}

	// Per-recipient gift cooldowns.
	for _, g := range genState.GiftRecordList {
		if g.Record == nil {
			continue
		}
		if err := k.GiftRecord.Set(ctx, collections.Join(g.Sender, g.Recipient), *g.Record); err != nil {
			return err
		}
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
	if poolRestored {
		// An imported pool keeps its own budget and drain anchor.
		remaining = math.OneInt()
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
	if err := k.ReconcileStakePoolTotals(ctx); err != nil {
		return err
	}

	// The economic state exports the seasonal accumulator, so an import
	// restores it and every stake's reward_debt stays exact: pending rewards
	// survive the round trip. Only a genesis without an accumulator (older
	// exports) needs the debts re-measured against the reset accumulator; see
	// RebaseStakeRewardDebt for what that costs in each direction.
	if genState.EconomicState == nil || genState.EconomicState.SeasonalPoolAccPerShare == "" {
		rebased, err := k.RebaseStakeRewardDebt(ctx)
		if err != nil {
			return err
		}
		if rebased > 0 {
			sdk.UnwrapSDKContext(ctx).Logger().Info(
				"rebased stake reward debt against the imported seasonal accumulator",
				"stakes", rebased)
		}
	}
	return k.initMigrationGenesis(ctx, genState)
}

// ExportGenesis returns the module's exported genesis.
// initMigrationGenesis imports the member key migration state and rebuilds
// its derived indexes (reverse alias index, pending pointers, queues).
func (k Keeper) initMigrationGenesis(ctx context.Context, genState types.GenesisState) error {
	params := genState.Params
	for _, t := range genState.TombstoneList {
		if err := k.Tombstones.Set(ctx, t.Address, t); err != nil {
			return err
		}
		if err := k.MigratedFrom.Set(ctx, collections.Join3(t.Canonical, t.FinalizedEpoch, t.Address)); err != nil {
			return err
		}
	}
	for _, m := range genState.MemberMigrationList {
		if err := k.MemberMigrations.Set(ctx, m.Id, m); err != nil {
			return err
		}
		if !types.IsPendingMigrationStatus(m.Status) {
			continue
		}
		if err := k.PendingMigrationByMember.Set(ctx, m.FromAddress, m.Id); err != nil {
			return err
		}
		if m.Status == types.MigrationStatus_MIGRATION_STATUS_REQUESTED {
			if err := k.MigrationRequestsByTarget.Set(ctx, collections.Join(m.ToAddress, m.Id)); err != nil {
				return err
			}
		} else if err := k.PendingMigrationByTarget.Set(ctx, m.ToAddress, m.Id); err != nil {
			return err
		}
		switch m.Status {
		case types.MigrationStatus_MIGRATION_STATUS_REQUESTED:
			if err := k.MigrationAcceptExpiry.Set(ctx, collections.Join(acceptBy(m, params), m.Id)); err != nil {
				return err
			}
		case types.MigrationStatus_MIGRATION_STATUS_ACCEPTED:
			if err := k.MigrationQueue.Set(ctx, collections.Join(m.ExecutesAt, m.Id)); err != nil {
				return err
			}
		case types.MigrationStatus_MIGRATION_STATUS_OBJECTED:
			if err := k.ObjectedMigrations.Set(ctx, m.Id); err != nil {
				return err
			}
		}
	}
	if err := k.MemberMigrationSeq.Set(ctx, genState.MemberMigrationCount); err != nil {
		return err
	}
	for _, e := range genState.RecoveryCommitmentList {
		if err := k.RecoveryCommitments.Set(ctx, e.Member, e.Set); err != nil {
			return err
		}
	}
	for _, c := range genState.RecoveryCommitList {
		if err := k.RecoveryCommits.Set(ctx, c.Sealed, c); err != nil {
			return err
		}
		if err := k.RecoveryCommitExpiry.Set(ctx, collections.Join(c.ExpiresAt, c.Sealed)); err != nil {
			return err
		}
	}
	for _, m := range genState.MemberMap {
		if len(m.PendingZkPublicKey) > 0 {
			if err := k.ZkKeyActivationQueue.Set(ctx, collections.Join(m.ZkKeyActivatesAtEpoch, m.Address)); err != nil {
				return err
			}
		}
	}
	return nil
}

// exportMigrationGenesis exports the member key migration state.
func (k Keeper) exportMigrationGenesis(ctx context.Context, genesis *types.GenesisState) error {
	if err := k.Tombstones.Walk(ctx, nil, func(_ string, t types.Tombstone) (bool, error) {
		genesis.TombstoneList = append(genesis.TombstoneList, t)
		return false, nil
	}); err != nil {
		return err
	}
	if err := k.MemberMigrations.Walk(ctx, nil, func(_ uint64, m types.MemberMigration) (bool, error) {
		genesis.MemberMigrationList = append(genesis.MemberMigrationList, m)
		return false, nil
	}); err != nil {
		return err
	}
	count, err := k.MemberMigrationSeq.Peek(ctx)
	if err != nil {
		return err
	}
	genesis.MemberMigrationCount = count
	if err := k.RecoveryCommitments.Walk(ctx, nil, func(member string, set types.RecoveryCommitmentSet) (bool, error) {
		genesis.RecoveryCommitmentList = append(genesis.RecoveryCommitmentList, types.RecoveryCommitmentEntry{Member: member, Set: set})
		return false, nil
	}); err != nil {
		return err
	}
	return k.RecoveryCommits.Walk(ctx, nil, func(_ []byte, c types.RecoveryCommit) (bool, error) {
		genesis.RecoveryCommitList = append(genesis.RecoveryCommitList, c)
		return false, nil
	})
}

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
		indexed, err := k.JuryReviewsByVerdict.Has(ctx, collections.Join(int32(elem.Verdict), elem.Id))
		if err != nil {
			return true, err
		}
		if !indexed {
			genesis.JuryReviewsOutsideVerdictIndex = append(genesis.JuryReviewsOutsideVerdictIndex, elem.Id)
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.JuryReviewCount, err = k.JuryReviewSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.PendingJuryDraws.Walk(ctx, nil, func(_ uint64, draw types.PendingJuryDraw) (bool, error) {
		genesis.PendingJuryDrawList = append(genesis.PendingJuryDrawList, draw)
		return false, nil
	}); err != nil {
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
	err = k.EscalatedReviews.Walk(ctx, nil, func(id uint64) (bool, error) {
		genesis.EscalatedReviewList = append(genesis.EscalatedReviewList, id)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.ReviewBounty.Walk(ctx, nil, func(_ uint64, b types.ReviewBounty) (bool, error) {
		genesis.ReviewBountyList = append(genesis.ReviewBountyList, b)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.InitiativeBounty.Walk(ctx, nil, func(_ uint64, b types.InitiativeBounty) (bool, error) {
		genesis.InitiativeBountyList = append(genesis.InitiativeBountyList, b)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.RoleRewardDayFunding.Walk(ctx, nil, func(day uint64, raw string) (bool, error) {
		amount, ok := math.NewIntFromString(raw)
		if !ok {
			amount = math.ZeroInt()
		}
		genesis.RoleRewardDayFundingList = append(genesis.RoleRewardDayFundingList,
			types.RoleRewardDayFunding{Day: day, AmountFunded: amount})
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
	// DefaultGenesis seeds the reserved-tag defaults; the export is the live
	// set only, or a removed default would come back (and a kept one would be
	// listed twice) on import.
	genesis.ReservedTagMap = nil
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
	if err := k.TagBudgetAwardByPost.Walk(ctx, nil, func(key collections.Pair[uint64, uint64], height int64) (bool, error) {
		genesis.TagBudgetAwardCooldownList = append(genesis.TagBudgetAwardCooldownList,
			types.TagBudgetAwardCooldown{BudgetId: key.K1(), PostId: key.K2(), Height: height})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TagBudgetAwarderEpoch.Walk(ctx, nil, func(key collections.Triple[uint64, string, int64], amt math.Int) (bool, error) {
		genesis.TagBudgetAwarderEpochList = append(genesis.TagBudgetAwarderEpochList,
			types.TagBudgetAwarderEpochSpend{BudgetId: key.K1(), Awarder: key.K2(), Epoch: key.K3(), Amount: amt})
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
	if err := k.MemberActionRestorations.Walk(ctx, nil, func(_ string, val types.MemberActionRestoration) (stop bool, err error) {
		genesis.MemberActionRestorationList = append(genesis.MemberActionRestorationList, val)
		return false, nil
	}); err != nil {
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

	// The four stake pools. The proto has carried fields 17-19 for these all
	// along, but nothing ever populated them — and two comments elsewhere
	// asserted they "are exported verbatim", which RebaseStakeRewardDebt relied
	// on when it decided to skip member and tag stakes. It was skipping them on
	// a false premise: their accumulators reset to zero on import while the
	// stakes kept debts taken against the old ones, so pending rewards clamped
	// to zero until the fresh accumulator climbed back past a stale figure.
	if err := k.MemberStakePool.Walk(ctx, nil, func(_ string, val types.MemberStakePool) (stop bool, err error) {
		genesis.MemberStakePoolList = append(genesis.MemberStakePoolList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.TagStakePool.Walk(ctx, nil, func(_ string, val types.TagStakePool) (stop bool, err error) {
		genesis.TagStakePoolList = append(genesis.TagStakePoolList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ProjectStakeInfo.Walk(ctx, nil, func(_ uint64, val types.ProjectStakeInfo) (stop bool, err error) {
		genesis.ProjectStakeInfoList = append(genesis.ProjectStakeInfoList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// Per-recipient gift cooldowns; without these a capped gift re-enables
	// itself across a restart.
	if err := k.GiftRecord.Walk(ctx, nil, func(key collections.Pair[string, string], val types.GiftRecord) (stop bool, err error) {
		genesis.GiftRecordList = append(genesis.GiftRecordList, types.GiftRecordEntry{
			Sender:    key.K1(),
			Recipient: key.K2(),
			Record:    &val,
		})
		return false, nil
	}); err != nil {
		return nil, err
	}

	econ, err := k.exportEconomicState(ctx)
	if err != nil {
		return nil, err
	}
	genesis.EconomicState = econ

	if err := k.exportMigrationGenesis(ctx, genesis); err != nil {
		return nil, err
	}
	return genesis, nil
}

// isLiveContentChallenge reports whether a content challenge still occupies its
// target's one-challenge-at-a-time slot. Only unresolved challenges hold it;
// CreateContentChallenge frees the slot on every terminal transition.
func isLiveContentChallenge(status types.ContentChallengeStatus) bool {
	switch status {
	case types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE,
		types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Economic ledger round trip
// ---------------------------------------------------------------------------

// itemOrEmpty reads a string Item, treating "not set" as the empty string so an
// export of a chain that has never touched a counter carries "" rather than
// failing. importIntItem turns "" back into zero.
func itemOrEmpty(ctx context.Context, item collections.Item[string]) (string, error) {
	v, err := item.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

func uint64ItemOrZero(ctx context.Context, item collections.Item[uint64]) (uint64, error) {
	v, err := item.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return v, nil
}

// exportEconomicState snapshots x/rep's own token ledger.
//
// DREAM lives on member records and module-level accumulators rather than in
// x/bank, so nothing here is recoverable from another module's genesis. An
// export without it is not a partial export, it is a different economy: zero
// treasury, zero season counters (which re-opens every per-season cap
// mid-season), and a zero seasonal pool, which additionally made the
// "only seed an uninitialised pool" guard in InitGenesis dead code — the pool
// always read as uninitialised, so every import refilled a full season budget.
func (k Keeper) exportEconomicState(ctx context.Context) (*types.EconomicState, error) {
	econ := &types.EconomicState{}
	var err error

	for _, f := range []struct {
		dst  *string
		item collections.Item[string]
	}{
		{&econ.TreasuryBalance, k.TreasuryBalance},
		{&econ.SeasonMinted, k.SeasonMinted},
		{&econ.SeasonBurned, k.SeasonBurned},
		{&econ.SeasonInitiativeRewardsMinted, k.SeasonInitiativeRewardsMinted},
		{&econ.SeasonStakingRewardsMinted, k.SeasonStakingRewardsMinted},
		{&econ.SeasonInterimRewardsMinted, k.SeasonInterimRewardsMinted},
		{&econ.SeasonTreasuryInflow, k.SeasonTreasuryInflow},
		{&econ.SeasonTreasuryOutflow, k.SeasonTreasuryOutflow},
		{&econ.SeasonalPoolRemaining, k.SeasonalPoolRemaining},
		{&econ.SeasonalPoolAccPerShare, k.SeasonalPoolAccPerShare},
		{&econ.EpochMintedAmount, k.EpochMintedAmount},
	} {
		if *f.dst, err = itemOrEmpty(ctx, f.item); err != nil {
			return nil, err
		}
	}

	for _, f := range []struct {
		dst  *uint64
		item collections.Item[uint64]
	}{
		{&econ.SeasonalPoolSeason, k.SeasonalPoolSeason},
		{&econ.SeasonalPoolStartEpoch, k.SeasonalPoolStartEpoch},
		{&econ.EpochMintedEpoch, k.EpochMintedEpoch},
		{&econ.DecayLastProcessedEpoch, k.DecayLastProcessedEpoch},
	} {
		if *f.dst, err = uint64ItemOrZero(ctx, f.item); err != nil {
			return nil, err
		}
	}

	// The decay pass in flight (REP-E-108), so an import resumes it instead
	// of re-decaying what it already visited.
	if econ.DecayPassEpoch, err = uint64ItemOrZero(ctx, k.DecayPassEpoch); err != nil {
		return nil, err
	}
	if econ.DecayPassEpoch != 0 {
		if econ.DecayPassMemberCursor, err = itemOrEmpty(ctx, k.DecayPassMemberCursor); err != nil {
			return nil, err
		}
		if econ.DecayPassStakePhase, err = k.DecayPassStakeCursor.Has(ctx); err != nil {
			return nil, err
		}
		if econ.DecayPassStakePhase {
			if econ.DecayPassStakeCursor, err = k.DecayPassStakeCursor.Get(ctx); err != nil {
				return nil, err
			}
		}
	}

	return econ, nil
}

// importEconomicState restores the ledger. Absent (nil) economic state is
// tolerated so a hand-written genesis stays valid: every field then keeps the
// zero value InitGenesis would otherwise have seeded.
//
// Returns whether the seasonal pool was restored, so InitGenesis knows not to
// seed a fresh season budget over the top of an imported one.
func (k Keeper) importEconomicState(ctx context.Context, econ *types.EconomicState) (bool, error) {
	if econ == nil {
		return false, nil
	}

	setStr := func(item collections.Item[string], v string) error {
		if v == "" {
			return nil // leave unset; readers treat missing as zero
		}
		return item.Set(ctx, v)
	}

	for _, f := range []struct {
		item collections.Item[string]
		val  string
	}{
		{k.TreasuryBalance, econ.TreasuryBalance},
		{k.SeasonMinted, econ.SeasonMinted},
		{k.SeasonBurned, econ.SeasonBurned},
		{k.SeasonInitiativeRewardsMinted, econ.SeasonInitiativeRewardsMinted},
		{k.SeasonStakingRewardsMinted, econ.SeasonStakingRewardsMinted},
		{k.SeasonInterimRewardsMinted, econ.SeasonInterimRewardsMinted},
		{k.SeasonTreasuryInflow, econ.SeasonTreasuryInflow},
		{k.SeasonTreasuryOutflow, econ.SeasonTreasuryOutflow},
		{k.SeasonalPoolRemaining, econ.SeasonalPoolRemaining},
		{k.SeasonalPoolAccPerShare, econ.SeasonalPoolAccPerShare},
		{k.EpochMintedAmount, econ.EpochMintedAmount},
	} {
		if err := setStr(f.item, f.val); err != nil {
			return false, err
		}
	}

	// A zero epoch is stored when it is written together with its partner
	// (InitSeasonalPool sets the pool season and start epoch with the pool
	// remaining; the epoch mint ceiling sets its epoch with the amount), so a
	// pool opened, or a mint made, in epoch 0 round-trips exactly.
	poolSet := econ.SeasonalPoolRemaining != ""
	for _, f := range []struct {
		item   collections.Item[uint64]
		val    uint64
		paired bool
	}{
		{k.SeasonalPoolSeason, econ.SeasonalPoolSeason, poolSet},
		{k.SeasonalPoolStartEpoch, econ.SeasonalPoolStartEpoch, poolSet},
		{k.EpochMintedEpoch, econ.EpochMintedEpoch, econ.EpochMintedAmount != ""},
		{k.DecayLastProcessedEpoch, econ.DecayLastProcessedEpoch, false},
	} {
		if f.val == 0 && !f.paired {
			continue
		}
		if err := f.item.Set(ctx, f.val); err != nil {
			return false, err
		}
	}

	// The decay pass in flight. Its cursors mean nothing without its epoch.
	if econ.DecayPassEpoch != 0 {
		if err := k.DecayPassEpoch.Set(ctx, econ.DecayPassEpoch); err != nil {
			return false, err
		}
		if econ.DecayPassMemberCursor != "" {
			if err := k.DecayPassMemberCursor.Set(ctx, econ.DecayPassMemberCursor); err != nil {
				return false, err
			}
		}
		if econ.DecayPassStakePhase {
			if err := k.DecayPassStakeCursor.Set(ctx, econ.DecayPassStakeCursor); err != nil {
				return false, err
			}
		}
	}

	// A pool is "restored" only if it carries a remaining budget; an exported
	// but exhausted pool must not block the incoming season from being seeded.
	return econ.SeasonalPoolRemaining != "" && econ.SeasonalPoolRemaining != "0", nil
}
