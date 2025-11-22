package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestSetPrimary(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Setup users with valid addresses
	// We create valid AccAddress from bytes to ensure they pass sdk.AccAddressFromBech32 validation
	userAliceAddr := sdk.AccAddress([]byte("alice_test_account_1"))
	userAlice := userAliceAddr.String()

	userBobAddr := sdk.AccAddress([]byte("bob_test_account_1__"))
	userBob := userBobAddr.String()

	// Setup names
	// Alice owns "alice"
	err := f.keeper.Names.Set(f.ctx, "alice", types.NameRecord{
		Name:  "alice",
		Owner: userAlice,
	})
	require.NoError(t, err)
	// Ensure OwnerInfo exists for Alice
	err = f.keeper.Owners.Set(f.ctx, userAlice, types.OwnerInfo{Address: userAlice})
	require.NoError(t, err)

	// Bob owns "bob"
	err = f.keeper.Names.Set(f.ctx, "bob", types.NameRecord{
		Name:  "bob",
		Owner: userBob,
	})
	require.NoError(t, err)
	// Ensure OwnerInfo exists for Bob
	err = f.keeper.Owners.Set(f.ctx, userBob, types.OwnerInfo{Address: userBob})
	require.NoError(t, err)

	tests := []struct {
		desc      string
		msg       *types.MsgSetPrimary
		checkInfo func(t *testing.T)
		err       error
	}{
		{
			desc: "Success - Alice sets primary to 'alice'",
			msg: &types.MsgSetPrimary{
				Authority: userAlice,
				Name:      "alice",
			},
			checkInfo: func(t *testing.T) {
				info, err := f.keeper.Owners.Get(f.ctx, userAlice)
				require.NoError(t, err)
				require.Equal(t, "alice", info.PrimaryName)
			},
		},
		{
			desc: "Failure - Alice tries to set name 'bob' (Owned by Bob)",
			msg: &types.MsgSetPrimary{
				Authority: userAlice,
				Name:      "bob",
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Alice tries to set non-existent name",
			msg: &types.MsgSetPrimary{
				Authority: userAlice,
				Name:      "ghost",
			},
			err: types.ErrNameNotFound,
		},
		{
			desc: "Failure - Invalid Address",
			msg: &types.MsgSetPrimary{
				Authority: "invalid_address",
				Name:      "alice",
			},
			err: sdkerrors.ErrInvalidAddress,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := ms.SetPrimary(f.ctx, tc.msg)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				if tc.checkInfo != nil {
					tc.checkInfo(t)
				}
			}
		})
	}
}
