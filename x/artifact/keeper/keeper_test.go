package keeper_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
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
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
	reptypes "sparkdream/x/rep/types"
)

const (
	bond  = "uspark"
	dream = "udream"
)

// --- Bank mock --------------------------------------------------------------

type mockBank struct {
	balances map[string]sdk.Coins
	blocked  map[string]bool
	burned   sdk.Coins
}

func newMockBank() *mockBank {
	return &mockBank{balances: map[string]sdk.Coins{}, blocked: map[string]bool{}}
}

func (b *mockBank) fund(addr sdk.AccAddress, amt int64) {
	b.balances[addr.String()] = b.balances[addr.String()].Add(sdk.NewInt64Coin(bond, amt))
}

func (b *mockBank) bal(addr sdk.AccAddress) math.Int {
	return b.balances[addr.String()].AmountOf(bond)
}

func (b *mockBank) move(from, to string, amt sdk.Coins) error {
	have := b.balances[from]
	if !have.IsAllGTE(amt) {
		return fmt.Errorf("insufficient funds: %s < %s", have, amt)
	}
	b.balances[from] = have.Sub(amt...)
	b.balances[to] = b.balances[to].Add(amt...)
	return nil
}

func moduleAddr(name string) string { return authtypes.NewModuleAddress(name).String() }

func (b *mockBank) SpendableCoins(_ context.Context, a sdk.AccAddress) sdk.Coins {
	return b.balances[a.String()]
}
func (b *mockBank) GetBalance(_ context.Context, a sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, b.balances[a.String()].AmountOf(denom))
}
func (b *mockBank) SendCoins(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	return b.move(from.String(), to.String(), amt)
}
func (b *mockBank) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, mod string, amt sdk.Coins) error {
	return b.move(from.String(), moduleAddr(mod), amt)
}
func (b *mockBank) SendCoinsFromModuleToAccount(_ context.Context, mod string, to sdk.AccAddress, amt sdk.Coins) error {
	return b.move(moduleAddr(mod), to.String(), amt)
}
func (b *mockBank) BurnCoins(_ context.Context, mod string, amt sdk.Coins) error {
	if err := b.move(moduleAddr(mod), "burned", amt); err != nil {
		return err
	}
	b.burned = b.burned.Add(amt...)
	return nil
}
func (b *mockBank) BlockedAddr(a sdk.AccAddress) bool { return b.blocked[a.String()] }

// --- Rep mock ---------------------------------------------------------------

type mockRep struct {
	members   map[string]reptypes.TrustLevel
	sentinels map[string]math.Int // available bond
	reserved  map[string]math.Int
	slashed   map[string]math.Int
	actions   []string
	outcomes  []string
	cooldown  map[string]int64
	appeals   []string // "<action type>:<target>" opened via CreateGovActionAppeal
}

func newMockRep() *mockRep {
	return &mockRep{
		members: map[string]reptypes.TrustLevel{}, sentinels: map[string]math.Int{},
		reserved: map[string]math.Int{}, slashed: map[string]math.Int{}, cooldown: map[string]int64{},
	}
}

