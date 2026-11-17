package keeper_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
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
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/keeper"
	module "sparkdream/x/storage/module"
	"sparkdream/x/storage/types"
)

const testDenom = "uspark.test"

// ---- bank -----------------------------------------------------------------

type mockBank struct {
	balances map[string]math.Int
	burned   math.Int
	// blocked recipients refuse a module send, as a blocked address does.
	blocked map[string]bool
}

func newMockBank() *mockBank {
	return &mockBank{balances: map[string]math.Int{}, burned: math.ZeroInt(), blocked: map[string]bool{}}
}

func moduleAddr() sdk.AccAddress { return authtypes.NewModuleAddress(types.ModuleName) }

func (b *mockBank) balance(addr string) math.Int {
	if v, ok := b.balances[addr]; ok {
		return v
	}
	return math.ZeroInt()
}
func (b *mockBank) move(from, to string, amt math.Int) error {
	if b.balance(from).LT(amt) {
		return sdkerrors.ErrInsufficientFunds
	}
	b.balances[from] = b.balance(from).Sub(amt)
	b.balances[to] = b.balance(to).Add(amt)
	return nil
}
func (b *mockBank) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(testDenom, b.balance(addr.String())))
}
func (b *mockBank) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balance(addr.String()))
}
func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, _ string, amt sdk.Coins) error {
	return b.move(from.String(), moduleAddr().String(), amt.AmountOf(testDenom))
}
func (b *mockBank) SendCoinsFromModuleToAccount(_ context.Context, _ string, to sdk.AccAddress, amt sdk.Coins) error {
	if b.blocked[to.String()] {
		return sdkerrors.ErrUnauthorized
	}
	return b.move(moduleAddr().String(), to.String(), amt.AmountOf(testDenom))
}
func (b *mockBank) BurnCoins(_ context.Context, _ string, amt sdk.Coins) error {
	a := amt.AmountOf(testDenom)
	if b.balance(moduleAddr().String()).LT(a) {
		return sdkerrors.ErrInsufficientFunds
	}
	b.balances[moduleAddr().String()] = b.balance(moduleAddr().String()).Sub(a)
	b.burned = b.burned.Add(a)
	return nil
}

type mockAuth struct{}

func (mockAuth) AddressCodec() address.Codec                             { return nil }
func (mockAuth) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI { return nil }
func (mockAuth) GetModuleAddress(name string) sdk.AccAddress             { return authtypes.NewModuleAddress(name) }

type mockIdentity struct{}

func (mockIdentity) BondDenom(context.Context) string { return testDenom }

// ---- rep ------------------------------------------------------------------

type mockRep struct {
	active      map[string]bool
	trust       map[string]reptypes.TrustLevel
	canonicalOf map[string]string // old -> current
}

func (m *mockRep) IsActiveMember(_ context.Context, a sdk.AccAddress) bool {
	return m.active[a.String()]
}
func (m *mockRep) GetTrustLevel(_ context.Context, a sdk.AccAddress) (reptypes.TrustLevel, error) {
	t, ok := m.trust[a.String()]
	if !ok {
		return 0, reptypes.ErrMemberNotFound
	}
	return t, nil
}
func (m *mockRep) Canonical(_ context.Context, a string) string {
	if c, ok := m.canonicalOf[a]; ok {
		return c
	}
	return a
}
func (m *mockRep) CanActAs(ctx context.Context, signer, stored string) bool {
	if signer == "" || stored == "" {
		return false
	}
	if _, tomb := m.canonicalOf[signer]; tomb {
		return false
	}
	return m.Canonical(ctx, stored) == signer
}
func (m *mockRep) SameMember(ctx context.Context, a, b string) bool {
	return a != "" && m.Canonical(ctx, a) == m.Canonical(ctx, b)
}
func (m *mockRep) AllAddressesOf(ctx context.Context, a string) []string {
	canon := m.Canonical(ctx, a)
	out := []string{canon}
	for old, cur := range m.canonicalOf {
		if cur == canon {
			out = append(out, old)
		}
	}
	return out
}
func (m *mockRep) RecentAddressesOf(ctx context.Context, a string) []string {
	return m.AllAddressesOf(ctx, a)
}

