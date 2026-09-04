package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// ---------------------------------------------------------------------------
// C1 — the interim path was an unbounded DREAM self-mint
// ---------------------------------------------------------------------------

// TestInterim_CompletingTwiceDoesNotPayTwice is the core of C1.
//
// CompleteInterimDirectly paid every assignee and set COMPLETED, but never
// checked the interim was not already finalized — so re-sending
// MsgCompleteInterim against the same interim minted the budget again, and
// again, bounded only by max_dream_mint_per_epoch. ApproveInterim has always
// had this guard; the direct path did not.
func TestInterim_CompletingTwiceDoesNotPayTwice(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := f.ctx

	worker := newStakerMember(t, f, "interim_worker______", math.NewInt(1_000_000))

	interimID, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_OTHER,
		[]string{worker.String()}, "", 0, "test",
		types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, 0)
	require.NoError(t, err)

	before := *mustMember(t, f, worker).DreamBalance
	require.NoError(t, k.CompleteInterimDirectly(ctx, interimID, "done"))
	afterFirst := *mustMember(t, f, worker).DreamBalance
	paid := afterFirst.Sub(before)
	require.True(t, paid.IsPositive(), "precondition: the first completion must pay")

	// The second call must be refused outright, not paid again.
	err = k.CompleteInterimDirectly(ctx, interimID, "done again")
	require.ErrorIs(t, err, types.ErrInvalidInterimStatus)

	afterSecond := *mustMember(t, f, worker).DreamBalance
	require.Equal(t, afterFirst.String(), afterSecond.String(),
		"a finalized interim must not pay a second time")
}

// TestInterim_SeasonCapBoundsTotalEmission covers the economic half of C1.
//
// Interims are self-assigned by their creator and self-completed by their
// assignee, so max_active_interims_per_member only bounds how many are open at
// once — a member can complete and re-create indefinitely. Without a season cap
// this was the one DREAM-creating path limited solely by
// max_dream_mint_per_epoch (250,000 DREAM/day against a 25,000 genesis supply).
func TestInterim_SeasonCapBoundsTotalEmission(t *testing.T) {
	params := types.DefaultParams()
	// A cap that one standard interim clears but two do not.
	params.MaxInterimRewardsPerSeason = params.StandardComplexityBudget
	f := initFixture(t, WithCustomParams(params), WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := f.ctx

	worker := newStakerMember(t, f, "interim_capped______", math.NewInt(1_000_000))

	mkInterim := func() uint64 {
		id, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_OTHER,
			[]string{worker.String()}, "", 0, "test",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, 0)
		require.NoError(t, err)
		return id
	}

	first := mkInterim()
	second := mkInterim()

	require.NoError(t, k.CompleteInterimDirectly(ctx, first, "done"))
	balanceAtCap := *mustMember(t, f, worker).DreamBalance

	// The cap is now exhausted: the next completion must be refused rather than
	// minting past it.
	err := k.CompleteInterimDirectly(ctx, second, "done")
	require.ErrorIs(t, err, types.ErrInterimRewardCapReached)
	require.Equal(t, balanceAtCap.String(), (*mustMember(t, f, worker).DreamBalance).String(),
		"a refused completion must not pay")

	minted, err := k.GetSeasonInterimRewardsMinted(ctx)
	require.NoError(t, err)
	require.True(t, minted.LTE(params.MaxInterimRewardsPerSeason),
		"the counter must never exceed the cap it gates")
}

// ---------------------------------------------------------------------------
// C3 — the trust ladder was frozen at ESTABLISHED after season 0
// ---------------------------------------------------------------------------

