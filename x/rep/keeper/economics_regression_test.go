package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Regression tests for the x/rep economics findings (REP-E-01 .. REP-E-20).

func econMember(t *testing.T, f *fixture, addr sdk.AccAddress, balance, staked int64) {
	t.Helper()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:          addr.String(),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:     keeper.PtrInt(math.NewInt(balance)),
		StakedDream:      keeper.PtrInt(math.NewInt(staked)),
		LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
		LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"tag": "100.0"},
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))
}

func econGet(t *testing.T, f *fixture, addr sdk.AccAddress) types.Member {
	t.Helper()
	m, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	return m
}

// REP-E-01: members cannot raise system-only interims, nor an interim whose
// deadline has passed; and a member-assigned ADJUDICATION interim expiring
// never resolves a challenge.
func TestREPE01_MemberCannotRaiseAdjudication(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	ms := keeper.NewMsgServerImpl(k)

	initID, chalID, _ := challengedInitiative(t, k, ctx)
	member := sdk.AccAddress([]byte("repe01-member-------"))
	econMember(t, f, member, 1_000_000, 0)

	for _, typ := range []types.InterimType{types.InterimType_INTERIM_TYPE_ADJUDICATION, types.InterimType_INTERIM_TYPE_JURY_DUTY} {
		_, err := ms.CreateInterim(ctx, &types.MsgCreateInterim{
			Creator: member.String(), InterimType: typ, ReferenceId: initID,
			Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, Deadline: ctx.BlockHeight() + 100,
		})
		require.ErrorIs(t, err, types.ErrInterimTypeSystemOnly)
	}
	_, err := ms.CreateInterim(ctx, &types.MsgCreateInterim{
		Creator: member.String(), InterimType: types.InterimType_INTERIM_TYPE_OTHER,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, Deadline: ctx.BlockHeight(),
	})
	require.ErrorIs(t, err, types.ErrInterimDeadlineInvalid)

	// Defence in depth: an ADJUDICATION interim not raised by the module
	// (here written through the keeper with a member assignee) does not touch
	// the challenge when it expires.
	interimID, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_ADJUDICATION,
		[]string{member.String()}, "", initID, "forged", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, 0)
	require.NoError(t, err)
	require.NoError(t, k.ExpireInterim(ctx, interimID))
	ch, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, ch.Status)
}

// REP-E-02 / REP-E-05: an assignee never pays itself; approval is by someone
// else.
func TestREPE02_InterimPaidOnlyByAnotherApprover(t *testing.T) {
	member := sdk.AccAddress([]byte("repe02-member-------"))
	f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(member)))
	k, ctx := f.keeper, f.ctx
	ms := keeper.NewMsgServerImpl(k)
	econMember(t, f, member, 0, 0)

	_, err := ms.CreateInterim(ctx, &types.MsgCreateInterim{
		Creator: member.String(), InterimType: types.InterimType_INTERIM_TYPE_OTHER,
		Complexity: types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT, Deadline: ctx.BlockHeight() + 100,
	})
	require.NoError(t, err)
	id := uint64(1)

	_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{Creator: member.String(), InterimId: id})
	require.ErrorIs(t, err, types.ErrInterimNeedsApproval)

	// Even an Operations Committee member cannot approve their own interim.
	_, err = ms.ApproveInterim(ctx, &types.MsgApproveInterim{Creator: member.String(), InterimId: id, Approved: true})
	require.ErrorIs(t, err, types.ErrSelfApproval)
	require.True(t, econGet(t, f, member).DreamBalance.IsZero())

	// A JURY_DUTY interim (legacy state) is not self-completable either.
	jd, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_JURY_DUTY, []string{member.String()},
		"", 0, "challenge", types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, ctx.BlockHeight()+100)
	require.NoError(t, err)
	_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{Creator: member.String(), InterimId: jd})
	require.ErrorIs(t, err, types.ErrInterimNeedsApproval)
}

// REP-E-03: fees cannot burn locked DREAM; a slash burns its own lock; an
// unlock never releases more than it is owed.
func TestREPE03_BurnRespectsLocks(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	addr := sdk.AccAddress([]byte("repe03-locked-------"))
	econMember(t, f, addr, 100, 100)

	require.ErrorIs(t, k.BurnDREAM(ctx, addr, math.NewInt(1)), types.ErrInsufficientBalance)

	require.NoError(t, k.BurnLockedDREAM(ctx, addr, math.NewInt(40)))
	m := econGet(t, f, addr)
	require.Equal(t, math.NewInt(60), *m.DreamBalance)
	require.Equal(t, math.NewInt(60), *m.StakedDream)

	require.ErrorIs(t, k.BurnLockedDREAM(ctx, addr, math.NewInt(61)), types.ErrInsufficientStake)
	require.ErrorIs(t, k.UnlockDREAM(ctx, addr, math.NewInt(61)), types.ErrInsufficientStake)
	require.Equal(t, math.NewInt(60), *econGet(t, f, addr).StakedDream)
}

