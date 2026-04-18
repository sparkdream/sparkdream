package keeper

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// distributeSpamTaxStubBank is a minimal BankKeeper stub that records
// SendCoinsFromModuleToModule and BurnCoins invocations so the split
// math in distributeSpamTax can be asserted directly.
type distributeSpamTaxStubBank struct {
	modToModCalls []struct {
		from string
		to   string
		amt  sdk.Coins
	}
	burnCalls []struct {
		module string
		amt    sdk.Coins
	}
	modToModErr error
	burnErr     error
}

func (s *distributeSpamTaxStubBank) SpendableCoins(_ context.Context, _ sdk.AccAddress) sdk.Coins {
	return nil
}

func (s *distributeSpamTaxStubBank) SendCoins(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
	return nil
}

func (s *distributeSpamTaxStubBank) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
	return nil
}

func (s *distributeSpamTaxStubBank) SendCoinsFromModuleToAccount(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
	return nil
}

func (s *distributeSpamTaxStubBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	s.modToModCalls = append(s.modToModCalls, struct {
		from string
		to   string
		amt  sdk.Coins
	}{from, to, amt})
	return s.modToModErr
}

func (s *distributeSpamTaxStubBank) BurnCoins(_ context.Context, moduleName string, amt sdk.Coins) error {
	s.burnCalls = append(s.burnCalls, struct {
		module string
		amt    sdk.Coins
	}{moduleName, amt})
	return s.burnErr
}

func (s *distributeSpamTaxStubBank) MintCoins(_ context.Context, _ string, _ sdk.Coins) error {
	return nil
}

// newSpamTaxTestKeeper constructs a Keeper with only the bankKeeper wired
// — enough to exercise distributeSpamTax which only touches bank.
func newSpamTaxTestKeeper(bank types.BankKeeper) Keeper {
	return Keeper{bankKeeper: bank}
}

// emptySDKCtx returns an sdk.Context usable for event emission without state.
func emptySDKCtx() sdk.Context {
	// A default sdk.Context has a nil EventManager until SetEventManager is
	// invoked; we rely on UnwrapSDKContext returning whatever is set here.
	return sdk.Context{}.WithEventManager(sdk.NewEventManager())
}

func TestDistributeSpamTax_EvenAmount(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	k := newSpamTaxTestKeeper(bank)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000)))

	err := k.distributeSpamTax(ctx, coins, "post")
	require.NoError(t, err)

	// Even amount 1000 → 500 burn / 500 pool exactly.
	require.Len(t, bank.modToModCalls, 1)
	require.Equal(t, types.ModuleName, bank.modToModCalls[0].from)
	require.Equal(t, reptypes.ModuleName, bank.modToModCalls[0].to)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(500))), bank.modToModCalls[0].amt)

	require.Len(t, bank.burnCalls, 1)
	require.Equal(t, types.ModuleName, bank.burnCalls[0].module)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(500))), bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_OddAmount(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	k := newSpamTaxTestKeeper(bank)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1001)))

	err := k.distributeSpamTax(ctx, coins, "post")
	require.NoError(t, err)

	// Odd amount 1001 → pool gets smaller half (500), burn gets larger
	// half (501). Conservative: any rounding remainder burned.
	require.Len(t, bank.modToModCalls, 1)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(500))), bank.modToModCalls[0].amt)

	require.Len(t, bank.burnCalls, 1)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(501))), bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_AmountOne_FullyBurned(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	k := newSpamTaxTestKeeper(bank)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1)))

	err := k.distributeSpamTax(ctx, coins, "reaction")
	require.NoError(t, err)

	// Amount 1 → pool half (1/2 = 0), burn half (1 - 0 = 1).
	// No mod-to-mod transfer should occur because pool share is zero.
	require.Len(t, bank.modToModCalls, 0)

	require.Len(t, bank.burnCalls, 1)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1))), bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_EmptyCoins(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	k := newSpamTaxTestKeeper(bank)

	ctx := emptySDKCtx()

	// sdk.NewCoins() with no args returns empty sdk.Coins.
	err := k.distributeSpamTax(ctx, sdk.NewCoins(), "edit")
	require.NoError(t, err)
	require.Len(t, bank.modToModCalls, 0)
	require.Len(t, bank.burnCalls, 0)

	// Nil coins also returns cleanly.
	err = k.distributeSpamTax(ctx, nil, "edit")
	require.NoError(t, err)
	require.Len(t, bank.modToModCalls, 0)
	require.Len(t, bank.burnCalls, 0)
}

func TestDistributeSpamTax_MultipleDenoms(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	k := newSpamTaxTestKeeper(bank)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(
		sdk.NewCoin("uspark", math.NewInt(2000)),
		sdk.NewCoin("udream", math.NewInt(401)),
	)

	err := k.distributeSpamTax(ctx, coins, "flag")
	require.NoError(t, err)

	// Each denom split independently:
	//   uspark 2000 → 1000 pool / 1000 burn
	//   udream 401  → 200 pool / 201 burn
	require.Len(t, bank.modToModCalls, 1)
	expectedPool := sdk.NewCoins(
		sdk.NewCoin("uspark", math.NewInt(1000)),
		sdk.NewCoin("udream", math.NewInt(200)),
	)
	require.Equal(t, expectedPool, bank.modToModCalls[0].amt)

	require.Len(t, bank.burnCalls, 1)
	expectedBurn := sdk.NewCoins(
		sdk.NewCoin("uspark", math.NewInt(1000)),
		sdk.NewCoin("udream", math.NewInt(201)),
	)
	require.Equal(t, expectedBurn, bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_TargetsRepModule(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	k := newSpamTaxTestKeeper(bank)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10)))

	err := k.distributeSpamTax(ctx, coins, "post")
	require.NoError(t, err)

	// Must always route pool share from forum → rep.
	require.Len(t, bank.modToModCalls, 1)
	require.Equal(t, types.ModuleName, bank.modToModCalls[0].from)
	require.Equal(t, reptypes.ModuleName, bank.modToModCalls[0].to)
}