// TestTrustLevel_SeasonsSinceJoiningUsesTheLiveSeason is C3.
//
// UpdateTrustLevel hardcoded `currentSeason := 0` as a placeholder long after
// x/season was wired, so the promotion gate `currentSeason >= JoinedSeason`
// failed permanently for every member who joined after season 0 — TRUSTED and
// CORE were unreachable for all of them, freezing the invitation-credit ladder
// and every tier gate above ESTABLISHED.
func TestTrustLevel_SeasonsSinceJoiningUsesTheLiveSeason(t *testing.T) {
	params := types.DefaultParams()
	cfg := params.TrustLevelConfig

	// A member who joined in season 1 and has since satisfied every
	// non-seasonal requirement for TRUSTED.
	joined := uint32(1)
	current := uint64(joined) + uint64(cfg.TrustedMinSeasons)

	f := initFixture(t, WithCustomParams(params), WithSeasonNumber(current))
	k := f.keeper
	ctx := f.ctx

	addr := sdk.AccAddress([]byte("trust_ladder_member_"))
	require.NoError(t, k.Member.Set(ctx, addr.String(), types.Member{
		Address:                addr.String(),
		Status:                 types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:           PtrInt(math.NewInt(1_000_000_000)),
		StakedDream:            PtrInt(math.ZeroInt()),
		LifetimeEarned:         PtrInt(math.ZeroInt()),
		LifetimeBurned:         PtrInt(math.ZeroInt()),
		TrustLevel:             types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		JoinedSeason:           joined,
		CompletedInterimsCount: cfg.EstablishedMinInterims + 50,
		ReputationScores: map[string]string{
			"backend": cfg.TrustedMinRep.Add(math.LegacyNewDec(100)).String(),
		},
	}))

	require.NoError(t, k.UpdateTrustLevel(ctx, addr))

	got := mustMember(t, f, addr).TrustLevel
	require.NotEqual(t, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, got,
		"a member who joined in season %d must be promotable by season %d", joined, current)
}

// ---------------------------------------------------------------------------
// M2 — a negative initiative budget inflated the project's budget headroom
// ---------------------------------------------------------------------------

func TestCreateInitiative_RejectsNegativeBudget(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "neg_budget_creator__", math.NewInt(5_000_000_000))
	projectID, err := k.CreateProject(ctx, creator, "P", "D", []string{"tag1"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(100_000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")),
		math.NewInt(100_000), math.NewInt(1000)))

	allocatedBefore := mustProjectAllocated(t, f, projectID)

	// Only the upper bound was checked. AllocateBudget tests `available <
	// amount`, never true for a negative, so allocated_budget SHRANK and the
	// project could commission more paid work than the committee approved.
	_, err = k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag1"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(-1_000_000))
	require.ErrorIs(t, err, types.ErrInvalidAmount)

	require.Equal(t, allocatedBefore.String(), mustProjectAllocated(t, f, projectID).String(),
		"a rejected initiative must not move the project's allocated budget")
}

func mustProjectAllocated(t *testing.T, f *fixture, projectID uint64) math.Int {
	t.Helper()
	p, err := f.keeper.GetProject(f.ctx, projectID)
	require.NoError(t, err)
	return keeper.DerefInt(p.AllocatedBudget)
}

// ---------------------------------------------------------------------------
// M5 — zeroing double-counted the burn and left zombie earning positions
// ---------------------------------------------------------------------------

