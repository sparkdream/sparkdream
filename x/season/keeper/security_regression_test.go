package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// Regression tests for the Part 7 security review findings on x/season
// (SEA-01 .. SEA-19).

// retroSetup puts season 1 into the RETRO_REWARDS phase with n nominations,
// each carrying one mature stake of `stake` micro-DREAM.
func retroSetup(t *testing.T, f *beginBlockFixture, n int, stake int64) []string {
	t.Helper()
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{
		Number: 1, Name: "Phoenix", EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING,
	}))
	addrs := make([]string, n)
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("aurora%d_________", i))).String()
		addrs[i] = addr
		f.repKeeper.SetMember(addr, 0, nil, 0)
		id := uint64(i + 1)
		require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{
			Id: id, Nominator: addr, ContentRef: fmt.Sprintf("blog/post/%d", id), Season: 1,
			TotalStaked: math.LegacyNewDec(stake), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
		}))
		staker := sdk.AccAddress([]byte(fmt.Sprintf("zenith%d_________", i))).String()
		require.NoError(t, f.keeper.NominationStake.Set(f.ctx, fmt.Sprintf("%d/%s", id, staker), types.NominationStake{
			NominationId: id, Staker: staker, Amount: math.LegacyNewDec(stake), StakedAtBlock: 0,
		}))
	}
	f.ctx = f.ctx.WithBlockHeight(200000) // all stakes fully matured
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS,
	}))
	return addrs
}

func countEvents(ctx sdk.Context, typ string) int {
	n := 0
	for _, e := range ctx.EventManager().Events() {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// SEA-01: once RETRO_REWARDS has paid, the transition cannot be aborted, and
// a re-run of the phase never pays a Rewarded nomination again.
func TestSEA01_RetroPaidOnlyOnce(t *testing.T) {
	f := initBeginBlockFixture(t)
	retroSetup(t, f, 2, 100_000_000)

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES, state.Phase)
	require.Equal(t, 2, countEvents(f.ctx, "retro_reward_distributed"))

	// Abort after the payout is refused, even for governance.
	ms := keeper.NewMsgServerImpl(f.keeper)
	gov, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)
	_, err = ms.AbortSeasonTransition(f.ctx, &types.MsgAbortSeasonTransition{Authority: gov})
	require.ErrorIs(t, err, types.ErrTransitionTooFarToAbort)

	// A forced re-run of the phase pays nothing a second time.
	ctx2 := f.ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, f.keeper.SeasonTransitionState.Set(ctx2, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS,
	}))
	require.NoError(t, f.keeper.BeginBlocker(ctx2))
	require.Zero(t, countEvents(ctx2, "retro_reward_distributed"))
}

// SEA-01: abort is still possible while convictions are being tallied.
func TestSEA01_AbortAllowedBeforePayout(t *testing.T) {
	f := initBeginBlockFixture(t)
	retroSetup(t, f, 1, 100_000_000)
	ms := keeper.NewMsgServerImpl(f.keeper)
	gov, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	_, err := ms.AbortSeasonTransition(f.ctx, &types.MsgAbortSeasonTransition{Authority: gov})
	require.NoError(t, err)
	for _, phase := range []types.TransitionPhase{
		types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES,
		types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT,
	} {
		require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: phase}))
		_, err = ms.AbortSeasonTransition(f.ctx, &types.MsgAbortSeasonTransition{Authority: gov})
		require.ErrorIs(t, err, types.ErrTransitionTooFarToAbort, phase.String())
	}
}

