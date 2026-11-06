package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v10/modules/core/23-commitment/types"
	host "github.com/cosmos/ibc-go/v10/modules/core/24-host"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"
	ibctypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"

	"sparkdream/x/federation/keeper"
	module "sparkdream/x/federation/module"
	"sparkdream/x/federation/types"
)

type fixture struct {
	ctx          context.Context
	keeper       keeper.Keeper
	addressCodec address.Codec
	authority    string
	repKeeper    *mockRepKeeper
	bankKeeper   *mockBankKeeper
	authKeeper   *mockAuthKeeper
	// commonsKeeper is exposed so authorization tests can replace the
	// permissive default stubs with ones that actually discriminate
	// between a committee POLICY and an individual committee member.
	commonsKeeper *mockCommonsKeeper
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	// ibctm registers the Tendermint ClientState so the fixture IBC keeper
	// can store the light client a peer's channel is pinned to (FED-106).
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{}, ibctm.AppModuleBasic{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(govtypes.ModuleName)
	mockUpgradeKeeper := newMockUpgradeKeeper()
	bankKeeper := &mockBankKeeper{balances: make(map[string]sdk.Coins)}
	authKeeper := &mockAuthKeeper{addressCodec: addressCodec}

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		authKeeper,
		bankKeeper,
		func() *ibckeeper.Keeper {
			return ibckeeper.NewKeeper(encCfg.Codec, storeService, newMockParams(), mockUpgradeKeeper, authority.String())
		},
	)

	// Wire mock commons keeper for authorization tests
	commonsKeeper := &mockCommonsKeeper{}
	k.SetCommonsKeeper(commonsKeeper)
	repKeeper := &mockRepKeeper{}
	k.SetRepKeeper(repKeeper)
	k.SetIdentityKeeper(&mockIdentityKeeper{})

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	authorityStr, _ := addressCodec.BytesToString(authority)

	return &fixture{
		ctx:          ctx,
		keeper:       k,
		addressCodec: addressCodec,
		authority:    authorityStr,
		repKeeper:    repKeeper,
		bankKeeper:   bankKeeper,
		authKeeper:   authKeeper,

		commonsKeeper: commonsKeeper,
	}
}

// --- Mock Keepers ---

type mockAuthKeeper struct {
	addressCodec address.Codec
	// missing lists addresses with no account. Every other address has one,
	// so the identity-challenge existence check passes by default.
	missing map[string]bool
}

func (m *mockAuthKeeper) AddressCodec() address.Codec {
	return m.addressCodec
}

func (m *mockAuthKeeper) GetAccount(_ context.Context, addr sdk.AccAddress) sdk.AccountI {
	if m.missing[addr.String()] {
		return nil
	}
	return authtypes.NewBaseAccountWithAddress(addr)
}

func (m *mockAuthKeeper) GetModuleAddress(name string) sdk.AccAddress {
	return authtypes.NewModuleAddress(name)
}

type mockBankKeeper struct {
	balances map[string]sdk.Coins
	metadata map[string]banktypes.Metadata

	// moduleSends counts module→account sends (fee refunds and payouts),
	// so settlement tests can tell a single payout from a double one.
	// failModuleSends makes every such send fail.
	moduleSends     int
	failModuleSends bool
}

func (m *mockBankKeeper) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[addr.String()]
}

// GetBalance reads a single denom. Backed by the same map SpendableCoins
// serves, so operator-reward tests can seed a pool and assert it drained.
func (m *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.balances[addr.String()].AmountOf(denom))
}

// SendCoins actually MOVES balance in the mock. It used to be a no-op, which
// was fine while nothing federation did depended on the result; the
// bridge-operator reward distribution does, and a no-op mock would let a
// broken payout pass its own test.
func (m *mockBankKeeper) SendCoins(_ context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	if m.balances == nil {
		m.balances = map[string]sdk.Coins{}
	}
	fromBal := m.balances[from.String()]
	if !fromBal.IsAllGTE(amt) {
		return fmt.Errorf("mock bank: insufficient funds for %s", from.String())
	}
	m.balances[from.String()] = fromBal.Sub(amt...)
	m.balances[to.String()] = m.balances[to.String()].Add(amt...)
	return nil
}

