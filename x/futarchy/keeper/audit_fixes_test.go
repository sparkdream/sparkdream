package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/futarchy/keeper"
	module "sparkdream/x/futarchy/module"
	"sparkdream/x/futarchy/types"
)

// ledgerBank is a double-entry bank mock: module accounts hold balances and
// mint/burn moves supply, so the solvency invariant can be checked.
type ledgerBank struct {
	bal    map[string]map[string]math.Int // addr -> denom -> amount
	supply map[string]math.Int
}

func newLedgerBank() *ledgerBank {
	return &ledgerBank{bal: map[string]map[string]math.Int{}, supply: map[string]math.Int{}}
}

func modAddr(name string) sdk.AccAddress { return authtypes.NewModuleAddress(name) }

func (b *ledgerBank) get(addr sdk.AccAddress, denom string) math.Int {
	if m, ok := b.bal[addr.String()]; ok {
		if v, ok := m[denom]; ok {
			return v
		}
	}
	return math.ZeroInt()
}

func (b *ledgerBank) add(addr sdk.AccAddress, c sdk.Coin) {
	m, ok := b.bal[addr.String()]
	if !ok {
		m = map[string]math.Int{}
		b.bal[addr.String()] = m
	}
	m[c.Denom] = b.get(addr, c.Denom).Add(c.Amount)
}

func (b *ledgerBank) sub(addr sdk.AccAddress, c sdk.Coin) error {
	have := b.get(addr, c.Denom)
	if have.LT(c.Amount) {
		return sdkerrors.ErrInsufficientFunds.Wrapf("%s < %s", have, c)
	}
	b.bal[addr.String()][c.Denom] = have.Sub(c.Amount)
	return nil
}

func (b *ledgerBank) move(from, to sdk.AccAddress, amt sdk.Coins) error {
	for _, c := range amt {
		if err := b.sub(from, c); err != nil {
			return err
		}
		b.add(to, c)
	}
	return nil
}

func (b *ledgerBank) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (b *ledgerBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.get(addr, denom))
}
func (b *ledgerBank) GetSupply(_ context.Context, denom string) sdk.Coin {
	s, ok := b.supply[denom]
	if !ok {
		s = math.ZeroInt()
	}
	return sdk.NewCoin(denom, s)
}
func (b *ledgerBank) MintCoins(_ context.Context, m string, amt sdk.Coins) error {
	for _, c := range amt {
		b.add(modAddr(m), c)
		b.supply[c.Denom] = b.GetSupply(context.TODO(), c.Denom).Amount.Add(c.Amount)
	}
	return nil
}
func (b *ledgerBank) BurnCoins(_ context.Context, m string, amt sdk.Coins) error {
	for _, c := range amt {
		if err := b.sub(modAddr(m), c); err != nil {
			return err
		}
		b.supply[c.Denom] = b.GetSupply(context.TODO(), c.Denom).Amount.Sub(c.Amount)
	}
	return nil
}
func (b *ledgerBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, m string, amt sdk.Coins) error {
	return b.move(from, modAddr(m), amt)
}
func (b *ledgerBank) SendCoinsFromModuleToAccount(_ context.Context, m string, to sdk.AccAddress, amt sdk.Coins) error {
	return b.move(modAddr(m), to, amt)
}
func (b *ledgerBank) SendCoinsFromModuleToModule(_ context.Context, from, to string, amt sdk.Coins) error {
	return b.move(modAddr(from), modAddr(to), amt)
}

type ledgerAuth struct{ MockAuthKeeper }

func (*ledgerAuth) GetModuleAddress(name string) sdk.AccAddress { return modAddr(name) }

type recordingHooks struct{ calls []string }

func (h *recordingHooks) AfterMarketResolved(_ context.Context, id uint64, winner string) error {
	h.calls = append(h.calls, fmt.Sprintf("%d:%s", id, winner))
	return nil
}

type ledgerFixture struct {
	ctx   sdk.Context
	k     keeper.Keeper
	bank  *ledgerBank
	hooks *recordingHooks
	ms    types.MsgServer
	denom string
}

func initLedgerFixture(t *testing.T) *ledgerFixture {
	t.Helper()
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_audit")).Ctx
	bank := newLedgerBank()
	k := keeper.NewKeeper(runtime.NewKVStoreService(storeKey), encCfg.Codec, addressCodec,
		authtypes.NewModuleAddress(types.GovModuleName), &ledgerAuth{}, bank)
	k.SetIdentityKeeper(&mockIdentityKeeperFutarchy{})
	hooks := &recordingHooks{}
	k.SetHooks(hooks)
	params := types.DefaultParams()
	params.TradingFeeBps = 0
	require.NoError(t, k.Params.Set(ctx, params))
	return &ledgerFixture{ctx: ctx.WithBlockHeight(100), k: k, bank: bank, hooks: hooks,
		ms: keeper.NewMsgServerImpl(k), denom: "uspark"}
}