// REP-E-03 (GLM REP-1): the inviter accountability penalty is capped at the
// inviter's unlocked balance, so the inviter's escrows stay backed.
func TestREPE03_InviterPenaltyCappedToUnlocked(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	inviter := sdk.AccAddress([]byte("repe03-inviter------"))
	invitee := sdk.AccAddress([]byte("repe03-invitee------"))
	econMember(t, f, inviter, 1000, 800)
	econMember(t, f, invitee, 0, 0)

	stake := math.NewInt(500)
	inv := types.Invitation{
		Id: 7, Inviter: inviter.String(), InviteeAddress: invitee.String(), StakedDream: &stake,
		Status:            types.InvitationStatus_INVITATION_STATUS_ACCEPTED,
		AccountabilityEnd: ctx.BlockTime().Unix() + 1000,
	}
	require.NoError(t, k.Invitation.Set(ctx, inv.Id, inv))
	require.NoError(t, k.InvitationsByInvitee.Set(ctx, invitee.String(), inv.Id))

	require.NoError(t, k.ProcessInviterAccountability(ctx, invitee, "test"))
	m := econGet(t, f, inviter)
	require.Equal(t, math.NewInt(800), *m.DreamBalance, "only the 200 unlocked is burned")
	require.Equal(t, math.NewInt(800), *m.StakedDream)
}

// REP-E-04: a conceded challenge (no verdict) mints no reward beyond the
// assignee's forfeited bond; a verdict pays the reward, booked against the
// season cap; a challenger invited by the assignee earns nothing.
func TestREPE04_ChallengeRingEarnsNothing(t *testing.T) {
	t.Run("conceded", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		_, chalID, challenger := challengedInitiative(t, k, ctx)
		before := econGet(t, f, challenger)
		require.NoError(t, k.UpholdConcededChallenge(ctx, chalID))
		after := econGet(t, f, challenger)
		require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String(), "stake returned, nothing minted")
		require.True(t, after.StakedDream.IsZero())
	})
	t.Run("verdict", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		_, chalID, challenger := challengedInitiative(t, k, ctx)
		before := econGet(t, f, challenger)
		mintedBefore, err := k.GetSeasonInitiativeRewardsMinted(ctx)
		require.NoError(t, err)
		require.NoError(t, k.UpholdChallenge(ctx, chalID))
		gained := econGet(t, f, challenger).DreamBalance.Sub(*before.DreamBalance)
		require.True(t, gained.IsPositive(), "a verdict pays the challenger")
		mintedAfter, err := k.GetSeasonInitiativeRewardsMinted(ctx)
		require.NoError(t, err)
		require.Equal(t, gained, mintedAfter.Sub(mintedBefore), "booked against the season cap")
	})
	t.Run("invited by the assignee", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		initID, chalID, challenger := challengedInitiative(t, k, ctx)
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		m := econGet(t, f, challenger)
		m.InvitedBy = initiative.Assignee
		require.NoError(t, k.Member.Set(ctx, challenger.String(), m))
		before := econGet(t, f, challenger)
		require.NoError(t, k.UpholdChallenge(ctx, chalID))
		require.Equal(t, before.DreamBalance.String(), econGet(t, f, challenger).DreamBalance.String())
	})
}