// TestZeroMember_ReleasesStakesAndCountsBurnOnce covers both halves of M5.
//
// staked_dream is a subset of dream_balance, so `balance + staked` double-counted
// every staked coin. And zeroing never touched the member's Stake records:
// decayStakes kept shrinking them into a now-zero aggregate (driving the
// aggregates negative) while settleStake kept minting rewards to the zeroed
// member, who retained unbacked earning positions.
func TestZeroMember_ReleasesStakesAndCountsBurnOnce(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := f.ctx

	creator := newStakerMember(t, f, "zero_stake_creator__", math.NewInt(5_000_000_000))
	victim := newStakerMember(t, f, "zero_stake_victim___", math.NewInt(10_000_000))
	initID := newActiveInitiative(t, f, creator, "zerostake")

	stakeAmt := math.NewInt(1_000_000)
	stakeID, err := k.CreateStake(ctx, victim, types.StakeTargetType_STAKE_TARGET_INITIATIVE, initID, "", stakeAmt)
	require.NoError(t, err)

	balanceBefore := *mustMember(t, f, victim).DreamBalance
	poolBefore, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	burnedBefore, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)

	require.NoError(t, k.ZeroMember(ctx, victim, "test"))

	// The burn is the whole balance, counted exactly once.
	burnedAfter, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.Equal(t, balanceBefore.String(), burnedAfter.Sub(burnedBefore).String(),
		"the season burn must equal the balance, not balance + staked")

	m := mustMember(t, f, victim)
	require.Equal(t, balanceBefore.String(), m.LifetimeBurned.String(),
		"lifetime_burned must not double-count the staked subset")

	// The position is gone, and the pool denominator shrank with it.
	_, err = k.GetStake(ctx, stakeID)
	require.Error(t, err, "a zeroed member's stake records must be removed")

	poolAfter, err := k.GetSeasonalPoolTotalStaked(ctx)
	require.NoError(t, err)
	require.Equal(t, poolBefore.Sub(stakeAmt).String(), poolAfter.String(),
		"the seasonal denominator must shed the zeroed stake, not keep diluting live stakers")

	require.True(t, m.StakedDream.IsZero())
	require.True(t, m.DreamBalance.IsZero())
}

// ---------------------------------------------------------------------------
// M6 — the bonded-role demotion cooldown was escapable in one round trip
// ---------------------------------------------------------------------------

// TestBondedRole_CancelUnbondCannotLaunderADemotion is M6.
//
// MsgBondRole refuses to re-bond a DEMOTED role until DemotionCooldownUntil
// elapses, but nothing checked the cooldown on the way back from UNBONDING:
// CancelUnbondRole recomputed status with computeBondStatus, which derives it
// from bond size alone, saw a bond above min_bond, and wrote NORMAL. So
// unbond(token amount) -> cancel voided the demotion instantly.
func TestBondedRole_CancelUnbondCannotLaunderADemotion(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	ctx := f.ctx

	const roleType = types.RoleType_ROLE_TYPE_CONTENT_SENTINEL
	holder := newStakerMember(t, f, "demoted_role_holder_", math.NewInt(10_000_000_000))

	now := ctx.BlockTime().Unix()

	// A role whose bond is comfortably above min_bond but which is serving an
	// unexpired demotion cooldown — the state the exploit starts from.
	require.NoError(t, k.SetBondedRoleForTest(ctx,
		types.BondedRole{
			RoleType:              roleType,
			Address:               holder.String(),
			CurrentBond:           "5000000000",
			PendingUnbondAmount:   "10000000",
			UnbondCompletionTime:  now + 1000,
			BondStatus:            types.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING,
			DemotionCooldownUntil: now + 100_000,
		}))

	ms := keeper.NewMsgServerImpl(k)
	_, err := ms.CancelUnbondRole(ctx, &types.MsgCancelUnbondRole{
		Creator:  holder.String(),
		RoleType: roleType,
		Amount:   "10000000", // full cancel
	})
	require.NoError(t, err)

	br, err := k.GetBondedRole(ctx, roleType, holder.String())
	require.NoError(t, err)
	require.Equal(t, "0", br.PendingUnbondAmount, "precondition: the cancel must have gone through")
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED, br.BondStatus,
		"a cancelled unbond must not return a role to NORMAL while its demotion cooldown runs")
	require.Equal(t, now+100_000, br.DemotionCooldownUntil,
		"the cooldown itself must be untouched")
}

// ---------------------------------------------------------------------------
// C2 (index half) — four derived indexes were not rebuilt on import
// ---------------------------------------------------------------------------

