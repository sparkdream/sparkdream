package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

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
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// --- Setup Helper ---
func setupCommonsKeeper(t *testing.T) (keeper.Keeper, sdk.Context, *mockBankKeeperCommons) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey("mem_commons")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, storetypes.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())

	cdc := codectestutil.CodecOptions{}.NewCodec()
	addressCodec := addresscodec.NewBech32Codec("cosmos")
	authority := sdk.AccAddress([]byte("authority"))

	mockBK := &mockBankKeeperCommons{
		balance: make(map[string]sdk.Coins),
	}

	storeService := runtime.NewKVStoreService(storeKey)

	k := keeper.NewKeeper(
		storeService,
		cdc,
		addressCodec,
		authority,
		nil,
		mockBK,
		nil,
		groupkeeper.Keeper{},
	)

	return k, ctx, mockBK
}

// --- Test Suite ---

func TestSpendFromCommons(t *testing.T) {
	k, ctx, mockBK := setupCommonsKeeper(t)
	ms := keeper.NewMsgServerImpl(k)

	// Setup Addresses
	councilAddr := sdk.AccAddress([]byte("council_address_____"))
	recipientAddr := sdk.AccAddress([]byte("recipient_address___"))
	attackerAddr := sdk.AccAddress([]byte("attacker_address____"))

	// Setup Params (Define the Authorized Council)
	params := types.DefaultParams()
	params.CommonsCouncilAddress = councilAddr.String()
	err := k.Params.Set(ctx, params)
	require.NoError(t, err)

	// Fund the Council (Mock Bank)
	fundAmount := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000)))
	mockBK.balance[councilAddr.String()] = fundAmount

	tests := []struct {
		desc    string
		msg     *types.MsgSpendFromCommons
		check   func(t *testing.T)
		err     error
		errCode codes.Code
	}{
		{
			desc: "Success - Valid Spend by Council",
			msg: &types.MsgSpendFromCommons{
				Authority: councilAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			},
			check: func(t *testing.T) {
				bal := mockBK.balance[recipientAddr.String()]
				require.Equal(t, math.NewInt(100), bal.AmountOf("uspark"))

				balCouncil := mockBK.balance[councilAddr.String()]
				require.Equal(t, math.NewInt(900), balCouncil.AmountOf("uspark"))
			},
		},
		{
			desc: "Failure - Unauthorized (Signer is not Council)",
			msg: &types.MsgSpendFromCommons{
				Authority: attackerAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			},
			err: sdkerrors.ErrUnauthorized,
		},
		{
			desc: "Failure - Insufficient Funds",
			msg: &types.MsgSpendFromCommons{
				Authority: councilAddr.String(),
				Recipient: recipientAddr.String(),
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(999999))),
			},
			err: sdkerrors.ErrInsufficientFunds,
		},
		{
			desc: "Failure - Invalid Recipient Address",
			msg: &types.MsgSpendFromCommons{
				Authority: councilAddr.String(),
				Recipient: "invalid_address",
				Amount:    sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10))),
			},
			errCode: codes.InvalidArgument,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			cacheCtx, _ := ctx.CacheContext()

			_, err := ms.SpendFromCommons(cacheCtx, tc.msg)

			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else if tc.errCode != codes.OK {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if tc.check != nil {
					tc.check(t)
				}
			}
		})
	}
}