// REP-E-06: zeroing forfeits the member's open challenge and pending
// invitation, frees the invitee slot, and the member never locks again.
func TestREPE06_ZeroingTerminatesEscrows(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	initID, chalID, challenger := challengedInitiative(t, k, ctx)

	m := econGet(t, f, challenger)
	m.InvitationCredits = 3
	require.NoError(t, k.Member.Set(ctx, challenger.String(), m))
	invitee := sdk.AccAddress([]byte("repe06-invitee------"))
	invID, err := k.CreateInvitation(ctx, challenger, invitee, k.RequiredInvitationStakeForTest(ctx, challenger), nil)
	require.NoError(t, err)

	require.NoError(t, k.ZeroMember(ctx, challenger, "test"))

	ch, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, ch.Status)
	has, err := k.HasActiveChallenges(ctx, initID)
	require.NoError(t, err)
	require.False(t, has, "the initiative is no longer frozen")

	inv, err := k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_REVOKED, inv.Status)
	_, err = k.InvitationsByInvitee.Get(ctx, invitee.String())
	require.Error(t, err, "invitee slot freed")

	z := econGet(t, f, challenger)
	require.True(t, z.DreamBalance.IsZero())
	require.True(t, z.StakedDream.IsZero())
	require.Zero(t, z.InvitationCredits)

	// Funded again later, a zeroed member still cannot lock (nor vouch).
	require.NoError(t, k.MintDREAM(ctx, challenger, math.NewInt(1_000)))
	require.ErrorIs(t, k.LockDREAM(ctx, challenger, math.NewInt(1)), types.ErrMemberNotActive)
}

// REP-E-07: an inviter that migrated keeps its pending invitation: revoking
// it from the new address unlocks the stake and restores the credit.
func TestREPE07_MigratedInviterRevokes(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	ms := keeper.NewMsgServerImpl(k)
	oldAddr := sdk.AccAddress([]byte("repe07-old----------"))
	newAddr := sdk.AccAddress([]byte("repe07-new----------"))
	econMember(t, f, oldAddr, 500_000_000, 0)
	m := econGet(t, f, oldAddr)
	m.InvitationCredits = 2
	require.NoError(t, k.Member.Set(ctx, oldAddr.String(), m))
	invitee := sdk.AccAddress([]byte("repe07-invitee------"))
	invID, err := k.CreateInvitation(ctx, oldAddr, invitee, k.RequiredInvitationStakeForTest(ctx, oldAddr), nil)
	require.NoError(t, err)

	// Migrate old -> new (the record moves, the invitation stays under old).
	moved := econGet(t, f, oldAddr)
	moved.Address = newAddr.String()
	require.NoError(t, k.Member.Remove(ctx, oldAddr.String()))
	require.NoError(t, k.Member.Set(ctx, newAddr.String(), moved))
	require.NoError(t, k.Tombstones.Set(ctx, oldAddr.String(), types.Tombstone{Canonical: newAddr.String()}))

	_, err = ms.RevokeInvitation(ctx, &types.MsgRevokeInvitation{Inviter: newAddr.String(), InvitationId: invID})
	require.NoError(t, err)
	after := econGet(t, f, newAddr)
	require.True(t, after.StakedDream.IsZero(), "stake unlocked on the canonical record")
	require.Equal(t, moved.InvitationCredits+1, after.InvitationCredits)
}

// REP-E-08: one committee member cannot move the epoch clocks.
func TestREPE08_EpochClocksLocked(t *testing.T) {
	f := initFixture(t)
	f.commonsKeeper.IsCouncilAuthorizedFn = func(context.Context, string, string, string) bool { return true }
	k, ctx := f.keeper, f.ctx
	ms := keeper.NewMsgServerImpl(k)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	op := params.ExtractOperationalParams()
	op.EpochBlocks = params.EpochBlocks / 2
	_, err = ms.UpdateOperationalParams(ctx, &types.MsgUpdateOperationalParams{Authority: f.keeper.GetAuthorityString(), OperationalParams: op})
	require.ErrorIs(t, err, types.ErrParamImmutable)

	op = params.ExtractOperationalParams()
	op.SeasonDurationEpochs++
	_, err = ms.UpdateOperationalParams(ctx, &types.MsgUpdateOperationalParams{Authority: f.keeper.GetAuthorityString(), OperationalParams: op})
	require.ErrorIs(t, err, types.ErrParamGovernanceOnly)

	changed := params
	changed.EpochBlocks = 1
	// Keep the ZK key delay's wall time unchanged so Validate's
	// anonymous-action-window check (network builds) is not what refuses it.
	changed.ZkKeyActivationDelay = params.ZkKeyActivationDelay * params.EpochBlocks
	_, err = ms.UpdateParams(ctx, &types.MsgUpdateParams{Authority: f.keeper.GetAuthorityString(), Params: changed})
	require.ErrorIs(t, err, types.ErrParamImmutable)
}