// TestGenesisRebuildsStakeAndInvitationIndexes covers the two rebuilds with
// economic consequences.
//
// Projects, initiatives, challenges and jury reviews were rebuilt on import,
// with comments explaining why; stakes, interims, content challenges and
// invitations were not. The stake index is the worst of the four: conviction is
// recomputed from GetInitiativeStakes, which reads it, so after a restart every
// imported stake was invisible — initiatives could never reach their threshold
// and CompleteInitiative settled nothing, stranding the principal. The
// invitation index is what ProcessInviterAccountability resolves through, so an
// unrebuilt one silently disables the invitation slash as well as the
// duplicate-invitation guard.
func TestGenesisRebuildsStakeAndInvitationIndexes(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k, ctx := f.keeper, f.ctx

	creator := newStakerMember(t, f, "genesis_idx_creator_", math.NewInt(5_000_000_000))
	staker := newStakerMember(t, f, "genesis_idx_staker__", math.NewInt(5_000_000_000))
	initID := newActiveInitiative(t, f, creator, "genidx")

	_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		initID, "", math.NewInt(1_000_000))
	require.NoError(t, err)

	invitee := sdk.AccAddress([]byte("genesis_idx_invitee_"))
	invID, err := k.InvitationSeq.Next(ctx)
	require.NoError(t, err)
	require.NoError(t, k.Invitation.Set(ctx, invID, types.Invitation{
		Id:             invID,
		Inviter:        creator.String(),
		InviteeAddress: invitee.String(),
		StakedDream:    PtrInt(math.NewInt(100_000_000)),
		Status:         types.InvitationStatus_INVITATION_STATUS_ACCEPTED,
	}))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)

	f2 := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k2, ctx2 := f2.keeper, f2.ctx
	require.NoError(t, k2.InitGenesis(ctx2, *exported))

	// The stake must be reachable through the by-target index, not merely
	// present in the primary collection.
	stakes, err := k2.GetInitiativeStakes(ctx2, initID)
	require.NoError(t, err)
	require.Len(t, stakes, 1,
		"an imported stake must be visible to conviction, or its initiative can never complete")

	// And the invitee lookup must resolve.
	gotID, err := k2.InvitationsByInvitee.Get(ctx2, invitee.String())
	require.NoError(t, err, "the invitee index must be rebuilt on import")
	require.Equal(t, invID, gotID)
}

// ---------------------------------------------------------------------------
// C2 (export half) / M7 — the economic ledger was dropped on export
// ---------------------------------------------------------------------------

// TestGenesisRoundTripsEconomicLedger is C2's export half.
//
// x/rep keeps DREAM outside x/bank, so its treasury, season counters, seasonal
// pool and per-epoch mint allowance are recoverable from nowhere else. Export
// carried none of them, which made an import a different economy rather than a
// restored one: zero treasury, zero season counters (re-opening every
// per-season cap mid-season), a zero per-epoch mint allowance, and a decay
// clock that re-applied an extra epoch.
//
// It also made the "only seed an uninitialised pool" guard in InitGenesis dead
// code — SeasonalPoolRemaining always read zero on import, so every import
// refilled a whole season budget over whatever the exporting chain had left.
func TestGenesisRoundTripsEconomicLedger(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k, ctx := f.keeper, f.ctx

	// Stage a distinctive ledger: nothing here is derivable from other state.
	require.NoError(t, k.TreasuryBalance.Set(ctx, "777000000"))
	require.NoError(t, k.SeasonMinted.Set(ctx, "12345678"))
	require.NoError(t, k.SeasonBurned.Set(ctx, "2345678"))
	require.NoError(t, k.SeasonInitiativeRewardsMinted.Set(ctx, "3456789"))
	require.NoError(t, k.SeasonInterimRewardsMinted.Set(ctx, "4567890"))
	require.NoError(t, k.SeasonStakingRewardsMinted.Set(ctx, "567890"))
	require.NoError(t, k.SeasonalPoolRemaining.Set(ctx, "9000000000"))
	require.NoError(t, k.SeasonalPoolAccPerShare.Set(ctx, "0.123456789012345678"))
	require.NoError(t, k.SeasonalPoolSeason.Set(ctx, 4))
	require.NoError(t, k.SeasonalPoolStartEpoch.Set(ctx, 606))
	require.NoError(t, k.DecayLastProcessedEpoch.Set(ctx, 42))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.NotNil(t, exported.EconomicState, "the ledger must be exported at all")

	f2 := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k2, ctx2 := f2.keeper, f2.ctx
	require.NoError(t, k2.InitGenesis(ctx2, *exported))

	treasury, err := k2.GetTreasuryBalance(ctx2)
	require.NoError(t, err)
	require.Equal(t, "777000000", treasury.String(), "treasury must survive the round trip")

	minted, err := k2.GetSeasonMinted(ctx2)
	require.NoError(t, err)
	require.Equal(t, "12345678", minted.String(), "season counters gate the per-season caps")

	interim, err := k2.GetSeasonInterimRewardsMinted(ctx2)
	require.NoError(t, err)
	require.Equal(t, "4567890", interim.String())

	// The pool must keep its own budget, not be refilled to a fresh season.
	remaining, err := k2.GetSeasonalPoolRemaining(ctx2)
	require.NoError(t, err)
	require.Equal(t, "9000000000", remaining.String(),
		"an imported pool must keep its remaining budget, not be reseeded")

	acc, err := k2.GetSeasonalPoolAccPerShare(ctx2)
	require.NoError(t, err)
	require.Equal(t, "0.123456789012345678", acc.String(),
		"the monotonic accumulator must survive, or every stake's debt is measured against a reset")

	season, err := k2.SeasonalPoolSeason.Get(ctx2)
	require.NoError(t, err)
	require.Equal(t, uint64(4), season)

	decayEpoch, err := k2.DecayLastProcessedEpoch.Get(ctx2)
	require.NoError(t, err)
	require.Equal(t, uint64(42), decayEpoch,
		"a reset decay clock re-applies an extra epoch of decay after a restart")
}

