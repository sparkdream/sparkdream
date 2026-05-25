package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestTreasuryBalance_AddSpendEnforce(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.True(t, bal.IsZero(), "empty store should report zero balance")

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(1_000)))
	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(500)))

	bal, err = k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_500), bal)

	// Partial spend.
	spent, err := k.SpendFromTreasury(ctx, math.NewInt(300))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300), spent)

	// Over-spend is capped at remaining balance.
	spent, err = k.SpendFromTreasury(ctx, math.NewInt(10_000))
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_200), spent)

	bal, err = k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.True(t, bal.IsZero())
}

func TestEnforceTreasuryBalance_BurnsExcess(t *testing.T) {
	params := types.DefaultParams()
	params.MaxTreasuryBalance = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(1_750)))
	require.NoError(t, k.EnforceTreasuryBalance(ctx))

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), bal, "balance should be capped at max")

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(750), burned, "excess should be tracked in SeasonBurned")
}

func TestEnforceTreasuryBalance_UnderCapIsNoop(t *testing.T) {
	params := types.DefaultParams()
	params.MaxTreasuryBalance = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(500)))
	require.NoError(t, k.EnforceTreasuryBalance(ctx))

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(500), bal)

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.True(t, burned.IsZero())
}

func TestSeasonCounters_TrackMintAndBurn(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.TrackMint(ctx, math.NewInt(100)))
	require.NoError(t, k.TrackMint(ctx, math.NewInt(250)))
	require.NoError(t, k.TrackBurn(ctx, math.NewInt(40)))
	require.NoError(t, k.TrackInitiativeRewardMint(ctx, math.NewInt(300)))

	minted, err := k.GetSeasonMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(350), minted)

	burned, err := k.GetSeasonBurned(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(40), burned)

	initRewards, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(300), initRewards)
}

func TestCheckAndTrackEpochMint_RejectsZeroCap(t *testing.T) {
	// REP-S2-14: MaxDreamMintPerEpoch=0 is rejected at Validate so it can no
	// longer be used as an "unbounded" sentinel.
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.ZeroInt()
	require.Error(t, params.Validate())
}

func TestCheckAndTrackEpochMint_EnforcesCapWithinEpoch(t *testing.T) {
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.NewInt(1_000)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	// Three mints that sum to the cap should all succeed.
	require.NoError(t, k.CheckAndTrackEpochMint(ctx, math.NewInt(400)))
	require.NoError(t, k.CheckAndTrackEpochMint(ctx, math.NewInt(500)))
	require.NoError(t, k.CheckAndTrackEpochMint(ctx, math.NewInt(100)))

	// The next mint, even by 1, must fail.
	err := k.CheckAndTrackEpochMint(ctx, math.NewInt(1))
	require.ErrorIs(t, err, types.ErrDreamMintCapExceeded)
}

func TestCheckAndTrackEpochMint_SingleMintExceedingCapFails(t *testing.T) {
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.NewInt(100)
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	err := k.CheckAndTrackEpochMint(ctx, math.NewInt(101))
	require.ErrorIs(t, err, types.ErrDreamMintCapExceeded)
}

func TestPayDREAMFromTreasuryFirst_DisabledMintsAll(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	addr := sdk.AccAddress([]byte("payee_disabled__"))
	createMemberWithTrustLevel(k, ctx, addr.String(), types.TrustLevel_TRUST_LEVEL_NEW)

	// Treasury has DREAM but the flag is OFF — all 600 must be freshly minted.
	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(1_000)))

	treasuryPaid, minted, err := k.PayDREAMFromTreasuryFirst(ctx, addr, math.NewInt(600), false)
	require.NoError(t, err)
	require.True(t, treasuryPaid.IsZero())
	require.Equal(t, math.NewInt(600), minted)

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), bal, "treasury must not be touched when flag is off")

	outflow, err := k.GetSeasonTreasuryOutflow(ctx)
	require.NoError(t, err)
	require.True(t, outflow.IsZero())
}

func TestPayDREAMFromTreasuryFirst_DrainsThenMintsShortfall(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	addr := sdk.AccAddress([]byte("payee_drain_____"))
	createMemberWithTrustLevel(k, ctx, addr.String(), types.TrustLevel_TRUST_LEVEL_NEW)

	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(400)))

	treasuryPaid, minted, err := k.PayDREAMFromTreasuryFirst(ctx, addr, math.NewInt(1_000), true)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(400), treasuryPaid, "drain the 400 the treasury holds first")
	require.Equal(t, math.NewInt(600), minted, "mint only the shortfall")

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.True(t, bal.IsZero(), "treasury fully drained")

	outflow, err := k.GetSeasonTreasuryOutflow(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(400), outflow)
}