func (f *ledgerFixture) fund(addr sdk.AccAddress, amt int64) {
	f.bank.add(addr, sdk.NewInt64Coin(f.denom, amt))
}

func (f *ledgerFixture) createMarket(t *testing.T, creator sdk.AccAddress, liquidity, duration int64) uint64 {
	t.Helper()
	f.fund(creator, liquidity)
	id, err := f.k.CreateMarketInternal(f.ctx, creator, "SYM", "q?", duration, 0, sdk.NewInt64Coin(f.denom, liquidity))
	require.NoError(t, err)
	return id
}

func (f *ledgerFixture) trade(t *testing.T, who sdk.AccAddress, id uint64, yes bool, amt int64) (math.Int, error) {
	f.fund(who, amt)
	in := math.NewInt(amt)
	res, err := f.ms.Trade(f.ctx, &types.MsgTrade{Creator: who.String(), MarketId: id, IsYes: yes, AmountIn: &in})
	if err != nil {
		return math.ZeroInt(), err
	}
	return *res.SharesOut, nil
}

func (f *ledgerFixture) endBlock(t *testing.T, height int64) {
	t.Helper()
	f.ctx = f.ctx.WithBlockHeight(height)
	require.NoError(t, module.EndBlocker(f.ctx, f.k))
}

func (f *ledgerFixture) requireSolvent(t *testing.T) {
	t.Helper()
	msg, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.False(t, broken, msg)
}

func requireDecNear(t *testing.T, want string, got math.LegacyDec, tol string) {
	t.Helper()
	w := math.LegacyMustNewDecFromStr(want)
	require.True(t, got.Sub(w).Abs().LTE(math.LegacyMustNewDecFromStr(tol)), "want %s got %s", want, got)
}

// FUT-03/FUT-04: Exp and Ln are accurate far outside |x| <= 1 (the plain
// Taylor series evaluated Exp(-60) to -4.4e19), and the settlement price is
// no longer clamped (it returned 0.5 for qY=30b, qN=25b).
func TestLMSRMathRangeReduction(t *testing.T) {
	ctx := initLedgerFixture(t).ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())

	e60 := types.Exp(ctx, math.LegacyNewDec(-60))
	require.False(t, e60.IsNegative())
	require.True(t, e60.LT(math.LegacyMustNewDecFromStr("0.000000000001")))
	requireDecNear(t, "0.000045399929762485", types.Exp(ctx, math.LegacyNewDec(-10)), "0.000000000000001")
	requireDecNear(t, "22026.465794806716516958", types.Exp(ctx, math.LegacyNewDec(10)), "0.0000001")

	ln, err := types.Ln(ctx, math.LegacyMustNewDecFromStr("0.000000001"))
	require.NoError(t, err)
	requireDecNear(t, "-20.723265836946411156", ln, "0.000000000001")
	ln, err = types.Ln(ctx, math.LegacyNewDec(1_000_000_000_000))
	require.NoError(t, err)
	requireDecNear(t, "27.631021115928547208", ln, "0.000000000001")

	b := math.LegacyNewDec(1000)
	p, err := types.SettlementPriceYes(ctx, b, math.LegacyNewDec(30000), math.LegacyNewDec(25000))
	require.NoError(t, err)
	requireDecNear(t, "0.993307149075715152", p, "0.000000000001")

	// C(0,0) = b ln 2 exactly to rounding.
	c, err := types.CalculateLMSRCost(ctx, b, math.LegacyZeroDec(), math.LegacyZeroDec())
	require.NoError(t, err)
	requireDecNear(t, "693.147180559945309", c, "0.000000000001")
}

// FUT-03: a trade that would push |qY-qN|/b past max_lmsr_exponent is
// rejected instead of returning negative or mispriced shares.
func TestTradeRejectedPastPriceBound(t *testing.T) {
	f := initLedgerFixture(t)
	creator := sdk.AccAddress([]byte("creator_phoenix_____"))
	whale := sdk.AccAddress([]byte("whale_aurora________"))
	id := f.createMarket(t, creator, 1_000_000, 100)

	// b ~ 1.44e6; pushing YES ~ 25b ahead costs ~ 25b SPARK: rejected at 20b.
	_, err := f.trade(t, whale, id, true, 40_000_000)
	require.ErrorContains(t, err, "max_lmsr_exponent")

	// A trade inside the bound succeeds and mints positive shares.
	shares, err := f.trade(t, whale, id, true, 10_000_000)
	require.NoError(t, err)
	require.True(t, shares.IsPositive())
	f.requireSolvent(t)
}