// TestGenesisRoundTripsMemberAndTagPools is M7.
//
// RebaseStakeRewardDebt skips member and tag stakes on the stated grounds that
// their pools "ARE exported with the pool records". The genesis proto has
// carried fields 17-19 for those pools all along, but nothing populated them —
// so the accumulators reset to zero on import while the stakes kept debts taken
// against the old ones, and pending rewards clamped to zero until the fresh
// accumulator climbed back past a stale figure. The skip was correct reasoning
// on a false premise; this makes the premise true.
func TestGenesisRoundTripsMemberAndTagPools(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k, ctx := f.keeper, f.ctx

	member := sdk.AccAddress([]byte("pool_roundtrip_membr")).String()
	require.NoError(t, k.MemberStakePool.Set(ctx, member, types.MemberStakePool{
		Member:            member,
		TotalStaked:       math.NewInt(5_000_000),
		PendingRevenue:    math.NewInt(1_234),
		AccRewardPerShare: math.LegacyNewDecWithPrec(25, 3),
	}))
	require.NoError(t, k.TagStakePool.Set(ctx, "backend", types.TagStakePool{
		Tag:               "backend",
		TotalStaked:       math.NewInt(7_000_000),
		AccRewardPerShare: math.LegacyNewDecWithPrec(75, 3),
	}))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Len(t, exported.MemberStakePoolList, 1, "member pools must be exported")
	require.Len(t, exported.TagStakePoolList, 1, "tag pools must be exported")

	f2 := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k2, ctx2 := f2.keeper, f2.ctx
	require.NoError(t, k2.InitGenesis(ctx2, *exported))

	mp, err := k2.MemberStakePool.Get(ctx2, member)
	require.NoError(t, err)
	require.Equal(t, "0.025000000000000000", mp.AccRewardPerShare.String(),
		"the member accumulator must survive, or every member staker's pending clamps to zero")
	// TotalStaked is deliberately NOT asserted: ReconcileStakePoolTotals
	// recomputes every denominator from the live stake records on import, which
	// is the right owner for a derived value. The accumulator is the half that
	// cannot be recomputed from anything, which is exactly why it has to travel
	// in genesis.

	tp, err := k2.TagStakePool.Get(ctx2, "backend")
	require.NoError(t, err)
	require.Equal(t, "0.075000000000000000", tp.AccRewardPerShare.String())
}

// ---------------------------------------------------------------------------
// M4 — content-challenge jury reviews could deadlock permanently
// ---------------------------------------------------------------------------