// ---- service --------------------------------------------------------------

type sysReport struct{ operator, serviceType string }

type mockService struct {
	ops       map[string]servicetypes.Operator // addr/type -> live record
	slashed   map[string]bool                  // addr/type
	configs   map[string]servicetypes.ServiceTypeConfig
	unbonding map[string]int64
	reports   []sysReport
	dedupe    map[string]bool
	reportErr error
}

func opKey(addr, serviceType string) string { return addr + "/" + serviceType }

func (m *mockService) GetOperator(_ context.Context, addrBytes []byte, st string) (servicetypes.Operator, bool) {
	op, ok := m.ops[opKey(sdk.AccAddress(addrBytes).String(), st)]
	return op, ok
}
func (m *mockService) IsActiveOperator(ctx context.Context, a sdk.AccAddress, st string) bool {
	op, ok := m.GetOperator(ctx, a.Bytes(), st)
	return ok && op.Status == servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE
}
func (m *mockService) HasSlashedRecord(_ context.Context, addrBytes []byte, st string) (bool, error) {
	return m.slashed[opKey(sdk.AccAddress(addrBytes).String(), st)], nil
}
func (m *mockService) GetServiceTypeConfig(_ context.Context, st string) (servicetypes.ServiceTypeConfig, bool) {
	c, ok := m.configs[st]
	return c, ok
}
func (m *mockService) UnbondingPeriodBlocks(_ context.Context, st string) (int64, bool) {
	v, ok := m.unbonding[st]
	return v, ok
}
func (m *mockService) OpenSystemReport(_ context.Context, _, operator sdk.AccAddress, st string, _ uint32, _ string, dedupe []byte) (uint64, bool, error) {
	if m.reportErr != nil {
		return 0, false, m.reportErr
	}
	if m.dedupe[string(dedupe)] {
		return 1, true, nil
	}
	m.dedupe[string(dedupe)] = true
	m.reports = append(m.reports, sysReport{operator.String(), st})
	return uint64(len(m.reports)), false, nil
}

// ---- commons, distribution -------------------------------------------------

type mockCommons struct {
	groups map[string]bool
	ops    map[string]bool // addresses allowed to set prices
}

func (m *mockCommons) IsGroupPolicyAddress(_ context.Context, a string) bool { return m.groups[a] }
func (m *mockCommons) IsCouncilAuthorized(_ context.Context, a, council, committee string) bool {
	return council == "commons" && committee == "operations" && m.ops[a]
}

type mockDistr struct {
	bank *mockBank
	pool math.Int
	err  error
}

func (m *mockDistr) FundCommunityPool(_ context.Context, amt sdk.Coins, sender sdk.AccAddress) error {
	if m.err != nil {
		return m.err
	}
	a := amt.AmountOf(testDenom)
	if err := m.bank.move(sender.String(), "community-pool", a); err != nil {
		return err
	}
	m.pool = m.pool.Add(a)
	return nil
}

// ---- fixture --------------------------------------------------------------

type fixture struct {
	t            *testing.T
	ctx          sdk.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	bank         *mockBank
	rep          *mockRep
	service      *mockService
	commons      *mockCommons
	distr        *mockDistr
	srv          types.MsgServer
	gov          string
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	ctx = ctx.WithBlockHeight(100)

	authority := authtypes.NewModuleAddress(types.GovModuleName)
	bank := newMockBank()

	k := keeper.NewKeeper(storeService, encCfg.Codec, addressCodec, authority, bank, mockAuth{})
	f := &fixture{
		t: t, ctx: ctx, keeper: k, addressCodec: addressCodec, bank: bank,
		rep:     &mockRep{active: map[string]bool{}, trust: map[string]reptypes.TrustLevel{}, canonicalOf: map[string]string{}},
		service: &mockService{ops: map[string]servicetypes.Operator{}, slashed: map[string]bool{}, configs: map[string]servicetypes.ServiceTypeConfig{}, unbonding: map[string]int64{}, dedupe: map[string]bool{}},
		commons: &mockCommons{groups: map[string]bool{}, ops: map[string]bool{}},
		distr:   &mockDistr{bank: bank, pool: math.ZeroInt()},
		gov:     authority.String(),
	}
	f.service.configs[types.ContentScannerServiceType] = servicetypes.ServiceTypeConfig{ServiceType: types.ContentScannerServiceType, AttestationQuorum: 1}
	f.service.configs[types.ArweaveServiceType] = servicetypes.ServiceTypeConfig{ServiceType: types.ArweaveServiceType}
	f.service.unbonding[types.ArweaveServiceType] = 1 << 40
	k.SetIdentityKeeper(mockIdentity{})
	k.SetRepKeeper(f.rep)
	k.SetServiceKeeper(f.service)
	k.SetCommonsKeeper(f.commons)
	k.SetDistrKeeper(f.distr)
	require.NoError(t, k.InitGenesis(ctx, *types.DefaultGenesis()))
	f.srv = keeper.NewMsgServerImpl(k)
	return f
}