// REP-E-09: per-member per-epoch tag creation limit and a counter-based cap
// that keeps headroom for governance.
func TestREPE09_TagCreationLimits(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	ms := keeper.NewMsgServerImpl(k)
	addr := sdk.AccAddress([]byte("repe09-tagger-------"))
	econMember(t, f, addr, 1_000_000_000, 0)

	names := []string{"repe-one", "repe-two", "repe-three", "repe-four", "repe-five", "repe-six"}
	for i, n := range names {
		_, err := ms.CreateTag(ctx, &types.MsgCreateTag{Creator: addr.String(), Name: n})
		if i < types.MaxTagCreationsPerEpoch {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, types.ErrTagCreationRateLimited)
		}
	}

	memberCap := types.DefaultMaxTotalTags - types.DefaultMaxTotalTags*types.CouncilTagHeadroomBps/10000
	require.NoError(t, k.TagCount.Set(ctx, memberCap))
	other := sdk.AccAddress([]byte("repe09-other--------"))
	econMember(t, f, other, 1_000_000_000, 0)
	_, err := ms.CreateTag(ctx, &types.MsgCreateTag{Creator: other.String(), Name: "repe-seven"})
	require.ErrorIs(t, err, types.ErrTagLimitExceeded)

	// Governance may still use the headroom.
	_, err = ms.CreateTag(ctx, &types.MsgCreateTag{Creator: f.keeper.GetAuthorityString(), Name: "repe-seven"})
	require.NoError(t, err)
	require.Equal(t, memberCap+1, k.GetTagCount(ctx))
}

// REP-E-10: tag stakes need a registered tag; member stakes are tranche
// capped per target member.
func TestREPE10_StakeCaps(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	staker := sdk.AccAddress([]byte("repe10-staker-------"))
	target := sdk.AccAddress([]byte("repe10-target-------"))
	econMember(t, f, staker, 10_000_000_000, 0)
	econMember(t, f, target, 0, 0)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.AllowSelfMemberStake = false
	require.NoError(t, k.Params.Set(ctx, params))
	amt := params.MinStakeAmount

	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_TAG, 0, "no-such-tag", amt)
	require.ErrorIs(t, err, types.ErrTagNotFound)

	for i := 0; i < types.MaxStakeTranchesPerTarget; i++ {
		_, err := k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), amt)
		require.NoError(t, err)
	}
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_MEMBER, 0, target.String(), amt)
	require.ErrorIs(t, err, types.ErrTooManyStakeTranches)

	// The circular check reads the by-staker index.
	has, err := k.HasMemberStakeOn(ctx, staker.String(), target.String())
	require.NoError(t, err)
	require.True(t, has)
}

// REP-E-11: a reported tag is not garbage-collected while its report is open.
func TestREPE11_ReportedTagSurvivesExpiry(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: "stale", LastUsedAt: 1}))
	require.NoError(t, k.TagReport.Set(ctx, "stale", types.TagReport{TagName: "stale"}))
	require.NoError(t, k.ExpireTags(ctx, 1+types.DefaultTagExpiration+1))
	ok, err := k.TagExists(ctx, "stale")
	require.NoError(t, err)
	require.True(t, ok)
}

// REP-E-13: a finalized interim cannot be relabelled.
func TestREPE13_AbandonOnlyLiveInterims(t *testing.T) {
	approver := sdk.AccAddress([]byte("repe13-approver-----"))
	f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(approver)))
	k, ctx := f.keeper, f.ctx
	ms := keeper.NewMsgServerImpl(k)
	member := sdk.AccAddress([]byte("repe13-member-------"))
	econMember(t, f, member, 0, 0)
	id, err := k.CreateInterimWork(ctx, types.InterimType_INTERIM_TYPE_OTHER, []string{member.String()},
		"", 0, "", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, ctx.BlockHeight()+100)
	require.NoError(t, err)
	_, err = ms.ApproveInterim(ctx, &types.MsgApproveInterim{Creator: approver.String(), InterimId: id, Approved: true})
	require.NoError(t, err)

	_, err = ms.AbandonInterim(ctx, &types.MsgAbandonInterim{Creator: member.String(), InterimId: id, Reason: "rewrite"})
	require.ErrorIs(t, err, types.ErrInvalidInterimStatus)
	in, err := k.GetInterim(ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, in.Status)
}