func (r *mockRep) IsActiveMember(_ context.Context, a sdk.AccAddress) bool {
	_, ok := r.members[a.String()]
	return ok
}
func (r *mockRep) GetTrustLevel(_ context.Context, a sdk.AccAddress) (reptypes.TrustLevel, error) {
	lvl, ok := r.members[a.String()]
	if !ok {
		return 0, fmt.Errorf("not a member")
	}
	return lvl, nil
}
func (r *mockRep) EligibleForRole(_ context.Context, _ reptypes.RoleType, addr string) (reptypes.BondedRole, error) {
	if _, ok := r.sentinels[addr]; !ok {
		return reptypes.BondedRole{}, fmt.Errorf("not a sentinel")
	}
	return reptypes.BondedRole{Address: addr}, nil
}
func (r *mockRep) RoleOverturnCooldownUntil(_ context.Context, _ reptypes.RoleType, addr string) int64 {
	return r.cooldown[addr]
}
func (r *mockRep) GetAvailableBond(_ context.Context, _ reptypes.RoleType, addr string) (math.Int, error) {
	v, ok := r.sentinels[addr]
	if !ok {
		return math.ZeroInt(), fmt.Errorf("no bond")
	}
	return v, nil
}
func (r *mockRep) ReserveBond(_ context.Context, _ reptypes.RoleType, addr string, amt math.Int) error {
	if r.sentinels[addr].LT(amt) {
		return fmt.Errorf("insufficient bond")
	}
	r.sentinels[addr] = r.sentinels[addr].Sub(amt)
	r.reserved[addr] = r.reservedOf(addr).Add(amt)
	return nil
}
func (r *mockRep) reservedOf(addr string) math.Int {
	if v, ok := r.reserved[addr]; ok {
		return v
	}
	return math.ZeroInt()
}
func (r *mockRep) ReleaseBond(_ context.Context, _ reptypes.RoleType, addr string, amt math.Int) error {
	r.reserved[addr] = r.reservedOf(addr).Sub(amt)
	r.sentinels[addr] = r.sentinels[addr].Add(amt)
	return nil
}
func (r *mockRep) SlashBond(_ context.Context, _ reptypes.RoleType, addr string, amt math.Int, _ string) error {
	r.reserved[addr] = r.reservedOf(addr).Sub(amt)
	prev, ok := r.slashed[addr]
	if !ok {
		prev = math.ZeroInt()
	}
	r.slashed[addr] = prev.Add(amt)
	return nil
}
func (r *mockRep) RecordRoleAction(_ context.Context, _ reptypes.RoleType, addr, kind string) error {
	r.actions = append(r.actions, kind)
	return nil
}
func (r *mockRep) CreateGovActionAppeal(_ context.Context, actionType reptypes.GovActionType, target string, _ sdk.AccAddress, _ string) (uint64, uint64, error) {
	r.appeals = append(r.appeals, actionType.String()+":"+target)
	return uint64(len(r.appeals)), uint64(100 + len(r.appeals)), nil
}

func (r *mockRep) RecordRoleOutcome(_ context.Context, _ reptypes.RoleType, addr, kind string, upheld bool) error {
	r.outcomes = append(r.outcomes, fmt.Sprintf("%s:%t", kind, upheld))
	return nil
}

// --- Commons / identity / distribution mocks --------------------------------

type mockCommons struct {
	ops     map[string]bool
	council map[string]bool
}

func (c *mockCommons) IsCouncilAuthorized(_ context.Context, addr, _, _ string) bool {
	return c.ops[addr]
}
func (c *mockCommons) IsCouncilBodyPolicy(_ context.Context, addr string) bool {
	return c.council[addr]
}

type mockIdentity struct{}

func (mockIdentity) BondDenom(context.Context) string  { return bond }
func (mockIdentity) DreamDenom(context.Context) string { return dream }

type mockDistr struct {
	bank *mockBank
	pool sdk.Coins
}

func (d *mockDistr) FundCommunityPool(_ context.Context, amt sdk.Coins, from sdk.AccAddress) error {
	if err := d.bank.move(from.String(), "community_pool", amt); err != nil {
		return err
	}
	d.pool = d.pool.Add(amt...)
	return nil
}

// --- Fixture ----------------------------------------------------------------

type fixture struct {
	t       *testing.T
	ctx     sdk.Context
	keeper  keeper.Keeper
	msg     types.MsgServer
	query   types.QueryServer
	codec   address.Codec
	bank    *mockBank
	rep     *mockRep
	commons *mockCommons
	distr   *mockDistr
	gov     string

	alice, bob, carol, dave, ops, council, sentinel sdk.AccAddress
}

func addr(name string) sdk.AccAddress {
	b := make([]byte, 20)
	copy(b, name)
	return b
}

func initFixture(t *testing.T) *fixture {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, cms.LoadLatestVersion())
	ctx := sdk.NewContext(cms, cmtproto.Header{Height: 100, Time: time.Unix(1_700_000_000, 0)}, false, log.NewNopLogger())

	cdc := codectestutil.CodecOptions{}.NewCodec()
	ac := addresscodec.NewBech32Codec("cosmos")
	gov := authtypes.NewModuleAddress(types.GovModuleName)

	bank := newMockBank()
	rep := newMockRep()
	commons := &mockCommons{ops: map[string]bool{}, council: map[string]bool{}}
	distr := &mockDistr{bank: bank}

	k := keeper.NewKeeper(runtime.NewKVStoreService(storeKey), cdc, ac, gov, nil, bank)
	k.SetIdentityKeeper(mockIdentity{})
	k.SetRepKeeper(rep)
	k.SetCommonsKeeper(commons)
	k.SetDistrKeeper(distr)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	f := &fixture{
		t: t, ctx: ctx, keeper: k, msg: keeper.NewMsgServerImpl(k), query: keeper.NewQueryServerImpl(k),
		codec: ac, bank: bank, rep: rep, commons: commons, distr: distr, gov: gov.String(),
		alice: addr("alice"), bob: addr("bob"), carol: addr("carol"), dave: addr("dave"),
		ops: addr("ops"), council: addr("council"), sentinel: addr("sentinel"),
	}
	// alice and bob are members; carol is a provisional member; dave is not.
	rep.members[f.alice.String()] = reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED
	rep.members[f.bob.String()] = reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED
	rep.members[f.carol.String()] = reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL
	commons.ops[f.ops.String()] = true
	commons.council[f.council.String()] = true
	rep.sentinels[f.sentinel.String()] = math.NewInt(1_000_000_000)
	for _, a := range []sdk.AccAddress{f.alice, f.bob, f.carol, f.dave, f.ops, f.council, f.sentinel} {
		bank.fund(a, 1_000_000_000)
	}
	return f
}