// FUT-01: trading closes at end_block, and the outcome is the
// time-weighted price, so a last-block whale cannot flip the market.
func TestTWAPResolutionResistsLastTrader(t *testing.T) {
	f := initLedgerFixture(t)
	creator := sdk.AccAddress([]byte("creator_phoenix_____"))
	alice := sdk.AccAddress([]byte("alice_zenith________"))
	whale := sdk.AccAddress([]byte("whale_aurora________"))
	id := f.createMarket(t, creator, 1_000_000, 100) // start 100, end 200

	f.ctx = f.ctx.WithBlockHeight(110)
	_, err := f.trade(t, alice, id, true, 300_000)
	require.NoError(t, err)

	// One block before the end the whale buys NO until NO leads the pools.
	f.ctx = f.ctx.WithBlockHeight(199)
	_, err = f.trade(t, whale, id, false, 1_000_000)
	require.NoError(t, err)
	m, err := f.k.Market.Get(f.ctx, id)
	require.NoError(t, err)
	require.True(t, m.PoolNo.GT(*m.PoolYes), "final pools favour NO (old rule would resolve NO)")

	// Trading in the end block itself is refused.
	f.ctx = f.ctx.WithBlockHeight(200)
	_, err = f.trade(t, whale, id, false, 10_000)
	require.ErrorContains(t, err, "trading closed")

	f.endBlock(t, 200)
	m, err = f.k.Market.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "RESOLVED_YES", m.Status)
	require.Equal(t, []string{fmt.Sprintf("%d:yes", id)}, f.hooks.calls)
	f.requireSolvent(t)

	// Winners redeem 1:1 even though the YES side holds fewer shares, and
	// the creator's residual (C - q_yes > subsidy) was paid automatically.
	_, err = f.ms.Redeem(f.ctx, &types.MsgRedeem{Creator: alice.String(), MarketId: id})
	require.NoError(t, err)
	require.True(t, m.LiquidityWithdrawn.GT(*m.InitialLiquidity))
	f.requireSolvent(t)
}

// FUT-02 + FUT-07: an untraded market resolves INVALID, the creator gets the
// whole subsidy back without signing anything, and the hook fires with
// "invalid".
func TestResolutionRefundsCreatorAndFiresInvalidHook(t *testing.T) {
	f := initLedgerFixture(t)
	commons := modAddr("commons")
	id := f.createMarket(t, commons, 1_000_000_000, 50)
	require.True(t, f.bank.get(commons, f.denom).IsZero())

	f.endBlock(t, 150)
	m, err := f.k.Market.Get(f.ctx, id)
	require.NoError(t, err)
	require.Equal(t, "RESOLVED_INVALID", m.Status)
	require.Equal(t, int64(1_000_000_000), f.bank.get(commons, f.denom).Int64())
	require.Equal(t, []string{fmt.Sprintf("%d:invalid", id)}, f.hooks.calls)

	// Nothing left for a manual withdrawal.
	_, err = f.ms.WithdrawLiquidity(f.ctx, &types.MsgWithdrawLiquidity{Creator: commons.String(), MarketId: id})
	require.ErrorContains(t, err, "No liquidity available")
	f.requireSolvent(t)
}

// FUT-09: at most MaxResolutionsPerBlock markets resolve per block; the rest
// carry over.
func TestEndBlockerResolutionCap(t *testing.T) {
	f := initLedgerFixture(t)
	creator := sdk.AccAddress([]byte("creator_phoenix_____"))
	n := keeper.MaxResolutionsPerBlock + 5
	for i := 0; i < n; i++ {
		f.createMarket(t, creator, 100_000, 10)
	}
	f.endBlock(t, 110)
	require.Len(t, f.hooks.calls, keeper.MaxResolutionsPerBlock)
	f.endBlock(t, 111)
	require.Len(t, f.hooks.calls, n)
	f.requireSolvent(t)
}