// SEA-03: a failing batch never fails the block; failures are persisted and
// recovery mode halts processing until a retry.
func TestSEA03_TransitionErrorDoesNotHaltChain(t *testing.T) {
	f := initBeginBlockFixture(t)
	retroSetup(t, f, 1, 100_000_000)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.TransitionMaxRetries = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// A corrupt cursor makes the retro phase fail deterministically.
	bad := types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS, LastProcessed: "not-a-number"}
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, bad))

	require.NoError(t, f.keeper.BeginBlocker(f.ctx), "a transition error must not fail BeginBlock")
	rec, err := f.keeper.TransitionRecoveryState.Get(f.ctx)
	require.NoError(t, err, "the failure must be persisted")
	require.Equal(t, uint64(1), rec.FailureCount)
	require.False(t, rec.RecoveryMode)

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	rec, _ = f.keeper.TransitionRecoveryState.Get(f.ctx)
	require.True(t, rec.RecoveryMode)

	// In recovery mode nothing runs: the count stays put.
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	rec, _ = f.keeper.TransitionRecoveryState.Get(f.ctx)
	require.Equal(t, uint64(2), rec.FailureCount)

	// Retry resets the cursor and the retry budget; the phase then completes.
	ms := keeper.NewMsgServerImpl(f.keeper)
	gov, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	_, err = ms.RetrySeasonTransition(f.ctx, &types.MsgRetrySeasonTransition{Authority: gov})
	require.NoError(t, err)
	rec, _ = f.keeper.TransitionRecoveryState.Get(f.ctx)
	require.Zero(t, rec.FailureCount)
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES, state.Phase)
}

// SEA-04: the retro phase is batched; nothing is paid until every
// nomination's conviction has been tallied.
func TestSEA04_RetroPhaseBatched(t *testing.T) {
	f := initBeginBlockFixture(t)
	retroSetup(t, f, 3, 100_000_000)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.TransitionBatchSize = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	for i := 1; i <= 3; i++ {
		require.NoError(t, f.keeper.BeginBlocker(f.ctx))
		state, err := f.keeper.SeasonTransitionState.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS, state.Phase, "block %d", i)
		require.Equal(t, fmt.Sprintf("%d", i), state.LastProcessed)
		require.Zero(t, countEvents(f.ctx, "retro_reward_distributed"))
		nom, _ := f.keeper.Nomination.Get(f.ctx, uint64(i))
		require.True(t, nom.Conviction.Equal(math.LegacyNewDec(100_000_000)), "conviction stored for nomination %d", i)
	}
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	state, _ := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES, state.Phase)
	require.Equal(t, 3, countEvents(f.ctx, "retro_reward_distributed"))
}

// SEA-05: SNAPSHOT resumes strictly after its cursor and builds TOP_XP
// eligibility incrementally across batches.
func TestSEA05_SnapshotBatchesAndTopXP(t *testing.T) {
	f := initBeginBlockFixture(t)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	require.NoError(t, f.keeper.SetTitle(f.ctx, types.Title{
		TitleId: "champion", Name: "Champion", Seasonal: true,
		RequirementType: types.RequirementType_REQUIREMENT_TYPE_TOP_XP, RequirementThreshold: 2,
	}))
	xp := map[string]uint64{}
	for i, v := range []uint64{10, 30, 20} {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("phoenix%d________", i))).String()
		xp[addr] = v
		require.NoError(t, f.keeper.MemberProfile.Set(f.ctx, addr, types.MemberProfile{Address: addr, SeasonXp: v, SeasonLevel: 1}))
	}
	params, _ := f.keeper.Params.Get(f.ctx)
	params.TransitionBatchSize = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_SNAPSHOT}))

	for i := 0; i < 3; i++ {
		require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	}
	state, _ := f.keeper.SeasonTransitionState.Get(f.ctx)
	require.Equal(t, types.TransitionPhase_TRANSITION_PHASE_ARCHIVE_REPUTATION, state.Phase)
	require.True(t, state.MaintenanceMode, "maintenance mode is on from SNAPSHOT (SEA-16)")

	elig, err := f.keeper.SeasonTitleEligibility.Get(f.ctx, 1)
	require.NoError(t, err)
	require.Len(t, elig.Grants, 1)
	var want []string
	for a, v := range xp {
		if v >= 20 {
			want = append(want, a)
		}
	}
	require.ElementsMatch(t, want, elig.Grants[0].EligibleMembers)
	require.IsIncreasing(t, elig.Grants[0].EligibleMembers, "sorted for the TITLES binary search")
	for a := range xp {
		_, err := f.keeper.MemberSeasonSnapshot.Get(f.ctx, "1/"+a)
		require.NoError(t, err, "every profile snapshotted exactly once")
	}
}