func addr(name string) string {
	b := make([]byte, 20)
	copy(b, name)
	return sdk.AccAddress(b).String()
}

func hashOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// locator returns a well-formed Arweave id derived from s.
func locator(s string) string {
	out := strings.NewReplacer("+", "-", "/", "_").Replace(hashOf(s))
	return (out + strings.Repeat("A", 43))[:43]
}

const funds = 1_000_000_000

// member makes name an active member at trust with funds.
func (f *fixture) member(name string, trust reptypes.TrustLevel) string {
	a := addr(name)
	f.rep.active[a] = true
	f.rep.trust[a] = trust
	f.bank.balances[a] = math.NewInt(funds)
	return a
}

// council registers name as a group policy address with funds.
func (f *fixture) council(name string) string {
	a := addr(name)
	f.commons.groups[a] = true
	f.bank.balances[a] = math.NewInt(funds)
	return a
}

// register puts a live ACTIVE operator record in the mock x/service.
func (f *fixture) register(a, serviceType, controller string) {
	f.service.ops[opKey(a, serviceType)] = servicetypes.Operator{
		Address: a, ServiceType: serviceType, Controller: controller,
		Status: servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE,
	}
}

func (f *fixture) setStatus(a, serviceType string, s servicetypes.OperatorStatus) {
	op := f.service.ops[opKey(a, serviceType)]
	op.Status = s
	f.service.ops[opKey(a, serviceType)] = op
}

