package keeper

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
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

// spamTaxStubRep records AddToSentinelRewardPool calls. distributeSpamTax used
// to hand the pool share to bank as a module-to-module send, and the assertions
// below checked only that the destination MODULE was x/rep -- which stayed true
// after x/rep moved the pool to a sub-address, so the transfers stopped landing
// in the pool without failing a single test. Routing through x/rep's own API
// means the destination is x/rep's to define and cannot drift out from under
// this test again.
type spamTaxStubRep struct {
	types.RepKeeper // embedded: only the pool method is exercised here
	poolCalls       []struct {
		sender sdk.AccAddress
		amount math.Int
	}
	poolErr error
}

func (s *spamTaxStubRep) AddToSentinelRewardPool(_ context.Context, sender sdk.AccAddress, amount math.Int) error {
	s.poolCalls = append(s.poolCalls, struct {
		sender sdk.AccAddress
		amount math.Int
	}{sender, amount})
	return s.poolErr
}

// spamTaxStubIdentity supplies the bond denom the split keys off.
type spamTaxStubIdentity struct{}

func (spamTaxStubIdentity) IsIdentityKeeper()                   {}
func (spamTaxStubIdentity) BondDenom(_ context.Context) string  { return "uspark" }
func (spamTaxStubIdentity) DreamDenom(_ context.Context) string { return "udream" }

// newSpamTaxTestKeeper constructs a Keeper with the bank, rep and identity
// keepers wired — enough to exercise distributeSpamTax.
func newSpamTaxTestKeeper(bank types.BankKeeper) Keeper {
	return newSpamTaxTestKeeperWithRep(bank, &spamTaxStubRep{})
}

func newSpamTaxTestKeeperWithRep(bank types.BankKeeper, rep types.RepKeeper) Keeper {
	return Keeper{bankKeeper: bank, repKeeper: rep, identityKeeper: spamTaxStubIdentity{}}
}

// emptySDKCtx returns an sdk.Context usable for event emission without state.
func emptySDKCtx() sdk.Context {
	// A default sdk.Context has a nil EventManager until SetEventManager is
	// invoked; we rely on UnwrapSDKContext returning whatever is set here.
	return sdk.Context{}.WithEventManager(sdk.NewEventManager())
}

func TestDistributeSpamTax_EvenAmount(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	rep := &spamTaxStubRep{}
	k := newSpamTaxTestKeeperWithRep(bank, rep)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000)))

	err := k.distributeSpamTax(ctx, coins, "post")
	require.NoError(t, err)

	// Even amount 1000 → 500 burn / 500 pool exactly.
	require.Len(t, rep.poolCalls, 1)
	require.Equal(t, authtypes.NewModuleAddress(types.ModuleName), rep.poolCalls[0].sender)
	require.Equal(t, math.NewInt(500), rep.poolCalls[0].amount)
	require.Len(t, bank.modToModCalls, 0, "the pool share must not go out as a module-to-module send")

	require.Len(t, bank.burnCalls, 1)
	require.Equal(t, types.ModuleName, bank.burnCalls[0].module)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(500))), bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_OddAmount(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	rep := &spamTaxStubRep{}
	k := newSpamTaxTestKeeperWithRep(bank, rep)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1001)))

	err := k.distributeSpamTax(ctx, coins, "post")
	require.NoError(t, err)

	// Odd amount 1001 → pool gets smaller half (500), burn gets larger
	// half (501). Conservative: any rounding remainder burned.
	require.Len(t, rep.poolCalls, 1)
	require.Equal(t, math.NewInt(500), rep.poolCalls[0].amount)

	require.Len(t, bank.burnCalls, 1)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(501))), bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_AmountOne_FullyBurned(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	rep := &spamTaxStubRep{}
	k := newSpamTaxTestKeeperWithRep(bank, rep)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1)))

	err := k.distributeSpamTax(ctx, coins, "reaction")
	require.NoError(t, err)

	// Amount 1 → pool half (1/2 = 0), burn half (1 - 0 = 1).
	// No pool contribution should occur because the pool share is zero.
	require.Len(t, rep.poolCalls, 0)

	require.Len(t, bank.burnCalls, 1)
	require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1))), bank.burnCalls[0].amt)
}

func TestDistributeSpamTax_EmptyCoins(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	rep := &spamTaxStubRep{}
	k := newSpamTaxTestKeeperWithRep(bank, rep)

	ctx := emptySDKCtx()

	// sdk.NewCoins() with no args returns empty sdk.Coins.
	err := k.distributeSpamTax(ctx, sdk.NewCoins(), "edit")
	require.NoError(t, err)
	require.Len(t, rep.poolCalls, 0)
	require.Len(t, bank.burnCalls, 0)

	// Nil coins also returns cleanly.
	err = k.distributeSpamTax(ctx, nil, "edit")
	require.NoError(t, err)
	require.Len(t, rep.poolCalls, 0)
	require.Len(t, bank.burnCalls, 0)
}

func TestDistributeSpamTax_MultipleDenoms(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	rep := &spamTaxStubRep{}
	k := newSpamTaxTestKeeperWithRep(bank, rep)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(
		sdk.NewCoin("uspark", math.NewInt(2000)),
		sdk.NewCoin("udream", math.NewInt(401)),
	)

	err := k.distributeSpamTax(ctx, coins, "flag")
	require.NoError(t, err)

	// Only the bond denom is splittable: the sentinel reward pool holds SPARK,
	// so a non-bond denom is burned in full rather than half-routed to a pool
	// that cannot hold it (and where it would be unreachable).
	//   uspark 2000 → 1000 pool / 1000 burn
	//   udream 401  → 0 pool / 401 burn
	require.Len(t, rep.poolCalls, 1)
	require.Equal(t, math.NewInt(1000), rep.poolCalls[0].amount)

	require.Len(t, bank.burnCalls, 1)
	expectedBurn := sdk.NewCoins(
		sdk.NewCoin("uspark", math.NewInt(1000)),
		sdk.NewCoin("udream", math.NewInt(401)),
	)
	require.Equal(t, expectedBurn, bank.burnCalls[0].amt)
}

// Regression guard. This test used to assert only that the pool share was sent
// to the x/rep MODULE account, which stayed true after x/rep moved the pool to
// a derived sub-address -- so the money stopped reaching the pool and no test
// noticed. Asserting the x/rep pool API instead makes the destination x/rep's
// to define, and any future move fails inside x/rep rather than silently here.
func TestDistributeSpamTax_ReachesSentinelPoolAPI(t *testing.T) {
	bank := &distributeSpamTaxStubBank{}
	rep := &spamTaxStubRep{}
	k := newSpamTaxTestKeeperWithRep(bank, rep)

	ctx := emptySDKCtx()
	coins := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(10)))

	err := k.distributeSpamTax(ctx, coins, "post")
	require.NoError(t, err)

	// Must always route the pool share through x/rep's sentinel pool API,
	// funded from the forum module account.
	require.Len(t, rep.poolCalls, 1)
	require.Equal(t, authtypes.NewModuleAddress(types.ModuleName), rep.poolCalls[0].sender)
	require.Equal(t, math.NewInt(5), rep.poolCalls[0].amount)
	require.Len(t, bank.modToModCalls, 0)
}