// SendCoinsFromAccountToModule debits when the source can cover it and
// otherwise no-ops.
//
// Deliberately laxer than SendCoins above. This is the fee-ESCROW path, and
// the challenge / arbiter tests that drive it never fund their signers — they
// are about verdict logic, not bank accounting, and a real bank would reject
// them. Keeping it permissive avoids making every one of those tests carry
// balance setup it has no interest in, while the reward-pool tests that DO
// care seed the pool and get a real debit.
func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, from sdk.AccAddress, _ string, amt sdk.Coins) error {
	if m.balances == nil {
		m.balances = map[string]sdk.Coins{}
	}
	fromBal := m.balances[from.String()]
	if fromBal.IsAllGTE(amt) {
		m.balances[from.String()] = fromBal.Sub(amt...)
	}
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
	if m.failModuleSends {
		return fmt.Errorf("mock bank: module send failed")
	}
	m.moduleSends++
	return nil
}

func (m *mockBankKeeper) BurnCoins(_ context.Context, _ string, _ sdk.Coins) error {
	return nil
}

func (m *mockBankKeeper) SetDenomMetaData(_ context.Context, md banktypes.Metadata) {
	if m.metadata == nil {
		m.metadata = make(map[string]banktypes.Metadata)
	}
	m.metadata[md.Base] = md
}

func (m *mockBankKeeper) GetDenomMetaData(_ context.Context, denom string) (banktypes.Metadata, bool) {
	md, ok := m.metadata[denom]
	return md, ok
}

// mockIdentityKeeper implements types.IdentityKeeper for testing.
// Always returns "uspark"/"udream" — the legacy denoms unit tests assume.
// E2E tests run against a real chain with the real identity keeper.
type mockIdentityKeeper struct{}

func (mockIdentityKeeper) IsIdentityKeeper()                      {}
func (m *mockIdentityKeeper) BondDenom(_ context.Context) string  { return "uspark" }
func (m *mockIdentityKeeper) DreamDenom(_ context.Context) string { return "udream" }

type mockCommonsKeeper struct {
	// IsGroupPolicyAddressFn lets specific tests override the default.
	// Default: every address is treated as a registered group policy
	// address, which lets tests exercise the happy path of any code
	// that takes a `controller_group` input without explicit setup.
	IsGroupPolicyAddressFn func(ctx context.Context, addr string) bool

	// GetCouncilPolicyAddressFn lets tests inject a specific policy
	// address. Default: returns a synthetic address so federation's
	// MsgRegisterBridge controller-resolution fall-back path works in
	// unit tests without standing up the full commons bootstrap.
	GetCouncilPolicyAddressFn func(ctx context.Context, council string, committee string) (string, bool)

	// IsCouncilOrCommitteePolicyFn lets a test discriminate between an
	// individual committee member and the committee POLICY. Default: true,
	// matching IsCouncilAuthorized, so existing tests are unaffected -- any
	// test asserting the stricter gate must override this, or it proves
	// nothing.
	IsCouncilOrCommitteePolicyFn func(ctx context.Context, addr string, council string, committee string) bool

	// IsCouncilAuthorizedFn overrides the permissive default below.
	IsCouncilAuthorizedFn func(ctx context.Context, addr string, council string, committee string) bool
}

func (m *mockCommonsKeeper) IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council, committee string) bool {
	if m.IsCouncilOrCommitteePolicyFn != nil {
		return m.IsCouncilOrCommitteePolicyFn(ctx, addr, council, committee)
	}
	return true
}

func (m *mockCommonsKeeper) IsCouncilAuthorized(ctx context.Context, addr string, council, committee string) bool {
	if m.IsCouncilAuthorizedFn != nil {
		return m.IsCouncilAuthorizedFn(ctx, addr, council, committee)
	}
	// In tests, authority is always authorized
	return true
}

func (m *mockCommonsKeeper) IsGroupPolicyAddress(ctx context.Context, addr string) bool {
	if m.IsGroupPolicyAddressFn != nil {
		return m.IsGroupPolicyAddressFn(ctx, addr)
	}
	return true
}

// testOpsCommPolicyAddress is the synthetic Operations Committee policy
// address returned by the default mockCommonsKeeper. Stable across the
// suite so tests can assert against the resolved controller.
const testOpsCommPolicyAddress = "cosmos1opcommopcommopcommopcommopcommop6vnq06"

func (m *mockCommonsKeeper) GetCouncilPolicyAddress(ctx context.Context, council string, committee string) (string, bool) {
	if m.GetCouncilPolicyAddressFn != nil {
		return m.GetCouncilPolicyAddressFn(ctx, council, committee)
	}
	if council == "commons" && committee == "operations" {
		return testOpsCommPolicyAddress, true
	}
	return "", false
}