// checkInvariants fails the test if any module invariant is broken.
func (f *fixture) checkInvariants() {
	f.t.Helper()
	msg, broken := keeper.AllInvariants(f.keeper)(f.ctx)
	require.False(f.t, broken, msg)
}

// advance moves block time forward by secs and height by blocks.
func (f *fixture) advance(secs, blocks int64) {
	f.ctx = f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(secs) * time.Second)).
		WithBlockHeight(f.ctx.BlockHeight() + blocks)
}

func (f *fixture) endBlock() {
	f.t.Helper()
	require.NoError(f.t, f.keeper.EndBlocker(f.ctx))
	f.checkInvariants()
}

func (f *fixture) params() types.Params { return f.keeper.GetParams(f.ctx) }

func (f *fixture) setParams(mut func(p *types.Params)) {
	p := f.params()
	mut(&p)
	require.NoError(f.t, f.keeper.Params.Set(f.ctx, p))
}

func spark(n int64) sdk.Coin { return sdk.NewInt64Coin(bond, n*1_000_000) }

// defaultClass is a transferable, mutable, unlimited class.
func (f *fixture) createClass(owner sdk.AccAddress, mut ...func(*types.MsgCreateClass)) uint64 {
	f.t.Helper()
	msg := &types.MsgCreateClass{
		Creator:                owner.String(),
		Name:                   "Phoenix Editions",
		Symbol:                 "PHX",
		Description:            "Aurora studies",
		Uri:                    "ipfs://bafyclasscover",
		Flags:                  types.ClassFlags{Transferable: true, TokenMetadataMutable: true},
		RoyaltyBps:             500,
		AcceptedContentLicense: types.ContentLicense,
	}
	for _, m := range mut {
		m(msg)
	}
	resp, err := f.msg.CreateClass(f.ctx, msg)
	require.NoError(f.t, err)
	return resp.ClassId
}

func meta(name string) types.TokenMetadata {
	return types.TokenMetadata{Name: name, Uri: "ipfs://bafy" + name, Attributes: []types.Attribute{{Key: "edition", Value: name}}}
}

// mint mints n tokens of classID from minter to recipient and returns the results.
func (f *fixture) mint(minter sdk.AccAddress, classID uint64, recipient sdk.AccAddress, n int) []types.MintResult {
	f.t.Helper()
	entries := make([]types.MintEntry, n)
	for i := range entries {
		entries[i] = types.MintEntry{Recipient: recipient.String(), Metadata: meta(fmt.Sprintf("t%d", i))}
	}
	resp, err := f.msg.Mint(f.ctx, &types.MsgMint{
		Minter: minter.String(), ClassId: classID, Entries: entries, AcceptedContentLicense: types.ContentLicense,
	})
	require.NoError(f.t, err)
	return resp.Results
}

func (f *fixture) token(classID, tokenID uint64) types.Token {
	f.t.Helper()
	tok, found, err := f.keeper.GetToken(f.ctx, classID, tokenID)
	require.NoError(f.t, err)
	require.True(f.t, found, "token %d/%d not found", classID, tokenID)
	return tok
}

func (f *fixture) class(classID uint64) types.Class {
	f.t.Helper()
	c, found, err := f.keeper.GetClass(f.ctx, classID)
	require.NoError(f.t, err)
	require.True(f.t, found)
	return c
}

func (f *fixture) moduleBalance() math.Int {
	return f.bank.bal(authtypes.NewModuleAddress(types.ModuleName))
}

func sdkCoin(denom string, n int64) sdk.Coin { return sdk.NewInt64Coin(denom, n) }

func keeperPair(a, b uint64) collections.Pair[uint64, uint64] { return collections.Join(a, b) }

type keeperPairT = collections.Pair[uint64, uint64]

func u64s(v uint64) string { return strconv.FormatUint(v, 10) }
