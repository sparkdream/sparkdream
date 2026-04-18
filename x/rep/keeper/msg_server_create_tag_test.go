package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// seedEstablishedMember creates a member with trust level ESTABLISHED and a
// balance sufficient to cover the tag creation fee.
func seedEstablishedMember(t *testing.T, f *fixture, addr sdk.AccAddress, balance math.Int) {
	t.Helper()
	zero := math.ZeroInt()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:        addr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   &balance,
		StakedDream:    &zero,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	}))
}

func TestCreateTag_HappyPath(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("tagmaker"))
	seedEstablishedMember(t, f, addr, math.NewInt(10_000))

	resp, err := srv.CreateTag(f.ctx, &types.MsgCreateTag{
		Creator: addr.String(),
		Name:    "newtag",
	})
	require.NoError(t, err)
	require.Equal(t, "newtag", resp.Name)

	tag, err := f.keeper.GetTag(f.ctx, "newtag")
	require.NoError(t, err)
	require.Greater(t, tag.ExpirationIndex, tag.CreatedAt, "new tags must carry an expiration in the future")
}

func TestCreateTag_RejectsInvalidFormat(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("tagmaker"))
	seedEstablishedMember(t, f, addr, math.NewInt(10_000))

	_, err := srv.CreateTag(f.ctx, &types.MsgCreateTag{
		Creator: addr.String(),
		Name:    "UPPERCASE_Invalid", // tag format rejects uppercase
	})
	require.ErrorIs(t, err, types.ErrInvalidTagName)
}

func TestCreateTag_RejectsDuplicate(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("tagmaker"))
	seedEstablishedMember(t, f, addr, math.NewInt(10_000))

	// The fixture pre-seeds "backend" — a second attempt must fail.
	_, err := srv.CreateTag(f.ctx, &types.MsgCreateTag{
		Creator: addr.String(),
		Name:    "backend",
	})
	require.ErrorIs(t, err, types.ErrTagAlreadyExists)
}

func TestCreateTag_RejectsReserved(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("tagmaker"))
	seedEstablishedMember(t, f, addr, math.NewInt(10_000))

	require.NoError(t, f.keeper.SetReservedTag(f.ctx, types.ReservedTag{Name: "admin"}))

	_, err := srv.CreateTag(f.ctx, &types.MsgCreateTag{
		Creator: addr.String(),
		Name:    "admin",
	})
	require.ErrorIs(t, err, types.ErrReservedTagName)
}

func TestCreateTag_RejectsLowTrust(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("noob"))
	zero := math.ZeroInt()
	balance := math.NewInt(10_000)
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:        addr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   &balance,
		StakedDream:    &zero,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_NEW,
	}))

	_, err := srv.CreateTag(f.ctx, &types.MsgCreateTag{
		Creator: addr.String(),
		Name:    "sometag",
	})
	require.ErrorIs(t, err, types.ErrInsufficientTrustLevel)
}

func TestCreateTag_UnknownMember(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	_, err := srv.CreateTag(f.ctx, &types.MsgCreateTag{
		Creator: sdk.AccAddress([]byte("ghost")).String(),
		Name:    "any",
	})
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}
