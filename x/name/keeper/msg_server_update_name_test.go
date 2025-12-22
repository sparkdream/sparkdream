package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

func TestUpdateName(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx
	ms := keeper.NewMsgServerImpl(k)

	// Define Users
	aliceAddr := sdk.AccAddress([]byte("alice_test_address__"))
	alice := aliceAddr.String()

	bobAddr := sdk.AccAddress([]byte("bob_test_address____"))
	bob := bobAddr.String()

	tests := []struct {
		desc      string
		msg       *types.MsgUpdateName
		runBefore func(sdk.Context)
		check     func(t *testing.T, ctx sdk.Context)
		err       error
		errCode   codes.Code
	}{
		{
			desc: "Success - Owner Updates Metadata",
			msg: &types.MsgUpdateName{
				Creator: alice,
				Name:    "alice",
				Data:    "ipfs://NewCIDv1",
			},
			runBefore: func(c sdk.Context) {
				// Alice owns "alice" with old data
				_ = k.Names.Set(c, "alice", types.NameRecord{Name: "alice", Owner: alice, Data: "old_data"})
			},
			check: func(t *testing.T, c sdk.Context) {
				rec, err := k.Names.Get(c, "alice")
				require.NoError(t, err)
				require.Equal(t, "ipfs://NewCIDv1", rec.Data)
				require.Equal(t, alice, rec.Owner)
			},
		},
		{
			desc: "Failure - Name Does Not Exist",
			msg: &types.MsgUpdateName{
				Creator: alice,
				Name:    "ghost_name",
				Data:    "meta",
			},
			runBefore: func(c sdk.Context) {
				// No setup needed
			},
			err: types.ErrNameNotFound,
		},
		{
			desc: "Failure - Unauthorized (Wrong Owner)",
			msg: &types.MsgUpdateName{
				Creator: bob, // Bob tries to update Alice's name
				Name:    "alice",
				Data:    "hacked_data",
			},
			runBefore: func(c sdk.Context) {
				_ = k.Names.Set(c, "alice", types.NameRecord{Name: "alice", Owner: alice, Data: "secure_data"})
			},
			// Expect generic Unauthorized error
			err: sdkerrors.ErrUnauthorized,
			check: func(t *testing.T, c sdk.Context) {
				// Verify data was NOT changed
				rec, err := k.Names.Get(c, "alice")
				require.NoError(t, err)
				require.Equal(t, "secure_data", rec.Data)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			// Use a cached context so tests don't pollute each other
			cacheCtx, _ := ctx.CacheContext()

			if tc.runBefore != nil {
				tc.runBefore(cacheCtx)
			}

			_, err := ms.UpdateName(cacheCtx, tc.msg)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if tc.errCode != codes.OK {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tc.errCode, st.Code())
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t, cacheCtx)
				}
			}
		})
	}
}