type mockRepKeeper struct {
	// In-memory BondedRole state keyed by (roleType, addr). Tests seed this
	// via SeedBondedRole to simulate a verifier having bonded via x/rep.
	bondedRoles map[string]reptypes.BondedRole
	// BondedRoleConfig write-throughs land here keyed by role_type; the
	// bonded_role_sync tests assert against this map.
	bondedRoleConfigs map[reptypes.RoleType]reptypes.BondedRoleConfig
	// Shared RoleActivity records keyed by (roleType, addr). Federation
	// reports actions and verdicts here since the verifier's accountability
	// state moved to x/rep.
	roleActivities map[string]reptypes.RoleActivity
	// blockTime the mock stamps cooldowns against. Tests that assert on
	// cooldown values set this to the same time the keeper ctx uses.
	blockTime int64

	aliases   map[string]string
	inactive  map[string]bool
	repScores map[string]map[string]string
}

// The mock mirrors x/rep's RecordRoleOutcome closely enough for federation's
// handlers to be exercised end to end, but it is NOT the specification: the
// authoritative tests for streak reset, escalating cooldown, ring stamping and
// streak demotion live in x/rep/keeper/role_activity_test.go, next to the
// implementation. Federation's tests assert that federation REPORTS the right
// action and verdict, and that it reads the results back correctly.

func (m *mockRepKeeper) RecordRoleAction(_ context.Context, roleType reptypes.RoleType, addr, kind string) error {
	ra := m.getOrInitRoleActivity(roleType, addr)
	ra.EpochActions[kind]++
	ra.TotalActions[kind]++
	m.roleActivities[mockBondedRoleKey(roleType, addr)] = ra
	return nil
}

func (m *mockRepKeeper) RecordRoleOutcome(_ context.Context, roleType reptypes.RoleType, addr, kind string, upheld bool) error {
	cfg := m.bondedRoleConfigs[roleType]
	ra := m.getOrInitRoleActivity(roleType, addr)
	ra.EpochAppealsResolved++

	if upheld {
		ra.UpheldActions[kind]++
		ra.ConsecutiveUpheld++
		needed := cfg.UpheldToResetOverturns
		if needed == 0 {
			needed = 1
		}
		if ra.ConsecutiveUpheld >= needed {
			ra.ConsecutiveOverturns = 0
		}
	} else {
		ra.OverturnedActions[kind]++
		ra.ConsecutiveOverturns++
		ra.ConsecutiveUpheld = 0
		if reptypes.CooldownOnOverturn[kind] {
			base := cfg.OverturnBaseCooldown
			if base <= 0 {
				base = reptypes.DefaultRoleOverturnCooldown
			}
			cooldown := base
			if cfg.OverturnCooldownEscalates && ra.ConsecutiveOverturns > 1 {
				shift := ra.ConsecutiveOverturns - 1
				if shift > 20 {
					shift = 20
				}
				cooldown = base * (int64(1) << shift)
				if cooldown > reptypes.MaxRoleOverturnCooldown || cooldown < 0 {
					cooldown = reptypes.MaxRoleOverturnCooldown
				}
			}
			ra.OverturnCooldownUntil = m.blockTime + cooldown
		}
	}
	m.roleActivities[mockBondedRoleKey(roleType, addr)] = ra
	return nil
}

// SeedRoleActivity inserts a RoleActivity record directly, standing in for
// accumulated reporting from federation's handlers.
func (m *mockRepKeeper) SeedRoleActivity(roleType reptypes.RoleType, addr string, ra reptypes.RoleActivity) error {
	if m.roleActivities == nil {
		m.roleActivities = make(map[string]reptypes.RoleActivity)
	}
	ra.RoleType = roleType
	ra.Address = addr
	m.roleActivities[mockBondedRoleKey(roleType, addr)] = ra
	return nil
}

func (m *mockRepKeeper) GetRoleActivity(_ context.Context, roleType reptypes.RoleType, addr string) (reptypes.RoleActivity, error) {
	if ra, ok := m.roleActivities[mockBondedRoleKey(roleType, addr)]; ok {
		return ra, nil
	}
	return reptypes.RoleActivity{RoleType: roleType, Address: addr}, nil
}

