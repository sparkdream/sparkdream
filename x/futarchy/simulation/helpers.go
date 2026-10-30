package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
	reptypes "sparkdream/x/rep/types"
)

// The x/futarchy simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions, dry-runs the message through its real handler on
// a throwaway cache of the block state, and returns a NoOp when nothing
// valid exists. No op writes a store: a market to trade on is created by an
// ESTABLISHED+ member's real MsgCreateMarket first, and markets resolve in
// the EndBlocker on their own (firing AfterMarketResolved into x/commons,
// which extends or shortens a council's term for a confidence-vote market).
//
// MsgUpdateOperationalParams is accepted only from the Commons Operations
// Committee's (or its council's) policy address, which only an executed
// x/commons proposal can sign for (council.go). MsgCancelMarket and
// MsgUpdateParams are signed by the x/gov authority alone and have no op.

// simEnv carries the arguments every operation needs.
type simEnv struct {
	r      *rand.Rand
	app    *baseapp.BaseApp
	ctx    sdk.Context
	accs   []simtypes.Account
	txGen  client.TxConfig
	ak     types.AuthKeeper
	bk     types.BankKeeper
	k      keeper.Keeper
	params types.Params

	repQuery     reptypes.QueryClient
	commonsQuery commonstypes.QueryClient
	tombstones   map[string]bool
	members      map[string]*reptypes.Member
	groups       []commonstypes.Group
}

// operation builds a simulation operation: it loads params and hands the
// body a simEnv. msg labels the NoOp when there is no app to run against.
func operation(
	ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig, msg sdk.Msg,
	body func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error),
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if app == nil {
			return noOp(msg, "no app to deliver to")
		}
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		// Each operation seeds its own source from two draws of the shared
		// stream, so how much randomness an op uses (which depends on
		// futarchy state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k, params: params}
		helper := &baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx}
		e.repQuery = reptypes.NewQueryClient(helper)
		e.commonsQuery = commonstypes.NewQueryClient(helper)
		return body(e)
	}
}

// noOp returns a skipped operation for msg's type.
func noOp(msg sdk.Msg, reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), reason), nil, nil
}

// noOpErr returns a skipped operation together with a store error, which
// fails the simulation: a failing store read is a real bug.
func noOpErr(msg sdk.Msg, err error) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), err.Error()), nil, err
}

// dryRun runs msg through its real handler on a throwaway cache of the block
// state and returns the handler's error. A panicking handler is reported as
// an error (and never delivered).
func (e *simEnv) dryRun(msg sdk.Msg) (err error) {
	handler := e.app.MsgServiceRouter().Handler(msg)
	if handler == nil {
		return fmt.Errorf("no handler for %s", sdk.MsgTypeURL(msg))
	}
	cacheCtx, _ := e.ctx.CacheContext()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("handler panicked: %v", p)
		}
	}()
	_, err = handler(cacheCtx, msg)
	return err
}

// input builds the delivery input for msg signed by acc; spent is what the
// handler itself takes from acc, which the random tx fee must leave in place.
func (e *simEnv) input(acc simtypes.Account, msg sdk.Msg, spent sdk.Coins) simulation.OperationInput {
	if spent == nil {
		spent = sdk.NewCoins()
	}
	return simulation.OperationInput{
		R:               e.r,
		App:             e.app,
		TxGen:           e.txGen,
		Cdc:             nil,
		Msg:             msg,
		CoinsSpentInMsg: spent,
		Context:         e.ctx,
		SimAccount:      acc,
		AccountKeeper:   e.ak,
		Bankkeeper:      e.bk,
		ModuleName:      types.ModuleName,
	}
}

// deliver signs msg with acc and delivers it with a random fee drawn from
// what is left after spent.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg, spent sdk.Coins) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	opMsg, fops, err := simulation.GenAndDeliverTxWithRandFees(e.input(acc, msg, spent))
	e.invalidate()
	return opMsg, fops, err
}

// smallFeeCap bounds the fee of a vote or execution (0.01 SPARK): the same
// few seats sign every vote, and a random fee (any share of the balance)
// would drain them within a few proposals.
const smallFeeCap = 10_000

// deliverSmallFee delivers msg from acc with a small random fee.
func (e *simEnv) deliverSmallFee(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, error) {
	capAmt := math.MinInt(e.spendable(acc), math.NewInt(smallFeeCap))
	fee := sdk.NewCoins()
	if capAmt.IsPositive() {
		if amt, err := simtypes.RandPositiveInt(e.r, capAmt); err == nil {
			fee = sdk.NewCoins(sdk.NewCoin(e.denom(), amt))
		}
	}
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(acc, msg, nil), fee)
	e.invalidate()
	return opMsg, err
}

// usable reports why acc cannot sign a tx that takes spent ("" when it
// can): a tombstoned (migrated-away) signer is refused by the ante handler,
// and the fee must fit next to spent.
func (e *simEnv) usable(acc simtypes.Account, spent sdk.Coins) string {
	if e.tombstoned(acc.Address.String()) {
		return "signer is a tombstoned address"
	}
	if !spent.IsZero() {
		have := e.bk.SpendableCoins(e.ctx, acc.Address)
		if !have.IsAllGTE(spent) || !have.AmountOf(e.denom()).GT(spent.AmountOf(e.denom())) {
			return "signer cannot pay " + spent.String()
		}
	}
	return ""
}