// REP-E-14: a zeroed member cannot invite, and a closed invitation never
// lifts credits above the trust level's maximum.
func TestREPE14_InvitationCredits(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	inviter := sdk.AccAddress([]byte("repe14-inviter------"))
	econMember(t, f, inviter, 1_000_000_000, 0)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	maxCredits := keeper.GetMaxInvitationCredits(params.TrustLevelConfig, types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	m := econGet(t, f, inviter)
	m.InvitationCredits = maxCredits
	require.NoError(t, k.Member.Set(ctx, inviter.String(), m))

	invitee := sdk.AccAddress([]byte("repe14-invitee------"))
	invID, err := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100_000_000), nil)
	require.NoError(t, err)
	// The season reset restores the full allowance before the close.
	m = econGet(t, f, inviter)
	m.InvitationCredits = maxCredits
	require.NoError(t, k.Member.Set(ctx, inviter.String(), m))
	inv, err := k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	ms := keeper.NewMsgServerImpl(k)
	_, err = ms.RevokeInvitation(ctx, &types.MsgRevokeInvitation{Inviter: inviter.String(), InvitationId: inv.Id})
	require.NoError(t, err)
	require.Equal(t, maxCredits, econGet(t, f, inviter).InvitationCredits, "clamped to the maximum")

	m = econGet(t, f, inviter)
	m.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	require.NoError(t, k.Member.Set(ctx, inviter.String(), m))
	_, err = k.CreateInvitation(ctx, inviter, sdk.AccAddress([]byte("repe14-invitee2-----")), math.NewInt(100_000_000), nil)
	require.ErrorIs(t, err, types.ErrMemberNotActive)
}

// REP-E-15: the award cooldown survives a genesis round trip.
func TestREPE15_AwardCooldownRebuilt(t *testing.T) {
	f := initFixture(t)
	gen := types.DefaultGenesis()
	gen.TagBudgetAwardList = []types.TagBudgetAward{{Id: 1, BudgetId: 3, PostId: 9, Amount: "1"}}
	gen.TagBudgetAwardCount = 2
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gen))
	_, err := f.keeper.TagBudgetAwardByPost.Get(f.ctx, collections.Join(uint64(3), uint64(9)))
	require.NoError(t, err)
}

// REP-E-16: missing amounts are refused instead of panicking, and evidence is
// bounded.
func TestREPE16_InputValidation(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	a := sdk.AccAddress([]byte("repe16-a------------")).String()
	b := sdk.AccAddress([]byte("repe16-b------------")).String()

	_, err := ms.InviteMember(f.ctx, &types.MsgInviteMember{Inviter: a, InviteeAddress: b})
	require.ErrorIs(t, err, types.ErrInvalidAmount)
	_, err = ms.TransferDream(f.ctx, &types.MsgTransferDream{Sender: a, Recipient: b})
	require.ErrorIs(t, err, types.ErrInvalidAmount)
	_, err = ms.ApproveProjectBudget(f.ctx, &types.MsgApproveProjectBudget{Approver: a, ProjectId: 1})
	require.ErrorIs(t, err, types.ErrInvalidAmount)
	// Gated on membership first; the point is that a nil budget no longer
	// panics.
	require.NotPanics(t, func() {
		_, err = ms.CreateInitiative(f.ctx, &types.MsgCreateInitiative{Creator: a, ProjectId: 1})
	})
	require.Error(t, err)

	evidence := make([]string, types.MaxChallengeEvidence+1)
	_, err = f.keeper.CreateChallenge(f.ctx, sdk.AccAddress([]byte("repe16-a------------")), 1, "r", evidence, math.NewInt(1))
	require.ErrorIs(t, err, types.ErrTooMuchEvidence)
}

// REP-E-17: a content challenge upheld after the bond was already slashed
// burns nothing more and mints no reward; the author's other escrow survives.
func TestREPE17_UpholdAfterBondSlashed(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	wireContentAuthor(k)
	author := sdk.AccAddress([]byte("repe17-author-------"))
	challenger := sdk.AccAddress([]byte("repe17-challenger---"))
	econMember(t, f, author, 1_000_000_000, 0)
	econMember(t, f, challenger, 1_000_000_000, 0)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.AuthorBondSlashOnModeration = true
	require.NoError(t, k.Params.Set(ctx, params))

	bondType := types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND
	_, err = k.CreateAuthorBond(ctx, author, bondType, 5, math.NewInt(50_000_000))
	require.NoError(t, err)
	require.NoError(t, k.LockDREAM(ctx, author, math.NewInt(200_000_000))) // unrelated escrow
	ccID, err := k.CreateContentChallenge(ctx, challenger, bondType, 5, "bad", nil, params.MinChallengeStake)
	require.NoError(t, err)

	require.NoError(t, k.SlashAuthorBond(ctx, bondType, 5)) // a sentinel hide
	before := econGet(t, f, challenger)
	require.NoError(t, k.UpholdContentChallenge(ctx, ccID))

	a := econGet(t, f, author)
	require.Equal(t, math.NewInt(200_000_000), *a.StakedDream, "the unrelated escrow is intact")
	require.Equal(t, math.NewInt(950_000_000), *a.DreamBalance, "the bond was slashed once")
	after := econGet(t, f, challenger)
	require.Equal(t, before.DreamBalance.String(), after.DreamBalance.String(), "no reward without a slash")
}