func TestPayDREAMFromTreasuryFirst_FiresReferralOnTotal(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Inviter has invitation credit; invitee is registered via AcceptInvitation
	// so the invitation is in the ACCEPTED state with an unexpired referral period.
	inviter := sdk.AccAddress([]byte("inviter_treasur_"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1_000_000_000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 1,
	})

	invitee := sdk.AccAddress([]byte("invitee_treasur_"))
	invitationID, err := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100_000_000), []string{"tag"})
	require.NoError(t, err)
	require.NoError(t, k.AcceptInvitation(ctx, invitationID, invitee))

	inviterBefore, _ := k.Member.Get(ctx, inviter.String())
	initialBalance := *inviterBefore.DreamBalance

	// Fund treasury so the payment is FULLY covered by a treasury draw — no
	// fresh mint hits the recipient, but the inviter must still see the
	// 5% referral reward on the full payment.
	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(10_000)))

	const payment = int64(5_000)
	treasuryPaid, minted, err := k.PayDREAMFromTreasuryFirst(ctx, invitee, math.NewInt(payment), true)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(payment), treasuryPaid, "treasury fully covered the payment")
	require.True(t, minted.IsZero(), "no fresh mint when treasury covers the whole amount")

	// Inviter should have earned 5% of the full payment (not 5% of the
	// minted shortfall, which is zero in this scenario).
	inviterAfter, _ := k.Member.Get(ctx, inviter.String())
	expectedReward := math.LegacyNewDecWithPrec(5, 2).MulInt(math.NewInt(payment)).TruncateInt()
	require.Equal(
		t,
		initialBalance.Add(expectedReward).String(),
		inviterAfter.DreamBalance.String(),
		"inviter referral must fire on total payment, not just the minted shortfall",
	)

	invitation, _ := k.Invitation.Get(ctx, invitationID)
	require.Equal(t, expectedReward.String(), invitation.ReferralEarned.String())
}

func TestPayDREAMFromTreasuryFirst_PartialCoverageReferralStillOnTotal(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	inviter := sdk.AccAddress([]byte("inviter_partial_"))
	k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(1_000_000_000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  make(map[string]string),
		InvitationCredits: 1,
	})

	invitee := sdk.AccAddress([]byte("invitee_partial_"))
	invitationID, err := k.CreateInvitation(ctx, inviter, invitee, math.NewInt(100_000_000), []string{"tag"})
	require.NoError(t, err)
	require.NoError(t, k.AcceptInvitation(ctx, invitationID, invitee))

	inviterBefore, _ := k.Member.Get(ctx, inviter.String())
	initialBalance := *inviterBefore.DreamBalance

	// Treasury covers 40%; remaining 60% is freshly minted.
	require.NoError(t, k.AddToTreasury(ctx, math.NewInt(2_000)))

	const payment = int64(5_000)
	treasuryPaid, minted, err := k.PayDREAMFromTreasuryFirst(ctx, invitee, math.NewInt(payment), true)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(2_000), treasuryPaid)
	require.Equal(t, math.NewInt(3_000), minted)

	// Referral must be 5% of the FULL 5000, not 5% of just the 3000 minted —
	// otherwise the inviter is silently underpaid whenever treasury covers
	// part of a payment.
	inviterAfter, _ := k.Member.Get(ctx, inviter.String())
	expectedReward := math.LegacyNewDecWithPrec(5, 2).MulInt(math.NewInt(payment)).TruncateInt()
	require.Equal(t, initialBalance.Add(expectedReward).String(), inviterAfter.DreamBalance.String())
}

func TestMintToTreasury_TracksInflowAndSeasonMinted(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	require.NoError(t, k.MintToTreasury(ctx, math.NewInt(2_500)))

	bal, err := k.GetTreasuryBalance(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(2_500), bal)

	inflow, err := k.GetSeasonTreasuryInflow(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(2_500), inflow)

	seasonMinted, err := k.GetSeasonMinted(ctx)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(2_500), seasonMinted, "treasury mint counts toward global mint counter")
}

func TestCheckAndTrackEpochMint_CounterResetsOnNewEpoch(t *testing.T) {
	params := types.DefaultParams()
	params.MaxDreamMintPerEpoch = math.NewInt(1_000)
	params.EpochBlocks = 10 // small epoch for easy block-height manipulation
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper
	ctx := f.ctx

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Epoch 0: mint up to cap.
	ctx0 := sdkCtx.WithBlockHeight(1)
	require.NoError(t, k.CheckAndTrackEpochMint(ctx0, math.NewInt(1_000)))
	require.ErrorIs(t, k.CheckAndTrackEpochMint(ctx0, math.NewInt(1)), types.ErrDreamMintCapExceeded)

	// Epoch 1: budget resets.
	ctx1 := sdkCtx.WithBlockHeight(15)
	require.NoError(t, k.CheckAndTrackEpochMint(ctx1, math.NewInt(700)))
	require.NoError(t, k.CheckAndTrackEpochMint(ctx1, math.NewInt(300)))
	require.ErrorIs(t, k.CheckAndTrackEpochMint(ctx1, math.NewInt(1)), types.ErrDreamMintCapExceeded)

	// Epoch 2: budget resets again.
	ctx2 := sdkCtx.WithBlockHeight(25)
	require.NoError(t, k.CheckAndTrackEpochMint(ctx2, math.NewInt(1_000)))
}