// SEA-07: retro-PGF thresholds are whole DREAM in micro-DREAM.
func TestSEA07_RetroThresholdDefaultsAreMicroDream(t *testing.T) {
	p := types.DefaultParams()
	oneDream := math.LegacyNewDec(1_000_000)
	require.True(t, p.NominationMinStake.GTE(oneDream), "nomination_min_stake below 1 DREAM: %s", p.NominationMinStake)
	require.True(t, p.RetroRewardMinConviction.GTE(oneDream), "retro_reward_min_conviction below 1 DREAM: %s", p.RetroRewardMinConviction)
	require.True(t, p.NominationMinStake.Equal(math.LegacyNewDec(10_000_000)))
	require.True(t, p.RetroRewardMinConviction.Equal(math.LegacyNewDec(50_000_000)))
}

type stubBlog struct {
	authors map[uint64]string
	hidden  map[uint64]bool
	created map[uint64]int64
}

func (s stubBlog) HasPost(_ context.Context, id uint64) bool { _, ok := s.authors[id]; return ok }
func (s stubBlog) HasReply(_ context.Context, _ uint64) bool { return false }
func (s stubBlog) LivePostCreatedAt(_ context.Context, id uint64) (int64, bool) {
	_, ok := s.authors[id]
	return s.created[id], ok && !s.hidden[id]
}
func (s stubBlog) GetPostAuthor(_ context.Context, id uint64) (string, error) {
	if a, ok := s.authors[id]; ok {
		return a, nil
	}
	return "", fmt.Errorf("post %d not found", id)
}

// SEA-08 / SEA-13: the content's author cannot stake on its nomination, and
// a past season's nomination takes no stakes.
func TestSEA08_SEA13_StakeNominationGuards(t *testing.T) {
	f := initBeginBlockFixture(t)
	author := sdk.AccAddress([]byte("author__________")).String()
	nominator := sdk.AccAddress([]byte("nominator_______")).String()
	f.repKeeper.SetMember(author, 1_000_000_000, nil, 0)
	f.repKeeper.SetMember(nominator, 1_000_000_000, nil, 0)
	f.keeper.SetBlogKeeper(stubBlog{authors: map[uint64]string{7: author}})
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 2, Status: types.SeasonStatus_SEASON_STATUS_NOMINATION, EndBlock: 1_000_000}))
	require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{
		Id: 1, Nominator: nominator, ContentRef: "blog/post/7", Season: 2,
		TotalStaked: math.LegacyZeroDec(), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
	}))
	require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{
		Id: 2, Nominator: nominator, ContentRef: "blog/post/8", Season: 1,
		TotalStaked: math.LegacyZeroDec(), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
	}))
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.StakeNomination(f.ctx, &types.MsgStakeNomination{Creator: author, NominationId: 1, Amount: "20000000"})
	require.ErrorContains(t, err, "own content")

	_, err = ms.StakeNomination(f.ctx, &types.MsgStakeNomination{Creator: author, NominationId: 2, Amount: "20000000"})
	require.ErrorIs(t, err, types.ErrNominationNotFound)
}

// SEA-09: resolved records ahead of an expired moderation no longer starve
// it out of the per-block budget.
func TestSEA09_ModerationQueueSkipsResolvedRecords(t *testing.T) {
	f := initBeginBlockFixture(t)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	// 250 closed moderations whose keys sort before the live one.
	for i := 0; i < 250; i++ {
		member := fmt.Sprintf("a%04d", i)
		require.NoError(t, f.keeper.SetDisplayNameModeration(f.ctx, member, types.DisplayNameModeration{
			Member: member, ModeratedAt: 1, Active: false,
		}))
	}
	target := sdk.AccAddress([]byte("zzzz_target_____")).String()
	require.NoError(t, f.keeper.SetDisplayNameModeration(f.ctx, target, types.DisplayNameModeration{
		Member: target, RejectedName: "Bad", ModeratedAt: 100, Active: true,
	}))
	params, _ := f.keeper.Params.Get(f.ctx)
	f.ctx = f.ctx.WithBlockHeight(100 + int64(params.DisplayNameAppealPeriodBlocks) + 1)

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	mod, err := f.keeper.DisplayNameModeration.Get(f.ctx, target)
	require.NoError(t, err)
	require.False(t, mod.Active, "expired moderation resolved despite 250 records ahead of it")
	has, err := f.keeper.ModerationQueue.Has(f.ctx, collections.Join(int64(100), target))
	require.NoError(t, err)
	require.False(t, has, "resolved moderation leaves the queue")
}

