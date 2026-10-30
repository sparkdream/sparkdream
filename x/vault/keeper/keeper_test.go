package keeper_test

import (
	"context"
	"testing"
	"time"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"sparkdream/x/vault/keeper"
	module "sparkdream/x/vault/module"
	"sparkdream/x/vault/types"
)

const testDenom = "uspark.test"

// mockBank records fee movements and bank sends routed through the fake router.
type mockBank struct {
	balances map[string]math.Int
	burned   math.Int
}

func newMockBank() *mockBank {
	return &mockBank{balances: map[string]math.Int{}, burned: math.ZeroInt()}
}

func (b *mockBank) balance(addr string) math.Int {
	if v, ok := b.balances[addr]; ok {
		return v
	}
	return math.ZeroInt()
}

func (b *mockBank) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(testDenom, b.balance(addr.String())))
}

func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, _ string, amt sdk.Coins) error {
	have := b.balance(from.String())
	need := amt.AmountOf(testDenom)
	if have.LT(need) {
		return sdkerrors.ErrInsufficientFunds
	}
	b.balances[from.String()] = have.Sub(need)
	b.balances["module"] = b.balance("module").Add(need)
	return nil
}

func (b *mockBank) BurnCoins(_ context.Context, _ string, amt sdk.Coins) error {
	b.balances["module"] = b.balance("module").Sub(amt.AmountOf(testDenom))
	b.burned = b.burned.Add(amt.AmountOf(testDenom))
	return nil
}

// send is what the fake router runs for a bank MsgSend.
func (b *mockBank) send(from, to string, amt math.Int) error {
	have := b.balance(from)
	if have.LT(amt) {
		return sdkerrors.ErrInsufficientFunds
	}
	b.balances[from] = have.Sub(amt)
	b.balances[to] = b.balance(to).Add(amt)
	return nil
}

type mockAuth struct {
	accounts map[string]sdk.AccountI
	next     uint64
}

func (a *mockAuth) AddressCodec() address.Codec { return nil }
func (a *mockAuth) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	return a.accounts[addr.String()]
}
func (a *mockAuth) NewAccountWithAddress(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	a.next++
	return authtypes.NewBaseAccount(addr, nil, a.next, 0)
}
func (a *mockAuth) SetAccount(_ context.Context, acc sdk.AccountI) {
	a.accounts[acc.GetAddress().String()] = acc
}

type mockIdentity struct{}

func (mockIdentity) BondDenom(context.Context) string { return testDenom }

// mockRep implements the alias contract with an explicit alias table.
type mockRep struct {
	canonicalOf map[string]string // old -> current
	tombstoned  map[string]bool
	nonPersonal map[string]bool
}

func (m *mockRep) Canonical(_ context.Context, addr string) string {
	if c, ok := m.canonicalOf[addr]; ok {
		return c
	}
	return addr
}
func (m *mockRep) CanActAs(ctx context.Context, signer, stored string) bool {
	if signer == "" || stored == "" || m.tombstoned[signer] {
		return false
	}
	return m.Canonical(ctx, stored) == signer
}
func (m *mockRep) AllAddressesOf(ctx context.Context, addr string) []string {
	canon := m.Canonical(ctx, addr)
	out := []string{canon}
	for old, cur := range m.canonicalOf {
		if cur == canon {
			out = append(out, old)
		}
	}
	return out
}
func (m *mockRep) IsTombstoned(_ context.Context, addr string) bool { return m.tombstoned[addr] }
func (m *mockRep) IsPersonalAddress(_ context.Context, addr string) error {
	if m.nonPersonal[addr] {
		return types.ErrNotPersonalAddress
	}
	return nil
}

// fakeRouter routes bank sends to mockBank and vault msgs to the msg server.
type fakeRouter struct {
	handlers map[string]baseapp.MsgServiceHandler
}

func (r *fakeRouter) Handler(msg sdk.Msg) baseapp.MsgServiceHandler {
	return r.handlers[sdk.MsgTypeURL(msg)]
}
func (r *fakeRouter) HandlerByTypeURL(url string) baseapp.MsgServiceHandler { return r.handlers[url] }

type fixture struct {
	ctx          sdk.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	bank         *mockBank
	auth         *mockAuth
	rep          *mockRep
	router       *fakeRouter
	srv          types.MsgServer
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))

	authority := authtypes.NewModuleAddress(types.GovModuleName)
	bank := newMockBank()
	auth := &mockAuth{accounts: map[string]sdk.AccountI{}}

	k := keeper.NewKeeper(storeService, encCfg.Codec, addressCodec, authority, bank, auth)
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}
	k.SetIdentityKeeper(mockIdentity{})
	srv := keeper.NewMsgServerImpl(k)
	router := &fakeRouter{handlers: map[string]baseapp.MsgServiceHandler{}}
	router.handlers[sdk.MsgTypeURL(&banktypes.MsgSend{})] = func(c sdk.Context, m sdk.Msg) (*sdk.Result, error) {
		s := m.(*banktypes.MsgSend)
		if err := bank.send(s.FromAddress, s.ToAddress, s.Amount.AmountOf(testDenom)); err != nil {
			return nil, err
		}
		return &sdk.Result{}, nil
	}
	router.handlers[sdk.MsgTypeURL(&types.MsgUpdateVault{})] = func(c sdk.Context, m sdk.Msg) (*sdk.Result, error) {
		res, err := srv.UpdateVault(c, m.(*types.MsgUpdateVault))
		return sdk.WrapServiceResult(c, res, err)
	}
	router.handlers[sdk.MsgTypeURL(&types.MsgCancelProposal{})] = func(c sdk.Context, m sdk.Msg) (*sdk.Result, error) {
		res, err := srv.CancelProposal(c, m.(*types.MsgCancelProposal))
		return sdk.WrapServiceResult(c, res, err)
	}
	k.SetRouter(router)

	return &fixture{ctx: ctx, keeper: k, addressCodec: addressCodec, bank: bank, auth: auth, router: router, srv: srv}
}

// withRep wires a mockRep into the fixture.
func (f *fixture) withRep() *mockRep {
	f.rep = &mockRep{canonicalOf: map[string]string{}, tombstoned: map[string]bool{}, nonPersonal: map[string]bool{}}
	f.keeper.SetRepKeeper(f.rep)
	return f.rep
}

func (f *fixture) advance(d time.Duration) {
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(d)).WithBlockHeight(f.ctx.BlockHeight() + 1)
}

func addr(name string) string {
	b := make([]byte, 20)
	copy(b, name)
	return sdk.AccAddress(b).String()
}

func signers(weights map[string]uint64, order ...string) []types.VaultSigner {
	out := make([]types.VaultSigner, 0, len(order))
	for _, a := range order {
		out = append(out, types.VaultSigner{Address: a, Weight: weights[a]})
	}
	return out
}