func (m *mockRepKeeper) RoleOverturnCooldownUntil(_ context.Context, roleType reptypes.RoleType, addr string) int64 {
	return m.roleActivities[mockBondedRoleKey(roleType, addr)].OverturnCooldownUntil
}

func (m *mockRepKeeper) getOrInitRoleActivity(roleType reptypes.RoleType, addr string) reptypes.RoleActivity {
	if m.roleActivities == nil {
		m.roleActivities = make(map[string]reptypes.RoleActivity)
	}
	ra, ok := m.roleActivities[mockBondedRoleKey(roleType, addr)]
	if !ok {
		ra = reptypes.RoleActivity{RoleType: roleType, Address: addr}
	}
	if ra.EpochActions == nil {
		ra.EpochActions = map[string]uint64{}
	}
	if ra.TotalActions == nil {
		ra.TotalActions = map[string]uint64{}
	}
	if ra.UpheldActions == nil {
		ra.UpheldActions = map[string]uint64{}
	}
	if ra.OverturnedActions == nil {
		ra.OverturnedActions = map[string]uint64{}
	}
	return ra
}

// SeedBondedRole inserts a BondedRole record into the mock keyed by
// (roleType, addr). Used by tests to stand in for a verifier having bonded
// via x/rep before federation runs its handlers.
func (m *mockRepKeeper) SeedBondedRole(roleType reptypes.RoleType, addr string, role reptypes.BondedRole) {
	if m.bondedRoles == nil {
		m.bondedRoles = make(map[string]reptypes.BondedRole)
	}
	m.bondedRoles[mockBondedRoleKey(roleType, addr)] = role
}

func mockBondedRoleKey(roleType reptypes.RoleType, addr string) string {
	return reptypes.RoleType_name[int32(roleType)] + "/" + addr
}

// --- Alias contract and membership (RepMemberKeeper) ---
//
// aliases maps an address a member migrated AWAY from to the address that
// now carries the member; inactive marks zeroed/inactive members;
// repScores seeds per-tag reputation.

func (m *mockRepKeeper) canonical(addr string) string {
	if to, ok := m.aliases[addr]; ok {
		return to
	}
	return addr
}

func (m *mockRepKeeper) CanActAs(_ context.Context, signer, stored string) bool {
	if signer == "" || stored == "" {
		return false
	}
	if _, tombstoned := m.aliases[signer]; tombstoned {
		return false
	}
	return m.canonical(stored) == signer
}

func (m *mockRepKeeper) SameMember(_ context.Context, a, b string) bool {
	return a != "" && b != "" && m.canonical(a) == m.canonical(b)
}

func (m *mockRepKeeper) IsActiveMember(_ context.Context, addr sdk.AccAddress) bool {
	return !m.inactive[addr.String()]
}

func (m *mockRepKeeper) GetMember(_ context.Context, addr sdk.AccAddress) (reptypes.Member, error) {
	return reptypes.Member{Address: addr.String(), JoinedAt: 1_700_000_000, ReputationScores: m.repScores[addr.String()]}, nil
}

func (m *mockRepKeeper) GetTrustLevel(_ context.Context, _ sdk.AccAddress) (reptypes.TrustLevel, error) {
	return reptypes.TrustLevel(3), nil // TRUSTED
}

func (m *mockRepKeeper) MintDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

func (m *mockRepKeeper) BurnDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

func (m *mockRepKeeper) LockDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

func (m *mockRepKeeper) UnlockDREAM(_ context.Context, _ sdk.AccAddress, _ math.Int) error {
	return nil
}

// --- BondedRole stubs (verifier is ROLE_TYPE_FEDERATION_VERIFIER). ---

func (m *mockRepKeeper) GetBondedRole(_ context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error) {
	if br, ok := m.bondedRoles[mockBondedRoleKey(roleType, addr)]; ok {
		return br, nil
	}
	return reptypes.BondedRole{}, reptypes.ErrBondedRoleNotFound
}