// SEA-10: an individual committee member cannot drive the transition or the
// season calendar; the council policy address can.
func TestSEA10_TransitionAuthorityNeedsPolicyAddress(t *testing.T) {
	member := sdk.AccAddress([]byte("committee_member")).String()
	council := sdk.AccAddress([]byte("council_policy__")).String()
	f := initFixtureWithCommons(t, newMockCommonsKeeperWithCouncil(council, member))
	ctx := sdk.UnwrapSDKContext(f.ctx)
	require.NoError(t, f.keeper.Season.Set(ctx, types.Season{Number: 1, EndBlock: 1000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	ms := keeper.NewMsgServerImpl(f.keeper)

	_, err := ms.ExtendSeason(ctx, &types.MsgExtendSeason{Authority: member, ExtensionEpochs: 1, Reason: "x"})
	require.ErrorIs(t, err, types.ErrNotCommonsCouncil)
	_, err = ms.ExtendSeason(ctx, &types.MsgExtendSeason{Authority: council, ExtensionEpochs: 1, Reason: "x"})
	require.NoError(t, err)

	require.NoError(t, f.keeper.SeasonTransitionState.Set(ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS}))
	_, err = ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{Authority: member})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = ms.SkipTransitionPhase(ctx, &types.MsgSkipTransitionPhase{Authority: member})
	require.ErrorIs(t, err, types.ErrNotAuthorized)
	_, err = ms.AbortSeasonTransition(ctx, &types.MsgAbortSeasonTransition{Authority: council})
	require.NoError(t, err)
}

// SEA-11: history beyond its retention window and expired guild invites are
// pruned by the BeginBlocker.
func TestSEA11_HistoryPruning(t *testing.T) {
	f := initBeginBlockFixture(t)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.EpochBlocks = 10
	params.EpochTrackerRetentionEpochs = 5
	params.InviteCleanupIntervalBlocks = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 20, EndBlock: 10_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	f.ctx = f.ctx.WithBlockHeight(1000) // epoch 100

	member := sdk.AccAddress([]byte("phoenix_________")).String()
	require.NoError(t, f.keeper.EpochXpTracker.Set(f.ctx, member+":10", types.EpochXpTracker{MemberEpoch: member + ":10"}))
	require.NoError(t, f.keeper.EpochXpTracker.Set(f.ctx, member+":99", types.EpochXpTracker{MemberEpoch: member + ":99"}))
	require.NoError(t, f.keeper.VoteXpRecord.Set(f.ctx, "3:"+member+":1", types.VoteXpRecord{SeasonMemberProposal: "3:" + member + ":1"}))
	require.NoError(t, f.keeper.VoteXpRecord.Set(f.ctx, "20:"+member+":1", types.VoteXpRecord{SeasonMemberProposal: "20:" + member + ":1"}))
	require.NoError(t, f.keeper.MemberSeasonSnapshot.Set(f.ctx, "2/"+member, types.MemberSeasonSnapshot{SeasonAddress: "2/" + member}))
	require.NoError(t, f.keeper.MemberSeasonSnapshot.Set(f.ctx, "19/"+member, types.MemberSeasonSnapshot{SeasonAddress: "19/" + member}))

	// An expired guild invite holding a slot.
	invitee := sdk.AccAddress([]byte("aurora__________")).String()
	require.NoError(t, f.keeper.Guild.Set(f.ctx, 1, types.Guild{Id: 1, PendingInvites: []string{invitee}}))
	require.NoError(t, f.keeper.GuildInvite.Set(f.ctx, "1:"+invitee, types.GuildInvite{GuildInvitee: invitee, ExpiresEpoch: 50}))

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))

	_, err := f.keeper.EpochXpTracker.Get(f.ctx, member+":10")
	require.Error(t, err, "old epoch tracker pruned")
	_, err = f.keeper.EpochXpTracker.Get(f.ctx, member+":99")
	require.NoError(t, err, "recent epoch tracker kept")
	_, err = f.keeper.VoteXpRecord.Get(f.ctx, "3:"+member+":1")
	require.Error(t, err)
	_, err = f.keeper.VoteXpRecord.Get(f.ctx, "20:"+member+":1")
	require.NoError(t, err)
	_, err = f.keeper.MemberSeasonSnapshot.Get(f.ctx, "2/"+member)
	require.Error(t, err)
	_, err = f.keeper.MemberSeasonSnapshot.Get(f.ctx, "19/"+member)
	require.NoError(t, err)
	_, err = f.keeper.GuildInvite.Get(f.ctx, "1:"+invitee)
	require.Error(t, err, "expired invite removed")
	g, _ := f.keeper.Guild.Get(f.ctx, 1)
	require.Empty(t, g.PendingInvites, "slot freed")
}

