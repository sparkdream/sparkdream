package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestPendingInvitationExpires: a never-accepted invitation unlocks the
// inviter's stake, restores the credit and frees the invitee address once
// the acceptance window has passed and an epoch ends.
func TestPendingInvitationExpires(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := sdk.UnwrapSDKContext(f.ctx)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.InvitationAcceptanceEpochs = 2
	require.NoError(t, k.Params.Set(ctx, params))

	inviter := sdk.AccAddress([]byte("inviter_expiry_1____"))
	invitee := sdk.AccAddress([]byte("invitee_expiry_1____"))
	require.NoError(t, k.Member.Set(ctx, inviter.String(), types.Member{
		Address:           inviter.String(),
		DreamBalance:      PtrInt(math.NewInt(2_000_000_000)),
		StakedDream:       PtrInt(math.ZeroInt()),
		LifetimeEarned:    PtrInt(math.ZeroInt()),
		LifetimeBurned:    PtrInt(math.ZeroInt()),
		ReputationScores:  map[string]string{},
		InvitationCredits: 3,
		TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		Status:            types.MemberStatus_MEMBER_STATUS_ACTIVE,
	}))

	stake := k.RequiredInvitationStakeForTest(ctx, inviter)
	invID, err := k.CreateInvitation(ctx, inviter, invitee, stake, nil)
	require.NoError(t, err)

	m, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.Equal(t, uint32(2), m.InvitationCredits)
	require.True(t, m.StakedDream.IsPositive(), "stake locked while pending")

	// Before the window: nothing happens, even at an epoch end.
	epochEnd := ctx.WithBlockHeight(params.EpochBlocks)
	require.NoError(t, k.ProcessExpiredAccountability(epochEnd))
	inv, err := k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_PENDING, inv.Status)

	// REP-E-16: the window is counted in epochs (block height), not time. A
	// large block-time jump at the last epoch end inside the window changes
	// nothing.
	window := time.Duration(params.InvitationAcceptanceEpochs*params.EpochBlocks*6+1) * time.Second
	late := ctx.WithBlockTime(ctx.BlockTime().Add(10 * window)).WithBlockHeight(2 * params.EpochBlocks)
	require.NoError(t, k.ProcessExpiredAccountability(late))
	inv, err = k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_PENDING, inv.Status)

	// Past the window but not at an epoch end: still pending.
	notEnd := ctx.WithBlockHeight(3*params.EpochBlocks + 1)
	require.NoError(t, k.ProcessExpiredAccountability(notEnd))
	inv, err = k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_PENDING, inv.Status)

	// Past the window at an epoch end, whatever the block time: expired.
	lateEpochEnd := ctx.WithBlockHeight(3 * params.EpochBlocks)
	require.NoError(t, k.ProcessExpiredAccountability(lateEpochEnd))
	inv, err = k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_EXPIRED, inv.Status)

	m, err = k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.True(t, m.StakedDream.IsZero(), "stake unlocked, got %s", m.StakedDream)
	require.Equal(t, uint32(3), m.InvitationCredits, "credit restored")

	_, err = k.InvitationsByInvitee.Get(ctx, invitee.String())
	require.Error(t, err, "invitee slot freed")

	// The same invitee can be invited again.
	_, err = k.CreateInvitation(ctx, inviter, invitee, stake, nil)
	require.NoError(t, err)

	// Disabled window never expires anything.
	params.InvitationAcceptanceEpochs = 0
	require.NoError(t, k.Params.Set(ctx, params))
	farFuture := lateEpochEnd.WithBlockTime(lateEpochEnd.BlockTime().Add(365 * 24 * time.Hour))
	require.NoError(t, k.ProcessExpiredAccountability(farFuture))
	byInvitee, err := k.InvitationsByInvitee.Get(ctx, invitee.String())
	require.NoError(t, err)
	inv, err = k.Invitation.Get(ctx, byInvitee)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_PENDING, inv.Status)
}

// REP-E-16: the inviter accountability window is counted in epochs. An
// invitation whose (estimated) unix-time end is long past still holds the
// inviter accountable while the epoch is inside accountability_end_epoch,
// and stops once the epoch passes it, whatever the block time.
func TestInviterAccountabilityCountsEpochs(t *testing.T) {
	run := func(t *testing.T, height int64) math.Int {
		f := initFixture(t)
		k := f.keeper
		params, err := k.Params.Get(f.ctx)
		require.NoError(t, err)
		ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(height * params.EpochBlocks)
		inviter := sdk.AccAddress([]byte("inviter_epochs______"))
		invitee := sdk.AccAddress([]byte("invitee_epochs______"))
		for _, a := range []sdk.AccAddress{inviter, invitee} {
			require.NoError(t, k.Member.Set(ctx, a.String(), types.Member{
				Address:          a.String(),
				DreamBalance:     PtrInt(math.NewInt(2_000_000_000)),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{},
				InvitedBy:        inviter.String(),
			}))
		}
		require.NoError(t, k.Invitation.Set(ctx, 1, types.Invitation{
			Id:                     1,
			Inviter:                inviter.String(),
			InviteeAddress:         invitee.String(),
			StakedDream:            PtrInt(math.NewInt(100_000_000)),
			AccountabilityEnd:      ctx.BlockTime().Unix() - 3600, // estimate long past
			AccountabilityEndEpoch: 5,
			Status:                 types.InvitationStatus_INVITATION_STATUS_ACCEPTED,
			ReferralEarned:         PtrInt(math.ZeroInt()),
		}))
		require.NoError(t, k.InvitationsByInvitee.Set(ctx, invitee.String(), 1))
		require.NoError(t, k.ProcessInviterAccountability(ctx, invitee, "zenith"))
		m, err := k.Member.Get(ctx, inviter.String())
		require.NoError(t, err)
		return *m.DreamBalance
	}
	require.True(t, run(t, 5).LT(math.NewInt(2_000_000_000)), "epoch 5 is inside the window")
	require.Equal(t, "2000000000", run(t, 6).String(), "epoch 6 is past the window")
}
