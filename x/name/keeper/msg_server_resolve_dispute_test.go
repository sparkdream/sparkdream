package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// --- Test Suite ---

func TestResolveDispute(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	// Define Actors (Generate VALID addresses)
	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	squatterAddr := sdk.AccAddress([]byte("squatter_address____"))
	newOwnerAddr := sdk.AccAddress([]byte("new_owner_address___"))

	claimant := claimantAddr.String()
	squatter := squatterAddr.String()
	newOwner := newOwnerAddr.String()
	name := "alice"

	// Initialize the Commons Keeper mock with the expected Council lookup key/value
	// The handler expects "Commons Council" to exist and return the policy address.
	f.mockCommons.ExtendedGroups["Commons Council"] = commonstypes.ExtendedGroup{
		PolicyAddress: f.councilAddr,
	}

	// Setup Initial State
	// 1. Create the Name (Owned by Squatter)
	record := types.NameRecord{
		Name:  name,
		Owner: squatter,
	}
	err := f.keeper.Names.Set(f.ctx, name, record)
	require.NoError(t, err)

	// 2. Index it (Important for the success check later)
	err = f.keeper.OwnerNames.Set(f.ctx, collections.Join(squatter, name))
	require.NoError(t, err)

	// 3. Create the Dispute (Paid by Claimant)
	dispute := types.Dispute{
		Name:     name,
		Claimant: claimant,
	}
	err = f.keeper.Disputes.Set(f.ctx, name, dispute)
	require.NoError(t, err)

	tests := []struct {
		desc    string
		msg     *types.MsgResolveDispute
		check   func(t *testing.T, ctx sdk.Context)
		err     error
		errCode codes.Code
	}{
		{
			desc: "Failure - Unauthorized (Signer is not Council Policy)",
			msg: &types.MsgResolveDispute{
				Authority: claimant, // NOT the council policy address
				Name:      name,
				NewOwner:  newOwner,
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Dispute Not Found (Fee not paid)",
			msg: &types.MsgResolveDispute{
				Authority: f.councilAddr,
				Name:      "nonexistent_dispute",
				NewOwner:  newOwner,
			},
			err: types.ErrDisputeNotFound,
		},
		{
			desc: "Success - Dispute Resolved",
			msg: &types.MsgResolveDispute{
				Authority: f.councilAddr,
				Name:      name,
				NewOwner:  newOwner,
			},
			check: func(t *testing.T, ctx sdk.Context) {
				// 1. Check Old Owner lost name
				count, err := f.keeper.GetOwnedNamesCount(ctx, squatterAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(0), count, "Squatter should have 0 names")

				// 2. Check New Owner got name
				countNew, err := f.keeper.GetOwnedNamesCount(ctx, newOwnerAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(1), countNew, "New Owner should have 1 name")

				// 3. Check Dispute is deleted
				_, found := f.keeper.GetDispute(ctx, name)
				require.False(t, found, "Dispute should be deleted")

				// 4. Check Record updated
				updatedRecord, found := f.keeper.GetName(ctx, name)
				require.True(t, found)
				require.Equal(t, newOwner, updatedRecord.Owner, "Owner should be updated in record")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			// We execute on a cached context so state changes don't affect other tests
			cacheCtx, _ := f.ctx.CacheContext()

			_, err := ms.ResolveDispute(cacheCtx, tc.msg)

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