// EligibleForRole mirrors x/rep's Keeper.EligibleForRole against the mock's
// bondedRoles map: NORMAL/RECOVERY pass, UNBONDING passes while the staying
// bond (current - pending) covers the role minimum (missing config = zero
// minimum, matching the real keeper's lenient fallback), DEMOTED fails.
func (m *mockRepKeeper) EligibleForRole(_ context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error) {
	br, ok := m.bondedRoles[mockBondedRoleKey(roleType, addr)]
	if !ok {
		return reptypes.BondedRole{}, reptypes.ErrBondedRoleNotFound
	}
	switch br.BondStatus {
	case reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
		reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY:
		return br, nil
	case reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING:
		current, cok := math.NewIntFromString(br.CurrentBond)
		if !cok {
			current = math.ZeroInt()
		}
		pending := math.ZeroInt()
		if p, pok := math.NewIntFromString(br.PendingUnbondAmount); pok {
			pending = p
		}
		minBond := math.ZeroInt()
		if cfg, cok := m.bondedRoleConfigs[roleType]; cok {
			if mb, mok := math.NewIntFromString(cfg.MinBond); mok {
				minBond = mb
			}
		}
		if current.Sub(pending).LT(minBond) {
			return br, reptypes.ErrRoleUnbondingBelowMin
		}
		return br, nil
	default:
		return br, reptypes.ErrRoleDemoted
	}
}

func (m *mockRepKeeper) ReserveBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	current, _ := math.NewIntFromString(br.CurrentBond)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	if current.Sub(committed).LT(amount) {
		return reptypes.ErrInsufficientBond
	}
	br.TotalCommittedBond = committed.Add(amount).String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) ReleaseBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	released := committed.Sub(amount)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	br.TotalCommittedBond = released.String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) SlashBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int, _ string) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	current, _ := math.NewIntFromString(br.CurrentBond)
	committed, _ := math.NewIntFromString(br.TotalCommittedBond)
	if committed.IsNil() {
		committed = math.ZeroInt()
	}
	slash := amount
	if slash.GT(current) {
		slash = current
	}
	br.CurrentBond = current.Sub(slash).String()
	released := committed.Sub(slash)
	if released.IsNegative() {
		released = math.ZeroInt()
	}
	br.TotalCommittedBond = released.String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) IncreaseBond(_ context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	current, _ := math.NewIntFromString(br.CurrentBond)
	if current.IsNil() {
		current = math.ZeroInt()
	}
	br.CurrentBond = current.Add(amount).String()
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) RecordRewardPayout(_ context.Context, roleType reptypes.RoleType, addr string, epoch int64, amount math.Int) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return nil
	}
	prev, _ := math.NewIntFromString(br.CumulativeRewards)
	if prev.IsNil() {
		prev = math.ZeroInt()
	}
	br.CumulativeRewards = prev.Add(amount).String()
	br.LastRewardEpoch = epoch
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) RecordActivity(_ context.Context, _ reptypes.RoleType, _ string) error {
	return nil
}

func (m *mockRepKeeper) SetBondStatus(_ context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error {
	key := mockBondedRoleKey(roleType, addr)
	br, ok := m.bondedRoles[key]
	if !ok {
		return reptypes.ErrBondedRoleNotFound
	}
	br.BondStatus = status
	br.DemotionCooldownUntil = cooldownUntil
	m.bondedRoles[key] = br
	return nil
}

func (m *mockRepKeeper) SetBondedRoleConfig(_ context.Context, cfg reptypes.BondedRoleConfig) error {
	if m.bondedRoleConfigs == nil {
		m.bondedRoleConfigs = make(map[reptypes.RoleType]reptypes.BondedRoleConfig)
	}
	m.bondedRoleConfigs[cfg.RoleType] = cfg
	return nil
}

// --- IBC Mocks ---

type mockUpgradeKeeper struct {
	clienttypes.UpgradeKeeper
	initialized bool
}

func (m mockUpgradeKeeper) GetUpgradePlan(_ context.Context) (upgradetypes.Plan, error) {
	return upgradetypes.Plan{}, nil
}

func newMockUpgradeKeeper() *mockUpgradeKeeper {
	return &mockUpgradeKeeper{initialized: true}
}

type mockParams struct {
	ibctypes.ParamSubspace
	initialized bool
}

func newMockParams() *mockParams {
	return &mockParams{initialized: true}
}

func (mockParams) GetParamSet(_ sdk.Context, _ paramtypes.ParamSet) {}

// --- Test Helpers ---

// activateTestPeer moves a freshly registered peer from PENDING to ACTIVE.
// Registration alone no longer makes a peer usable and RegisterBridge no
// longer auto-activates, so every fixture that binds a bridge or moves
// content has to walk the real lifecycle: register, activate, then bind.
// f.authority is the gov authority, which satisfies the committee-policy
// gate on ResumePeer directly.
func activateTestPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	if peer, err := f.keeper.Peers.Get(f.ctx, peerID); err == nil &&
		peer.Type == types.PeerType_PEER_TYPE_SPARK_DREAM && peer.IbcChannelId != "" {
		openTestChannelTo(t, f, peer.IbcChannelId, channeltypes.OPEN, peer.ExpectedChainId)
		if peer.IbcTransferChannelId != "" {
			openTestChannelOn(t, f, ibctransfertypes.PortID, peer.IbcTransferChannelId, channeltypes.OPEN, peer.ExpectedChainId)
		}
	}
	_, err := ms.ResumePeer(f.ctx, &types.MsgResumePeer{
		Authority: f.authority,
		PeerId:    peerID,
	})
	require.NoError(t, err)
}