// FUT-06: min_shares_out bounds slippage.
func TestTradeSlippageBound(t *testing.T) {
	f := initLedgerFixture(t)
	creator := sdk.AccAddress([]byte("creator_phoenix_____"))
	alice := sdk.AccAddress([]byte("alice_zenith________"))
	id := f.createMarket(t, creator, 1_000_000, 100)

	f.fund(alice, 100_000)
	in := math.NewInt(100_000)
	tooMany := math.NewInt(1_000_000)
	_, err := f.ms.Trade(f.ctx, &types.MsgTrade{Creator: alice.String(), MarketId: id, IsYes: true, AmountIn: &in, MinSharesOut: &tooMany})
	require.ErrorContains(t, err, "slippage")

	// The mock bank is not transactional; refund the failed trade's input.
	f.fund(alice, 100_000)
	enough := math.NewInt(100_000)
	res, err := f.ms.Trade(f.ctx, &types.MsgTrade{Creator: alice.String(), MarketId: id, IsYes: true, AmountIn: &in, MinSharesOut: &enough})
	require.NoError(t, err)
	require.True(t, res.SharesOut.GTE(enough))
}

// FUT-05: the solvency invariant holds through a full lifecycle and trips
// when collateral goes missing.
func TestSolvencyInvariant(t *testing.T) {
	f := initLedgerFixture(t)
	creator := sdk.AccAddress([]byte("creator_phoenix_____"))
	alice := sdk.AccAddress([]byte("alice_zenith________"))
	bob := sdk.AccAddress([]byte("bob_zenith__________"))
	id := f.createMarket(t, creator, 1_000_000, 100)
	f.requireSolvent(t)

	f.ctx = f.ctx.WithBlockHeight(120)
	_, err := f.trade(t, alice, id, true, 700_000)
	require.NoError(t, err)
	f.ctx = f.ctx.WithBlockHeight(150)
	_, err = f.trade(t, bob, id, false, 200_000)
	require.NoError(t, err)
	f.requireSolvent(t)

	f.endBlock(t, 200)
	f.requireSolvent(t)
	_, err = f.ms.Redeem(f.ctx, &types.MsgRedeem{Creator: alice.String(), MarketId: id})
	require.NoError(t, err)
	f.requireSolvent(t)

	// Steal collateral from a still-active market: invariant breaks.
	id2 := f.createMarket(t, creator, 1_000_000, 100)
	_ = id2
	require.NoError(t, f.bank.sub(modAddr(types.ModuleName), sdk.NewInt64Coin(f.denom, 500_000)))
	_, broken := keeper.SolvencyInvariant(f.k)(f.ctx)
	require.True(t, broken)
}

// FUT-04 / FUT-10: compiled ceilings on the dangerous params.
func TestParamHardCeilings(t *testing.T) {
	p := types.DefaultParams()
	p.MaxLmsrExponent = "31"
	require.ErrorContains(t, p.Validate(), "max_lmsr_exponent")
	p = types.DefaultParams()
	p.TradingFeeBps = types.MaxTradingFeeBps + 1
	require.ErrorContains(t, p.Validate(), "trading_fee_bps")
	p.TradingFeeBps = types.MaxTradingFeeBps
	require.NoError(t, p.Validate())
}

// TestHooksReachTheAppModuleCopy: app.go sets the futarchy hooks after
// depinject, on its own keeper value; the AppModule ran its EndBlocker
// market resolution on a copy taken earlier and never saw them, so
// AfterMarketResolved (council confidence outcomes) never fired on chain.
// The hooks now live in the keeper's shared late struct.
func TestHooksReachTheAppModuleCopy(t *testing.T) {
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_hooks")).Ctx
	bank := newLedgerBank()
	k := keeper.NewKeeper(runtime.NewKVStoreService(storeKey), encCfg.Codec, addressCodec,
		authtypes.NewModuleAddress(types.GovModuleName), &ledgerAuth{}, bank)
	k.SetIdentityKeeper(&mockIdentityKeeperFutarchy{})
	moduleCopy := k // what NewAppModule(k) captures inside depinject
	hooks := &recordingHooks{}
	k.SetHooks(hooks) // what app.go does afterwards
	params := types.DefaultParams()
	params.TradingFeeBps = 0
	require.NoError(t, k.Params.Set(ctx, params))

	f := &ledgerFixture{ctx: ctx.WithBlockHeight(100), k: moduleCopy, bank: bank, hooks: hooks,
		ms: keeper.NewMsgServerImpl(moduleCopy), denom: "uspark"}
	creator := sdk.AccAddress("phoenix_____________")
	id := f.createMarket(t, creator, 1_000_000, 10)
	m, err := moduleCopy.Market.Get(f.ctx, id)
	require.NoError(t, err)
	f.endBlock(t, m.EndBlock+1)
	require.NotEmpty(t, hooks.calls, "AfterMarketResolved must fire from the AppModule's keeper copy")
}
