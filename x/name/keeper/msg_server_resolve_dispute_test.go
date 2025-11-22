package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	"cosmossdk.io/math"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectestutil "github.com/cosmos/cosmos-sdk/codec/testutil"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/group"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// --- Mocks ---

type mockGroupKeeperDispute struct {
	policyAddr string
}

func (m mockGroupKeeperDispute) GroupPoliciesByGroup(ctx context.Context, request *group.QueryGroupPoliciesByGroupRequest) (*group.QueryGroupPoliciesByGroupResponse, error) {
	// Simulate finding the policy for Group 1
	if request.GroupId != 1 {
		return &group.QueryGroupPoliciesByGroupResponse{}, nil
	}
	return &group.QueryGroupPoliciesByGroupResponse{
		GroupPolicies: []*group.GroupPolicyInfo{
			{
				Address:  m.policyAddr,
				Metadata: "standard",
			},
		},
	}, nil
}

func (m mockGroupKeeperDispute) GroupsByMember(ctx context.Context, request *group.QueryGroupsByMemberRequest) (*group.QueryGroupsByMemberResponse, error) {
	return &group.QueryGroupsByMemberResponse{Groups: []*group.GroupInfo{}}, nil
}

// Added to satisfy interface
func (m mockGroupKeeperDispute) GetGroupSequence(ctx sdk.Context) uint64 {
	return 1
}

type mockBankKeeperDispute struct{}

func (m mockBankKeeperDispute) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}
func (m mockBankKeeperDispute) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return nil
}

func (m mockBankKeeperDispute) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000000)))
}

// --- Setup Helper ---

// setupKeeperWithMock builds a Keeper from scratch, allowing us to inject the mockGroupKeeperDispute.
func setupKeeperWithMock(t *testing.T) (keeper.Keeper, sdk.Context, string) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey("mem_name")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, storetypes.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())

	cdc := codectestutil.CodecOptions{}.NewCodec()

	// Use "cosmos" prefix for tests to match SDK defaults
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	// Create a mock council address
	councilAddr := sdk.AccAddress([]byte("council_policy_addr_"))

	mockGK := mockGroupKeeperDispute{policyAddr: councilAddr.String()}
	mockBK := mockBankKeeperDispute{}

	// Create the StoreService using the runtime package (Standard for SDK v0.50)
	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		mockBK,
		mockGK,
	)

	// Initialize Params
	err := k.Params.Set(ctx, types.DefaultParams())
	require.NoError(t, err)

	return k, ctx, councilAddr.String()
}

// --- Test Suite ---

func TestResolveDispute(t *testing.T) {
	k, ctx, councilAddr := setupKeeperWithMock(t)
	ms := keeper.NewMsgServerImpl(k)

	// Define Actors (Generate VALID addresses)
	claimantAddr := sdk.AccAddress([]byte("claimant_address____"))
	claimant := claimantAddr.String()

	squatterAddr := sdk.AccAddress([]byte("squatter_address____"))
	squatter := squatterAddr.String()

	newOwnerAddr := sdk.AccAddress([]byte("new_owner_address___"))
	newOwner := newOwnerAddr.String()

	name := "alice"

	// Setup Initial State
	// 1. Create the Name (Owned by Squatter)
	record := types.NameRecord{
		Name:  name,
		Owner: squatter,
	}
	err := k.Names.Set(ctx, name, record)
	require.NoError(t, err)

	// 2. Index it (Important for the success check later)
	err = k.OwnerNames.Set(ctx, collections.Join(squatter, name))
	require.NoError(t, err)

	// 3. Create the Dispute (Paid by Claimant)
	dispute := types.Dispute{
		Name:     name,
		Claimant: claimant,
	}
	err = k.Disputes.Set(ctx, name, dispute)
	require.NoError(t, err)

	tests := []struct {
		desc    string
		msg     *types.MsgResolveDispute
		check   func(t *testing.T)
		err     error
		errCode codes.Code
	}{
		{
			desc: "Failure - Unauthorized (Signer is not Council)",
			msg: &types.MsgResolveDispute{
				Authority: claimant, // NOT the council
				Name:      name,
				NewOwner:  newOwner,
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Dispute Not Found (Fee not paid)",
			msg: &types.MsgResolveDispute{
				Authority: councilAddr,
				Name:      "nonexistent_dispute",
				NewOwner:  newOwner,
			},
			err: types.ErrDisputeNotFound,
		},
		{
			desc: "Success - Dispute Resolved",
			msg: &types.MsgResolveDispute{
				Authority: councilAddr,
				Name:      name,
				NewOwner:  newOwner,
			},
			check: func(t *testing.T) {
				// 1. Check Old Owner lost name
				count, err := k.GetOwnedNamesCount(ctx, squatterAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(0), count, "Squatter should have 0 names")

				// 2. Check New Owner got name
				countNew, err := k.GetOwnedNamesCount(ctx, newOwnerAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(1), countNew, "New Owner should have 1 name")

				// 3. Check Dispute is deleted
				_, found := k.GetDispute(ctx, name)
				require.False(t, found, "Dispute should be deleted")

				// 4. Check Record updated
				updatedRecord, found := k.GetName(ctx, name)
				require.True(t, found)
				require.Equal(t, newOwner, updatedRecord.Owner, "Owner should be updated in record")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			// We execute on a cached context so state changes don't affect other tests
			cacheCtx, _ := ctx.CacheContext()

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
					// Run checks using the cached context to verify state changes
					if tc.desc == "Success - Dispute Resolved" {
						// 1. Check Old Owner lost name
						count, err := k.GetOwnedNamesCount(cacheCtx, squatterAddr)
						require.NoError(t, err)
						require.Equal(t, uint64(0), count, "Squatter should have 0 names")

						// 2. Check New Owner got name
						countNew, err := k.GetOwnedNamesCount(cacheCtx, newOwnerAddr)
						require.NoError(t, err)
						require.Equal(t, uint64(1), countNew, "New Owner should have 1 name")

						// 3. Check Dispute is deleted
						_, found := k.GetDispute(cacheCtx, name)
						require.False(t, found, "Dispute should be deleted")

						// 4. Check Record updated
						updatedRecord, found := k.GetName(cacheCtx, name)
						require.True(t, found)
						require.Equal(t, newOwner, updatedRecord.Owner, "Owner should be updated in record")
					}
				}
			}
		})
	}
}
