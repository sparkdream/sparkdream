package keeper_test

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func validZkKey() []byte {
	k := make([]byte, keeper.ZkPublicKeySize)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func seedActiveMember(t *testing.T, f *fixture, addr sdk.AccAddress, status types.MemberStatus) {
	t.Helper()
	zero := math.ZeroInt()
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:        addr.String(),
		Status:         status,
		DreamBalance:   &zero,
		StakedDream:    &zero,
		LifetimeEarned: &zero,
		LifetimeBurned: &zero,
	}))
}

func TestRegisterZkPublicKey_HappyPath(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("member1"))
	seedActiveMember(t, f, addr, types.MemberStatus_MEMBER_STATUS_ACTIVE)

	key := validZkKey()
	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{
		Member:      addr.String(),
		ZkPublicKey: key,
	})
	require.NoError(t, err)

	got, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.True(t, bytes.Equal(key, got.ZkPublicKey))

	// The dirty flag should be set so EndBlocker rebuilds the trust tree.
	require.True(t, f.keeper.IsTrustTreeDirty(f.ctx))
}

func TestRegisterZkPublicKey_RejectsWrongKeySize(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("member1"))
	seedActiveMember(t, f, addr, types.MemberStatus_MEMBER_STATUS_ACTIVE)

	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{
		Member:      addr.String(),
		ZkPublicKey: []byte{1, 2, 3}, // too short
	})
	require.ErrorIs(t, err, types.ErrInvalidRequest)
}

func TestRegisterZkPublicKey_RejectsMissingMember(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{
		Member:      sdk.AccAddress([]byte("ghost")).String(),
		ZkPublicKey: validZkKey(),
	})
	require.ErrorIs(t, err, types.ErrMemberNotFound)
}

func TestRegisterZkPublicKey_RejectsInactiveMember(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("zeroed"))
	seedActiveMember(t, f, addr, types.MemberStatus_MEMBER_STATUS_ZEROED)

	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{
		Member:      addr.String(),
		ZkPublicKey: validZkKey(),
	})
	require.ErrorIs(t, err, types.ErrMemberNotActive)
}

func TestRegisterZkPublicKey_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	_, err := srv.RegisterZkPublicKey(f.ctx, &types.MsgRegisterZkPublicKey{
		Member:      "not-an-address",
		ZkPublicKey: validZkKey(),
	})
	require.Error(t, err)
}
