package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	groupkeeper "github.com/cosmos/cosmos-sdk/x/group/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"sparkdream/x/commons/keeper"
	module "sparkdream/x/commons/module"
	"sparkdream/x/commons/types"
)

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		mockAuthKeeper{},
		nil,
		nil,
		groupkeeper.Keeper{},
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
	}
}

// --- Mock Auth Keeper ---
type mockAuthKeeper struct{}

func (m mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

func (m mockAuthKeeper) AddressCodec() address.Codec {
	return addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
}

func (m mockAuthKeeper) IterateAccounts(ctx context.Context, cb func(account sdk.AccountI) bool) {
	// No-op for msg server tests
}

func (m mockAuthKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	// Return a dummy base account so we don't crash if something checks for existence
	return authtypes.NewBaseAccountWithAddress(addr)
}

// --- Mock Bank Keeper ---
type mockBankKeeperCommons struct {
	balance map[string]sdk.Coins
}

func (m *mockBankKeeperCommons) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	if m.balance[fromAddr.String()].IsAllGTE(amt) {
		m.balance[fromAddr.String()] = m.balance[fromAddr.String()].Sub(amt...)
		m.balance[toAddr.String()] = m.balance[toAddr.String()].Add(amt...)
		return nil
	}
	return sdkerrors.ErrInsufficientFunds
}

func (m *mockBankKeeperCommons) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}

func (m *mockBankKeeperCommons) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	return nil
}

func (m *mockBankKeeperCommons) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balance[addr.String()]
}

func (m *mockBankKeeperCommons) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balance[addr.String()]
}

// --- Mock Staking Keeper ---
type mockStakingKeeper struct{}

func (m mockStakingKeeper) TotalBondedTokens(ctx context.Context) (math.Int, error) {
	return math.NewInt(1000000), nil
}
func (m mockStakingKeeper) IterateBondedValidatorsByPower(ctx context.Context, fn func(index int64, validator stakingtypes.ValidatorI) (stop bool)) error {
	return nil
}
func (m mockStakingKeeper) ValidatorAddressCodec() address.Codec {
	return addresscodec.NewBech32Codec("cosmosvaloper")
}
func (m mockStakingKeeper) IterateDelegations(ctx context.Context, delegator sdk.AccAddress, fn func(index int64, delegation stakingtypes.DelegationI) (stop bool)) error {
	return nil
}

// --- Mock Distribution Keeper ---
type mockDistrKeeper struct{}

func (m mockDistrKeeper) FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error {
	return nil
}
