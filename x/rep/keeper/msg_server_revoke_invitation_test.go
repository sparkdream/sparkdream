package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestRevokeInvitation(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := sdk.UnwrapSDKContext(f.ctx)
	srv := keeper.NewMsgServerImpl(k)
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	inviter := sdk.AccAddress([]byte("inviter_revoke_1____"))
	stranger := sdk.AccAddress([]byte("stranger_revoke_1___"))
	invitee := sdk.AccAddress([]byte("invitee_revoke_1____"))
	for _, a := range []sdk.AccAddress{inviter, stranger} {
		require.NoError(t, k.Member.Set(ctx, a.String(), types.Member{
			Address:           a.String(),
			DreamBalance:      PtrInt(math.NewInt(2_000_000_000)),
			StakedDream:       PtrInt(math.ZeroInt()),
			LifetimeEarned:    PtrInt(math.ZeroInt()),
			LifetimeBurned:    PtrInt(math.ZeroInt()),
			ReputationScores:  map[string]string{},
			InvitationCredits: 3,
			TrustLevel:        types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
			Status:            types.MemberStatus_MEMBER_STATUS_ACTIVE,
		}))
	}

	invID, err := k.CreateInvitation(ctx, inviter, invitee, params.MinInvitationStake, nil)
	require.NoError(t, err)

	// Only the inviter may revoke.
	_, err = srv.RevokeInvitation(ctx, &types.MsgRevokeInvitation{Inviter: stranger.String(), InvitationId: invID})
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = srv.RevokeInvitation(ctx, &types.MsgRevokeInvitation{Inviter: inviter.String(), InvitationId: 999})
	require.ErrorIs(t, err, types.ErrInvitationNotFound)

	_, err = srv.RevokeInvitation(ctx, &types.MsgRevokeInvitation{Inviter: inviter.String(), InvitationId: invID})
	require.NoError(t, err)

	inv, err := k.Invitation.Get(ctx, invID)
	require.NoError(t, err)
	require.Equal(t, types.InvitationStatus_INVITATION_STATUS_REVOKED, inv.Status)
	m, err := k.Member.Get(ctx, inviter.String())
	require.NoError(t, err)
	require.True(t, m.StakedDream.IsZero(), "stake unlocked")
	require.Equal(t, uint32(3), m.InvitationCredits, "credit restored")
	_, err = k.InvitationsByInvitee.Get(ctx, invitee.String())
	require.Error(t, err, "invitee slot freed")

	// Revoking twice, or accepting a revoked invitation, is refused.
	_, err = srv.RevokeInvitation(ctx, &types.MsgRevokeInvitation{Inviter: inviter.String(), InvitationId: invID})
	require.ErrorIs(t, err, types.ErrInvitationNotPending)
	require.ErrorIs(t, k.AcceptInvitation(ctx, invID, invitee, "CC0-1.0"), types.ErrInvitationNotPending)

	// The invitee can be invited again.
	_, err = k.CreateInvitation(ctx, inviter, invitee, params.MinInvitationStake, nil)
	require.NoError(t, err)
}