// invalidate drops the state read so far: a delivered tx moved it.
func (e *simEnv) invalidate() {
	e.tombstones = nil
	e.members = nil
	e.groups = nil
}

// denom is the chain's bond denom (liquidity, trades, tx fees).
func (e *simEnv) denom() string {
	return e.k.BondDenom(e.ctx)
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.denom())
}

// sparkCoins returns amt SPARK as coins.
func (e *simEnv) sparkCoins(amt math.Int) sdk.Coins {
	return sdk.NewCoins(sdk.NewCoin(e.denom(), amt))
}

// --- Selection helpers ---

// pick returns a random element of items.
func pick[T any](r *rand.Rand, items []T) (T, bool) {
	var zero T
	if len(items) == 0 {
		return zero, false
	}
	return items[r.Intn(len(items))], true
}

// getAccount returns the sim account for addr.
func getAccount(accs []simtypes.Account, addr string) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// account returns the live (not tombstoned) sim account for addr.
func (e *simEnv) account(addr string) (simtypes.Account, bool) {
	acc, ok := getAccount(e.accs, addr)
	if !ok || e.tombstoned(addr) {
		return simtypes.Account{}, false
	}
	return acc, true
}

// live returns the sim accounts accepted by keep that are not tombstoned.
func (e *simEnv) live(keep func(simtypes.Account) bool) []simtypes.Account {
	var out []simtypes.Account
	for _, acc := range e.accs {
		if !e.tombstoned(acc.Address.String()) && (keep == nil || keep(acc)) {
			out = append(out, acc)
		}
	}
	return out
}

// tombstoned reports whether addr migrated away (x/rep refuses most
// messages from it at the ante handler).
func (e *simEnv) tombstoned(addr string) bool {
	if t, ok := e.tombstones[addr]; ok {
		return t
	}
	res, err := e.repQuery.Canonical(e.ctx, &reptypes.QueryCanonicalRequest{Address: addr})
	t := err == nil && res.Tombstoned
	if e.tombstones == nil {
		e.tombstones = map[string]bool{}
	}
	e.tombstones[addr] = t
	return t
}

// --- x/rep views ---

// member returns addr's x/rep member record (nil when it has none).
func (e *simEnv) member(addr string) *reptypes.Member {
	if e.members == nil {
		e.members = map[string]*reptypes.Member{}
	}
	if m, ok := e.members[addr]; ok {
		return m
	}
	var m *reptypes.Member
	if res, err := e.repQuery.GetMember(e.ctx, &reptypes.QueryGetMemberRequest{Address: addr}); err == nil {
		m = &res.Member
	}
	e.members[addr] = m
	return m
}

// activeMember reports whether addr is an active x/rep member of at least
// trust level minLevel.
func (e *simEnv) activeMember(addr string, minLevel reptypes.TrustLevel) bool {
	m := e.member(addr)
	return m != nil && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE && m.TrustLevel >= minLevel
}

// --- x/commons views ---

// allGroups returns every x/commons group with a policy address that is not
// a sim account (no sim account is ever a policy; a group whose policy were
// one could be signed for without a vote).
func (e *simEnv) allGroups() []commonstypes.Group {
	if e.groups != nil {
		return e.groups
	}
	out := []commonstypes.Group{}
	var next []byte
	for {
		res, err := e.commonsQuery.ListGroups(e.ctx, &commonstypes.QueryAllGroupRequest{
			Pagination: &query.PageRequest{Key: next, Limit: 200},
		})
		if err != nil {
			break
		}
		for _, g := range res.Group {
			if g.PolicyAddress == "" {
				continue
			}
			if _, isAcc := getAccount(e.accs, g.PolicyAddress); isAcc {
				continue
			}
			out = append(out, g)
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			break
		}
		next = res.Pagination.NextKey
	}
	e.groups = out
	return out
}

// groupsAllowing returns the names of the groups whose policy allowlist
// carries typeURL (the messages a proposal of the group may execute).
func (e *simEnv) groupsAllowing(typeURL string) []string {
	var out []string
	for _, g := range e.allGroups() {
		perms, err := e.commonsQuery.GetPolicyPermissions(e.ctx, &commonstypes.QueryGetPolicyPermissionsRequest{PolicyAddress: g.PolicyAddress})
		if err != nil {
			continue
		}
		for _, m := range perms.PolicyPermissions.AllowedMessages {
			if m == typeURL {
				out = append(out, g.Index)
				break
			}
		}
	}
	return out
}

// --- x/futarchy views (keeper reads only) ---

// markets returns the markets accepted by keep, in id order.
func (e *simEnv) markets(keep func(types.Market) bool) ([]types.Market, error) {
	var out []types.Market
	err := e.k.Market.Walk(e.ctx, nil, func(_ uint64, m types.Market) (bool, error) {
		if keep == nil || keep(m) {
			out = append(out, m)
		}
		return false, nil
	})
	return out, err
}

// shareDenom is the conditional-token denom of a market outcome.
func shareDenom(marketID uint64, outcome string) string {
	return fmt.Sprintf("f/%d/%s", marketID, outcome)
}