// operator makes name an active member, a storage-arweave operator under
// controller, and approves it for the arweave backend.
func (f *fixture) operator(name, controller string) string {
	a := f.member(name, reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	f.register(a, types.ArweaveServiceType, controller)
	_, err := f.srv.ApproveOperator(f.ctx, &types.MsgApproveOperator{Authority: controller, Backend: types.ArweaveBackend, Operator: a})
	require.NoError(f.t, err)
	return a
}

// scanner registers name as a content scanner under controller and approves it.
func (f *fixture) scanner(name, controller string) string {
	a := addr(name)
	f.register(a, types.ContentScannerServiceType, controller)
	_, err := f.srv.ApproveScanner(f.ctx, &types.MsgApproveScanner{Authority: controller, Scanner: a})
	require.NoError(f.t, err)
	return a
}

// world is the usual cast: a council, one operator and one scanner it
// controls, and a requester.
type world struct{ council, operator, scanner, requester string }

func (f *fixture) world() world {
	c := f.council("council")
	return world{
		council:   c,
		operator:  f.operator("operator", c),
		scanner:   f.scanner("scanner", c),
		requester: f.member("requester", reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED),
	}
}

func (f *fixture) params() types.Params {
	p, err := f.keeper.Params.Get(f.ctx)
	require.NoError(f.t, err)
	return p
}

func (f *fixture) setParams(mut func(*types.Params)) {
	p := f.params()
	mut(&p)
	require.NoError(f.t, p.Validate())
	require.NoError(f.t, f.keeper.Params.Set(f.ctx, p))
}

func (f *fixture) backend() types.Backend {
	b, err := f.keeper.Backends.Get(f.ctx, types.ArweaveBackend)
	require.NoError(f.t, err)
	return b
}

func (f *fixture) setBackend(mut func(*types.Backend)) {
	b := f.backend()
	mut(&b)
	require.NoError(f.t, b.Validate())
	require.NoError(f.t, f.keeper.Backends.Set(f.ctx, b.Name, b))
}

func reqMsg(requester, content string) *types.MsgRequestPermanentCopy {
	return &types.MsgRequestPermanentCopy{
		Requester: requester, Backend: types.ArweaveBackend, Sha256: hashOf(content),
		SizeBytes: 1000, ContentType: "image/png", Url: "https://files.example/" + content,
		MaxFee: math.NewInt(funds),
	}
}

// request files a request for a file named content and returns its id.
func (f *fixture) request(requester, content string) uint64 {
	res, err := f.srv.RequestPermanentCopy(f.ctx, reqMsg(requester, content))
	require.NoError(f.t, err)
	return res.RequestId
}

func (f *fixture) claim(operator string, id uint64) {
	_, err := f.srv.ClaimRequest(f.ctx, &types.MsgClaimRequest{Operator: operator, RequestId: id})
	require.NoError(f.t, err)
}

func completeMsg(operator string, id uint64) *types.MsgCompleteRequest {
	return &types.MsgCompleteRequest{
		Operator: operator, RequestId: id, Locator: locator(fmt.Sprintf("copy-%d", id)),
		Attestations:    []string{hashOf(fmt.Sprintf("verdict-%d", id))},
		VerdictsLocator: locator(fmt.Sprintf("verdicts-%d", id)),
	}
}

func (f *fixture) complete(operator string, id uint64) {
	_, err := f.srv.CompleteRequest(f.ctx, completeMsg(operator, id))
	require.NoError(f.t, err)
}

// completed files, claims and completes a request.
func (f *fixture) completed(w world, content string) uint64 {
	id := f.request(w.requester, content)
	f.claim(w.operator, id)
	f.complete(w.operator, id)
	return id
}

func (f *fixture) flag(scanner, content string) {
	_, err := f.srv.FlagRemoved(f.ctx, &types.MsgFlagRemoved{Scanner: scanner, Sha256: hashOf(content), AttestationHash: hashOf("removed-" + content + scanner)})
	require.NoError(f.t, err)
}

func (f *fixture) get(id uint64) types.Request {
	r, err := f.keeper.Requests.Get(f.ctx, id)
	require.NoError(f.t, err)
	return r
}

func (f *fixture) exists(id uint64) bool {
	has, err := f.keeper.Requests.Has(f.ctx, id)
	require.NoError(f.t, err)
	return has
}

// run advances n blocks, running the EndBlocker at the end of each (the
// current block first), and checks the invariants after every block.
func (f *fixture) run(n int64) {
	for i := int64(0); i < n; i++ {
		require.NoError(f.t, f.keeper.EndBlocker(f.ctx))
		f.checkInvariants()
		f.ctx = f.ctx.WithBlockHeight(f.ctx.BlockHeight() + 1).WithEventManager(sdk.NewEventManager())
	}
}

// runTo advances to height h, so that the EndBlocker of h-1 has run.
func (f *fixture) runTo(h int64) {
	require.Greater(f.t, h, f.ctx.BlockHeight())
	f.run(h - f.ctx.BlockHeight())
}

func (f *fixture) checkInvariants() {
	msg, broken := keeper.AllInvariants(f.keeper)(f.ctx)
	require.False(f.t, broken, msg)
}

func (f *fixture) bal(a string) int64 { return f.bank.balance(a).Int64() }

func (f *fixture) escrow() int64 { return f.bank.balance(moduleAddr().String()).Int64() }

// hasEvent reports whether the current block emitted an event of that type.
func (f *fixture) hasEvent(name string) bool {
	for _, e := range f.ctx.EventManager().Events() {
		if e.Type == name {
			return true
		}
	}
	return false
}

func copyFee() int64 { return types.DefaultArweaveBackend().MinFee.Int64() }
func reqFee() int64  { return types.DefaultRequestFee.Int64() }