// SEA-12: the retro budget base is initiative-completion minting only.
func TestSEA12_RetroBudgetUsesInitiativeMinting(t *testing.T) {
	f := initBeginBlockFixture(t)
	retroSetup(t, f, 1, 100_000_000)
	base := math.NewInt(100_000_000_000) // 100,000 DREAM of initiative minting
	f.repKeeper.InitiativeRewardsMinted = &base
	require.NoError(t, f.keeper.BeginBlocker(f.ctx))
	nom, err := f.keeper.Nomination.Get(f.ctx, 1)
	require.NoError(t, err)
	// 25% of 100,000 DREAM, inside [10,000, 75,000]; GetSeasonMinted (60,000)
	// would have given 15,000.
	require.True(t, nom.RewardAmount.Equal(math.LegacyNewDec(25_000_000_000)), "got %s", nom.RewardAmount)
}

// SEA-14: aborts are capped per season and the horizon check counts every
// extension and abort.
func TestSEA14_AbortCapAndHorizon(t *testing.T) {
	f := initBeginBlockFixture(t)
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 100, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	ms := keeper.NewMsgServerImpl(f.keeper)
	gov, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	for i := 0; i < types.MaxTransitionAbortsPerSeason; i++ {
		require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS}))
		_, err := ms.AbortSeasonTransition(f.ctx, &types.MsgAbortSeasonTransition{Authority: gov})
		require.NoError(t, err)
	}
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS}))
	_, err := ms.AbortSeasonTransition(f.ctx, &types.MsgAbortSeasonTransition{Authority: gov})
	require.ErrorIs(t, err, types.ErrTransitionTooFarToAbort)

	p := types.DefaultParams()
	require.NoError(t, p.Validate())
	p.MaxSeasonExtensions = 1000
	if reptypes.AliasDedupHorizonEpochs < 1<<40 { // production horizon only
		require.Error(t, p.Validate(), "extensions x max_extension_epochs must count against the horizon")
	}
	require.Equal(t, p.SeasonDurationEpochs+p.SeasonTransitionEpochs+int64(p.MaxSeasonExtensions)*int64(p.MaxExtensionEpochs)+
		int64(types.MaxTransitionAbortsPerSeason)*((int64(p.TransitionGracePeriod)+p.EpochBlocks-1)/p.EpochBlocks),
		p.MaxSeasonLengthEpochs())
}