// testPeerChainID is the chain id every Spark Dream test peer is registered
// for; activateTestPeer puts a light client for it behind the channel.
const testPeerChainID = "phoenix-1"

// openTestChannelTo writes a federation-port channel in the given state
// into the fixture's IBC store, backed by a Tendermint client for chainID;
// a Spark Dream peer's activation requires its channel to be OPEN and its
// client to name the peer's expected_chain_id (FED-106). The connection
// and client are keyed on the channel so two channels never share a client.
func openTestChannelTo(t *testing.T, f *fixture, channelID string, state channeltypes.State, chainID string) {
	t.Helper()
	openTestChannelOn(t, f, types.PortID, channelID, state, chainID)
}

// openTestChannelOn is openTestChannelTo on an arbitrary port; the ICS-20
// transfer channel of a Spark Dream peer is verified the same way at
// activation (FED-201). The connection and client are keyed on port and
// channel so no two channels share a client.
func openTestChannelOn(t *testing.T, f *fixture, port, channelID string, state channeltypes.State, chainID string) {
	t.Helper()
	ibcK := f.keeper.IBCKeeperForTest()
	require.NotNil(t, ibcK)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	connID := "connection-" + port + "-" + channelID
	clientID := "07-tendermint-" + port + "-" + channelID
	ibcK.ChannelKeeper.SetChannel(sdkCtx, port, channelID, channeltypes.NewChannel(
		state, channeltypes.UNORDERED, channeltypes.NewCounterparty(port, "channel-0"),
		[]string{connID}, types.Version))
	ibcK.ConnectionKeeper.SetConnection(sdkCtx, connID, connectiontypes.NewConnectionEnd(
		connectiontypes.OPEN, clientID,
		connectiontypes.NewCounterparty(clientID, "connection-0", commitmenttypes.NewMerklePrefix([]byte("ibc"))),
		nil, 0))
	ibcK.ClientKeeper.SetClientState(sdkCtx, clientID, &ibctm.ClientState{ChainId: chainID})
}

// dropTestClient removes the light client behind a channel opened with
// openTestChannelTo, leaving a channel whose chain cannot be resolved.
func dropTestClient(t *testing.T, f *fixture, channelID string) {
	t.Helper()
	ibcK := f.keeper.IBCKeeperForTest()
	require.NotNil(t, ibcK)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	ibcK.ClientKeeper.ClientStore(sdkCtx, "07-tendermint-"+types.PortID+"-"+channelID).Delete(host.ClientStateKey())
}

// registerTestPeer registers an ActivityPub peer with inbound blog_post allowed.
func registerTestPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:   f.authority,
		PeerId:      peerID,
		DisplayName: "Test Peer " + peerID,
		Type:        types.PeerType_PEER_TYPE_ACTIVITYPUB,
	})
	require.NoError(t, err)
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority,
		PeerId:    peerID,
		Policy: types.PeerPolicy{
			InboundContentTypes:  []string{"blog_post", "forum_thread"},
			OutboundContentTypes: []string{"blog_post"},
			// any author: these fixtures are not about curation
			AllowedIdentities: []string{types.AllIdentities},
		},
	})
	require.NoError(t, err)
	activateTestPeer(t, f, ms, peerID)
}

// registerTestNOSTRPeer registers a NOSTR relay peer with inbound blog_post allowed.
func registerTestNOSTRPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:   f.authority,
		PeerId:      peerID,
		DisplayName: "NOSTR Relay " + peerID,
		Type:        types.PeerType_PEER_TYPE_NOSTR,
	})
	require.NoError(t, err)
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority,
		PeerId:    peerID,
		Policy: types.PeerPolicy{
			InboundContentTypes:  []string{"blog_post", "forum_thread"},
			OutboundContentTypes: []string{"blog_post"},
			// any author: these fixtures are not about curation
			AllowedIdentities: []string{types.AllIdentities},
		},
	})
	require.NoError(t, err)
	activateTestPeer(t, f, ms, peerID)
}

