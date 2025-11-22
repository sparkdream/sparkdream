package keeper_test

import (
	"context"
	"testing"
	"time"

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

// --- Mocks for RegisterName ---

type mockGroupKeeperReg struct {
	members map[string]bool
}

func (m mockGroupKeeperReg) GroupsByMember(ctx context.Context, request *group.QueryGroupsByMemberRequest) (*group.QueryGroupsByMemberResponse, error) {
	// If address is marked as member in our map, return a list containing Group ID 1 (Default Council)
	if m.members[request.Address] {
		return &group.QueryGroupsByMemberResponse{
			Groups: []*group.GroupInfo{
				{Id: 1}, // Default Council ID
			},
		}, nil
	}
	// Otherwise return empty list
	return &group.QueryGroupsByMemberResponse{Groups: []*group.GroupInfo{}}, nil
}

// Stubs to satisfy interface
func (m mockGroupKeeperReg) GroupPoliciesByGroup(ctx context.Context, request *group.QueryGroupPoliciesByGroupRequest) (*group.QueryGroupPoliciesByGroupResponse, error) {
	return nil, nil
}

// Added to satisfy interface
func (m mockGroupKeeperReg) GetGroupSequence(ctx sdk.Context) uint64 {
	return 1
}

type mockBankKeeperReg struct {
}

func (m mockBankKeeperReg) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}

func (m mockBankKeeperReg) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	// Simple mock: verify user has enough (we don't actually deduct in this test struct unless we track it, just return nil or err)
	// For this test, we assume everyone has funds unless we want to test failure.
	// Let's simulate "insufficient funds" if the amount is huge.
	if amt.AmountOf("uspark").GT(math.NewInt(1000000000)) {
		return sdkerrors.ErrInsufficientFunds
	}
	return nil
}

// Added to satisfy interface
func (m mockBankKeeperReg) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000000000)))
}

// --- Setup Helper ---

func setupKeeperForRegister(t *testing.T) (keeper.Keeper, sdk.Context, *mockGroupKeeperReg) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey("mem_name")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, storetypes.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())
	// Set block time for expiration tests
	ctx = ctx.WithBlockTime(time.Now())

	cdc := codectestutil.CodecOptions{}.NewCodec()
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	mockGK := &mockGroupKeeperReg{members: make(map[string]bool)}
	mockBK := mockBankKeeperReg{}

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
	params := types.DefaultParams()
	params.BlockedNames = []string{"admin", "blocked"}
	params.MinNameLength = 3
	params.MaxNameLength = 30 // Aligned with keys.go
	params.MaxNamesPerAddress = 2
	params.RegistrationFee = sdk.NewCoin("uspark", math.NewInt(10))
	// Short expiration for testing
	params.ExpirationDuration = time.Hour * 24
	err := k.Params.Set(ctx, params)
	require.NoError(t, err)

	return k, ctx, mockGK
}

// --- Test Suite ---

func TestRegisterName(t *testing.T) {
	k, ctx, mockGK := setupKeeperForRegister(t)
	ms := keeper.NewMsgServerImpl(k)

	// Define Users with valid Bech32 generation
	aliceAddr := sdk.AccAddress([]byte("alice_test_address__"))
	alice := aliceAddr.String()

	bobAddr := sdk.AccAddress([]byte("bob_test_address____"))
	bob := bobAddr.String()

	// Alice is a Council Member
	mockGK.members[alice] = true
	// Bob is NOT a Council Member
	mockGK.members[bob] = false

	// Valid Registration Data
	validMsg := &types.MsgRegisterName{
		Authority: alice,
		Name:      "alice",
		Data:      "meta",
	}

	tests := []struct {
		desc      string
		msg       *types.MsgRegisterName
		runBefore func()
		check     func(t *testing.T)
		err       error
		errCode   codes.Code
	}{
		{
			desc: "Success - Valid Registration",
			msg:  validMsg,
			check: func(t *testing.T) {
				// Check Name Record
				rec, found := k.GetName(ctx, "alice")
				require.True(t, found)
				require.Equal(t, alice, rec.Owner)

				// Check Owner Info
				count, err := k.GetOwnedNamesCount(ctx, aliceAddr)
				require.NoError(t, err)
				require.Equal(t, uint64(1), count)

				// Check Primary Name (First name set as primary)
				info, err := k.Owners.Get(ctx, alice)
				require.NoError(t, err)
				require.Equal(t, "alice", info.PrimaryName)
			},
		},
		{
			desc: "Failure - Name Too Short",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "al",
			},
			err: types.ErrInvalidName,
		},
		{
			desc: "Failure - Name Too Long",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "verylongnameverylongnameverylongname", // > 30
			},
			err: types.ErrInvalidName,
		},
		{
			desc: "Failure - Blocked Name",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "admin",
			},
			err: types.ErrNameReserved,
		},
		{
			desc: "Failure - Not Council Member",
			msg: &types.MsgRegisterName{
				Authority: bob, // Not in mockGK
				Name:      "bobby",
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Name Already Taken (Active)",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "active_name",
			},
			runBefore: func() {
				// Set an active name owned by Bob (who we will simulate as active)
				_ = k.Names.Set(ctx, "active_name", types.NameRecord{Name: "active_name", Owner: bob})
				_ = k.OwnerNames.Set(ctx, collections.Join(bob, "active_name"))

				// Set Bob as active RECENTLY (so not expired)
				_ = k.Owners.Set(ctx, bob, types.OwnerInfo{
					Address:        bob,
					LastActiveTime: ctx.BlockTime().Unix(),
				})
			},
			err: types.ErrNameTaken,
		},
		{
			desc: "Success - Scavenge Expired Name",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "expired_name",
			},
			runBefore: func() {
				// Set a name owned by Bob
				_ = k.Names.Set(ctx, "expired_name", types.NameRecord{Name: "expired_name", Owner: bob})
				_ = k.OwnerNames.Set(ctx, collections.Join(bob, "expired_name"))

				// Make Bob expired
				// Expiration is 24 hours. Set LastActive to 25 hours ago.
				expiredTime := ctx.BlockTime().Add(-25 * time.Hour).Unix()
				_ = k.Owners.Set(ctx, bob, types.OwnerInfo{
					Address:        bob,
					LastActiveTime: expiredTime,
				})
			},
			check: func(t *testing.T) {
				rec, found := k.GetName(ctx, "expired_name")
				require.True(t, found)
				require.Equal(t, alice, rec.Owner, "Alice should have stolen the name")
			},
		},
		{
			desc: "Failure - Max Names Limit Reached",
			msg: &types.MsgRegisterName{
				Authority: alice,
				Name:      "name3",
			},
			runBefore: func() {
				// Alice already has "alice" from first test.
				// Add "name2". That makes 2 names. Limit is 2.
				// Trying to add "name3" should fail.
				_ = k.Names.Set(ctx, "name2", types.NameRecord{Name: "name2", Owner: alice})
				_ = k.OwnerNames.Set(ctx, collections.Join(alice, "name2"))
			},
			err: types.ErrTooManyNames,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			if tc.runBefore != nil {
				tc.runBefore()
			}

			_, err := ms.RegisterName(ctx, tc.msg)

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
					tc.check(t)
				}
			}
		})
	}
}