// seedContentJurors adds enough members clearing min_juror_reputation for
// SelectContentJury to fill a panel.
func seedContentJurors(t *testing.T, f *fixture, n int) {
	t.Helper()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	rep := params.MinJurorReputation.Add(math.LegacyNewDec(10)).String()
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte("m4_juror_" + string(rune('a'+i)) + "___________"))
		require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.NewInt(1_000_000_000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
			Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
			ReputationScores: map[string]string{"general": rep},
		}))
	}
}

// TestContentJuryReview_TimesOutInsteadOfDeadlocking is M4.
//
// Content jury reviews are excluded from the shared PENDING verdict index, and
// nothing replaced the sweep that exclusion turned off. A jury that never voted
// left the review PENDING forever: the challenger's stake, the author's bond,
// and the target's one-challenge-at-a-time slot were locked permanently, with
// no code path able to reach them.
//
// The contract has two halves, and the second is what the e2e suite was
// implicitly relying on before: a live review must NOT be swept early, and an
// expired one MUST resolve — releasing the stake and freeing the target slot.
func TestContentJuryReview_TimesOutInsteadOfDeadlocking(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	k := f.keeper
	seedContentJurors(t, f, 6)

	stake := math.NewInt(100_000_000)
	ccID, err := k.CreateContentChallenge(f.ctx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1, "bad content", nil, stake)
	require.NoError(t, err)

	// A non-empty author response routes the challenge to a jury.
	require.NoError(t, k.RespondToContentChallenge(f.ctx, ccID, authorAddr, "I disagree", nil))

	cc, err := k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW, cc.Status)
	require.NotZero(t, cc.JuryReviewId)

	review, err := k.JuryReview.Get(f.ctx, cc.JuryReviewId)
	require.NoError(t, err)
	require.Greater(t, review.Deadline, int64(0), "the review must carry a deadline to sweep against")

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Half one: a review still inside its window is untouched. Sweeping early
	// would resolve an in-review challenge out from under its jurors, which is
	// exactly why these were excluded from the shared verdict index.
	require.NoError(t, k.SweepExpiredContentJuryReviews(f.ctx))
	cc, err = k.ContentChallenge.Get(f.ctx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_IN_JURY_REVIEW, cc.Status,
		"a live jury review must survive the sweep")

	// Half two: past the deadline it resolves, and everything it held is freed.
	agedCtx := sdkCtx.WithBlockHeight(review.Deadline + 1)
	require.NoError(t, k.SweepExpiredContentJuryReviews(agedCtx))

	cc, err = k.ContentChallenge.Get(agedCtx, ccID)
	require.NoError(t, err)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_REJECTED, cc.Status,
		"an expired jury review must resolve inconclusive, not sit PENDING forever")

	// The target's one-challenge slot is released, so the content can be
	// challenged again. Before the fix this slot was held permanently.
	_, err = k.CreateContentChallenge(agedCtx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1, "second look", nil, stake)
	require.NoError(t, err,
		"the target slot must be released when a jury review times out")
}

// TestContentChallenge_ResolversRejectAlreadyResolved covers the terminal-status
// guard the three content resolvers were missing. Every branch of each moves
// DREAM (burn, refund, or slash-and-reward), so a second call paid or burned a
// second time — and the timeout sweep above gives them one more caller.
func TestContentChallenge_ResolversRejectAlreadyResolved(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	k := f.keeper

	ccID, err := k.CreateContentChallenge(f.ctx, challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1, "bad content", nil,
		math.NewInt(100_000_000))
	require.NoError(t, err)

	require.NoError(t, k.ResolveInconclusiveContentChallenge(f.ctx, ccID))

	for name, fn := range map[string]func() error{
		"uphold":       func() error { return k.UpholdContentChallenge(f.ctx, ccID) },
		"reject":       func() error { return k.RejectContentChallenge(f.ctx, ccID) },
		"inconclusive": func() error { return k.ResolveInconclusiveContentChallenge(f.ctx, ccID) },
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, fn(), types.ErrContentChallengeNotActive,
				"a resolved content challenge must not resolve again")
		})
	}
}