// REP-E-18: a content stake needs existing content.
func TestREPE18_ContentMustExist(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	staker := sdk.AccAddress([]byte("repe18-staker-------"))
	econMember(t, f, staker, 1_000_000_000, 0)
	k.SetBlogKeeper(&stubBlogKeeper{authors: map[uint64]string{}, replyAuthors: map[uint64]string{}})
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	_, err = k.CreateStake(ctx, staker, types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT, 77, "", params.MinStakeAmount)
	require.ErrorIs(t, err, types.ErrContentNotFound)
}

// REP-E-19: a group member cannot award the budget to their own post.
func TestREPE19_NoSelfAward(t *testing.T) {
	f := initFixture(t)
	fk := &mockForumKeeper{authors: map[uint64]string{}, tags: map[uint64][]string{}}
	f.keeper.SetForumKeeper(fk)
	ms := keeper.NewMsgServerImpl(f.keeper)
	group := sdk.AccAddress([]byte("repe19-group--------")).String()
	awarder := sdk.AccAddress([]byte("repe19-awarder------")).String()
	id, err := f.keeper.TagBudgetSeq.Next(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
		Id: id, GroupAccount: group, Tag: "gopher", PoolBalance: "1000", Active: true,
	}))
	fk.authors[42] = awarder
	fk.tags[42] = []string{"gopher"}

	_, err = ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{Creator: awarder, BudgetId: id, PostId: 42, Amount: "100"})
	require.ErrorIs(t, err, types.ErrSelfAward)
}

// REP-E-20: a huge page limit is clamped instead of overflowing.
func TestREPE20_SortedPageClamped(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	_, err := qs.ListProject(f.ctx, &types.QueryAllProjectRequest{
		Pagination: &query.PageRequest{Offset: 1, Limit: ^uint64(0)},
	})
	require.NoError(t, err)
}

// Handoff: AddReputationCapped applies the per-epoch per-tag cap.
func TestAddReputationCapped(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	addr := sdk.AccAddress([]byte("repcap-member-------"))
	econMember(t, f, addr, 0, 0)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	granted, err := k.AddReputationCapped(ctx, addr, "reveal", params.MaxReputationGainPerEpoch.MulInt64(3))
	require.NoError(t, err)
	require.Equal(t, params.MaxReputationGainPerEpoch.String(), granted.String())

	m := econGet(t, f, addr)
	m.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	require.NoError(t, k.Member.Set(ctx, addr.String(), m))
	_, err = k.AddReputationCapped(ctx, addr, "reveal", math.LegacyOneDec())
	require.ErrorIs(t, err, types.ErrMemberNotActive)
}

// Handoff: MembersAfter pages every member record.
func TestMembersAfterPages(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	var all []string
	after := ""
	for {
		page, done, err := k.MembersAfter(ctx, after, 2)
		require.NoError(t, err)
		all = append(all, page...)
		if done {
			break
		}
		require.Len(t, page, 2)
		after = page[len(page)-1]
	}
	count := 0
	require.NoError(t, k.Member.Walk(ctx, nil, func(string, types.Member) (bool, error) { count++; return false, nil }))
	require.Len(t, all, count)
}

// Handoff: zeroing hooks receive every address of the member.
type recordingZeroingHook struct{ got []string }

func (r *recordingZeroingHook) AfterMemberZeroed(_ context.Context, member string, addrs []string) error {
	r.got = append([]string{member}, addrs...)
	return nil
}

func TestZeroingHookCalled(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	hook := &recordingZeroingHook{}
	k.AddMemberZeroingHook(hook)
	addr := sdk.AccAddress([]byte("hook-member---------"))
	econMember(t, f, addr, 10, 0)
	require.NoError(t, k.ZeroMember(ctx, addr, "test"))
	require.Equal(t, addr.String(), hook.got[0])
	require.Contains(t, hook.got[1:], addr.String())
}