// SEA-17: an unlock failure is never reported as a return; a lock that is
// already gone (zeroed member) drops the record, anything else keeps it.
func TestSEA17_NominationStakeReturnFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		recordKept bool
	}{
		{"store failure keeps the record", fmt.Errorf("decay failed"), true},
		{"zeroed staker drops the record", reptypes.ErrInsufficientStake, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := initBeginBlockFixture(t)
			require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
			staker := sdk.AccAddress([]byte("zenith__________")).String()
			require.NoError(t, f.keeper.SetNomination(f.ctx, types.Nomination{
				Id: 1, Nominator: staker, ContentRef: "blog/post/1", Season: 1,
				TotalStaked: math.LegacyZeroDec(), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
			}))
			key := "1/" + staker
			require.NoError(t, f.keeper.NominationStake.Set(f.ctx, key, types.NominationStake{NominationId: 1, Staker: staker, Amount: math.LegacyNewDec(10_000_000)}))
			f.repKeeper.UnlockErr = tc.err
			require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{Phase: types.TransitionPhase_TRANSITION_PHASE_RETURN_NOMINATION_STAKES}))

			require.NoError(t, f.keeper.BeginBlocker(f.ctx))
			require.Zero(t, countEvents(f.ctx, "nomination_stake_returned"))
			require.Equal(t, 1, countEvents(f.ctx, "nomination_stake_return_failed"))
			_, err := f.keeper.NominationStake.Get(f.ctx, key)
			if tc.recordKept {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// SEA-19: the watchdog flags an overdue transition, and quest prerequisites
// must exist.
func TestSEA19_WatchdogAndQuestPrerequisites(t *testing.T) {
	f := initBeginBlockFixture(t)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.EpochBlocks = 10
	params.MaxTransitionEpochs = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, Status: types.SeasonStatus_SEASON_STATUS_ENDING}))
	require.NoError(t, f.keeper.SeasonTransitionState.Set(f.ctx, types.SeasonTransitionState{
		Phase: types.TransitionPhase_TRANSITION_PHASE_RETRO_REWARDS, TransitionStart: 100,
	}))
	ctx := f.ctx.WithBlockHeight(100 + 20 + 1)
	require.NoError(t, f.keeper.BeginBlocker(ctx))
	require.Equal(t, 1, countEvents(ctx, "season_transition_overdue"))

	ms := keeper.NewMsgServerImpl(f.keeper)
	gov, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	_, err := ms.CreateQuest(f.ctx, &types.MsgCreateQuest{Authority: gov, QuestId: "q1", Name: "Q", PrerequisiteQuest: "missing"})
	require.ErrorIs(t, err, types.ErrInvalidQuestObjective)
	_, err = ms.CreateQuest(f.ctx, &types.MsgCreateQuest{Authority: gov, QuestId: "q1", Name: "Q", RequiredAchievement: "missing"})
	require.ErrorIs(t, err, types.ErrInvalidQuestObjective)
	_, err = ms.CreateQuest(f.ctx, &types.MsgCreateQuest{Authority: gov, QuestId: "q1", Name: "Q", PrerequisiteQuest: "q1"})
	require.ErrorIs(t, err, types.ErrInvalidQuestObjective, "self-prerequisite is a cycle")
}

// SEA-08: a hidden or deleted post cannot be nominated.
func TestSEA08_HiddenContentRefused(t *testing.T) {
	f := initBeginBlockFixture(t)
	author := sdk.AccAddress([]byte("author__________")).String()
	f.keeper.SetBlogKeeper(stubBlog{authors: map[uint64]string{7: author, 8: author}, hidden: map[uint64]bool{8: true}})
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "blog/post/7"))
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "blog/post/8"), types.ErrInvalidContentRef)

	// Content older than the current season is refused.
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 2, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE, StartTime: 5_000, EndBlock: 1_000_000}))
	f.keeper.SetBlogKeeper(stubBlog{authors: map[uint64]string{7: author, 9: author}, created: map[uint64]int64{7: 4_999, 9: 5_000}})
	require.ErrorIs(t, f.keeper.ValidateContentRef(f.ctx, "blog/post/7"), types.ErrInvalidContentRef)
	require.NoError(t, f.keeper.ValidateContentRef(f.ctx, "blog/post/9"))
}