// registerTestLENSPeer registers a Lens Chain peer with inbound blog_post allowed.
func registerTestLENSPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:   f.authority,
		PeerId:      peerID,
		DisplayName: "Lens deployment " + peerID,
		Type:        types.PeerType_PEER_TYPE_LENS,
	})
	require.NoError(t, err)
	_, err = ms.UpdatePeerPolicy(f.ctx, &types.MsgUpdatePeerPolicy{
		Authority: f.authority,
		PeerId:    peerID,
		Policy: types.PeerPolicy{
			InboundContentTypes:  []string{"blog_post", "forum_thread"},
			OutboundContentTypes: []string{"blog_post"},
			// any author: these fixtures are not about curation
			AllowedIdentities: []string{types.AllIdentities},
		},
	})
	require.NoError(t, err)
	activateTestPeer(t, f, ms, peerID)
}

// registerTestIBCPeer registers a Spark Dream IBC peer.
func registerTestIBCPeer(t *testing.T, f *fixture, ms types.MsgServer, peerID string) {
	t.Helper()
	_, err := ms.RegisterPeer(f.ctx, &types.MsgRegisterPeer{
		Authority:       f.authority,
		PeerId:          peerID,
		DisplayName:     "IBC Peer " + peerID,
		Type:            types.PeerType_PEER_TYPE_SPARK_DREAM,
		IbcChannelId:    "channel-0",
		ExpectedChainId: testPeerChainID,
	})
	require.NoError(t, err)
	activateTestPeer(t, f, ms, peerID)
}

// testHash returns a deterministic 32-byte content hash.
func testHash(seed byte) []byte {
	h := make([]byte, 32)
	for i := range h {
		h[i] = seed + byte(i)
	}
	return h
}

// testAddr returns a deterministic test address string.
func testAddr(t *testing.T, f *fixture, seed string) string {
	t.Helper()
	// Pad seed to exactly 20 bytes for AccAddress
	padded := seed
	for len(padded) < 20 {
		padded += "_"
	}
	addr := sdk.AccAddress([]byte(padded[:20]))
	str, err := f.addressCodec.BytesToString(addr)
	require.NoError(t, err)
	return str
}

// registerTestBridge registers a bridge operator for a peer and returns the operator address string.
func registerTestBridge(t *testing.T, f *fixture, ms types.MsgServer, peerID, operatorSeed string) string {
	t.Helper()
	operatorStr := testAddr(t, f, operatorSeed)
	_, err := ms.RegisterBridge(f.ctx, &types.MsgRegisterBridge{Operator: operatorStr,
		PeerId:   peerID,
		Protocol: "activitypub",
		Endpoint: "https://" + operatorSeed + ".example.com",
	})
	require.NoError(t, err)
	return operatorStr
}

// submitTestContent submits content and returns the content ID.
func submitTestContent(t *testing.T, f *fixture, ms types.MsgServer, operatorStr, peerID string, hash []byte) uint64 {
	t.Helper()
	resp, err := ms.SubmitFederatedContent(f.ctx, &types.MsgSubmitFederatedContent{
		License:         "CC0-1.0",
		Operator:        operatorStr,
		PeerId:          peerID,
		RemoteContentId: "post-" + string(hash[:4]),
		ContentType:     "blog_post",
		CreatorIdentity: "@test@" + peerID,
		Title:           "Test",
		Body:            "Content",
		ContentHash:     hash,
	})
	require.NoError(t, err)
	return resp.ContentId
}

// bondTestVerifier seeds a BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr)
// in the mock rep keeper, standing in for what x/rep's MsgBondRole would do.
// Returns the bonded address string.
func bondTestVerifier(t *testing.T, f *fixture, _ types.MsgServer, seed string) string {
	t.Helper()
	addr := testAddr(t, f, seed)
	f.repKeeper.SeedBondedRole(
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
		addr,
		reptypes.BondedRole{
			Address:            addr,
			RoleType:           reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			CurrentBond:        "500000000", // 500 DREAM in udream — covers MinVerifierBond
			TotalCommittedBond: "0",
		},
	)
	return addr
}