// SEA-11 (remainder): old nominations without stakes, old retro reward
// records and long-closed display-name moderations are pruned; a nomination
// that still carries a stake is kept.
func TestSEA11_RetroAndModerationPruning(t *testing.T) {
	f := initBeginBlockFixture(t)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.SnapshotRetentionSeasons = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 20, EndBlock: 100_000_000, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	f.ctx = f.ctx.WithBlockHeight(2_000_000)

	zero := math.LegacyZeroDec()
	nom := func(id, season uint64) types.Nomination {
		return types.Nomination{Id: id, Season: season, ContentRef: fmt.Sprintf("blog/post/%d", id), TotalStaked: zero, Conviction: zero, RewardAmount: zero}
	}
	require.NoError(t, f.keeper.SetNomination(f.ctx, nom(1, 3)))  // old, no stake: pruned
	require.NoError(t, f.keeper.SetNomination(f.ctx, nom(2, 3)))  // old, staked: kept
	require.NoError(t, f.keeper.SetNomination(f.ctx, nom(3, 19))) // recent: kept
	staker := sdk.AccAddress([]byte("zenith__________")).String()
	require.NoError(t, f.keeper.NominationStake.Set(f.ctx, "2/"+staker, types.NominationStake{NominationId: 2, Staker: staker, Amount: math.LegacyNewDec(1)}))
	require.NoError(t, f.keeper.RetroRewardRecord.Set(f.ctx, "3/1", types.RetroRewardRecord{Season: 3, NominationId: 1, Conviction: zero, RewardAmount: zero}))
	require.NoError(t, f.keeper.RetroRewardRecord.Set(f.ctx, "19/3", types.RetroRewardRecord{Season: 19, NominationId: 3, Conviction: zero, RewardAmount: zero}))
	closed := sdk.AccAddress([]byte("closed__________")).String()
	recent := sdk.AccAddress([]byte("recent__________")).String()
	require.NoError(t, f.keeper.DisplayNameModeration.Set(f.ctx, closed, types.DisplayNameModeration{Member: closed, ModeratedAt: 100}))
	require.NoError(t, f.keeper.DisplayNameModeration.Set(f.ctx, recent, types.DisplayNameModeration{Member: recent, ModeratedAt: 1_999_000}))

	require.NoError(t, f.keeper.BeginBlocker(f.ctx))

	_, err := f.keeper.Nomination.Get(f.ctx, 1)
	require.Error(t, err)
	has, err := f.keeper.NominationsBySeason.Has(f.ctx, collections.Join(uint64(3), uint64(1)))
	require.NoError(t, err)
	require.False(t, has)
	_, err = f.keeper.Nomination.Get(f.ctx, 2)
	require.NoError(t, err, "staked nomination kept")
	_, err = f.keeper.Nomination.Get(f.ctx, 3)
	require.NoError(t, err)
	_, err = f.keeper.RetroRewardRecord.Get(f.ctx, "3/1")
	require.Error(t, err)
	_, err = f.keeper.RetroRewardRecord.Get(f.ctx, "19/3")
	require.NoError(t, err)
	_, err = f.keeper.DisplayNameModeration.Get(f.ctx, closed)
	require.Error(t, err)
	_, err = f.keeper.DisplayNameModeration.Get(f.ctx, recent)
	require.NoError(t, err)
}

// SEA-15: the quest hook reads a member's open quests through the Open
// index; completed records do not cost gas on the vote path.
func TestSEA15_QuestHookReadsOpenQuestsOnly(t *testing.T) {
	f := initBeginBlockFixture(t)
	member := sdk.AccAddress([]byte("phoenix_________"))
	m := member.String()
	set := func(quest string, completed bool) {
		key := m + ":" + quest
		require.NoError(t, f.keeper.MemberQuestProgress.Set(f.ctx, key, types.MemberQuestProgress{MemberQuest: key, Completed: completed}))
	}
	for i := 0; i < 30; i++ {
		set(fmt.Sprintf("done%02d", i), true)
	}
	set("open", false)
	count := func() int {
		n := 0
		require.NoError(t, f.keeper.MemberQuestProgress.Indexes.Open.Walk(f.ctx,
			collections.NewPrefixedPairRange[string, string](m+"|open"),
			func(string, string) (bool, error) { n++; return false, nil }))
		return n
	}
	require.Equal(t, 1, count())

	sdkCtx := f.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	require.NoError(t, f.keeper.OnQuestObjectiveProgress(sdkCtx, member, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, 1))
	require.Less(t, sdkCtx.GasMeter().GasConsumed(), uint64(10_000), "30 completed quests must not be read")

	set("open", true)
	require.Equal(t, 0, count())
}
