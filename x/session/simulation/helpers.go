package simulation

import (
	"fmt"
	"math/rand"
	"sort"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// The x/session simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions, dry-runs the message through its real handler on
// a throwaway cache of the block state, and returns a NoOp when nothing
// valid exists. No op writes a store: a prerequisite (a session key to
// execute through, a recurring pull to claim, an allowance to pull from, a
// grant to revoke or decline) is built by delivering the real
// MsgCreateGrant / MsgCreateSession first.

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

	repQuery   reptypes.QueryClient
	tombstones map[string]bool
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
		// session state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k, params: params}
		helper := &baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx}
		e.repQuery = reptypes.NewQueryClient(helper)
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
// state and returns the handler's error. When fee is positive it is first
// moved from payer to the fee collector on the cache, the way the ante
// handler takes it before the message runs (MsgExecSession's fee comes out
// of the granter, whose inner message may then need the same funds). A
// panicking handler is reported as an error (and never delivered).
func (e *simEnv) dryRun(msg sdk.Msg) error {
	return e.dryRunAfterFee(msg, nil, nil)
}

func (e *simEnv) dryRunAfterFee(msg sdk.Msg, payer sdk.AccAddress, fee sdk.Coins) (err error) {
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
	if payer != nil && fee.IsAllPositive() {
		if err := e.bk.SendCoinsFromAccountToModule(cacheCtx, payer, authtypes.FeeCollectorName, fee); err != nil {
			return fmt.Errorf("fee payer cannot pay %s: %w", fee, err)
		}
	}
	_, err = handler(cacheCtx, msg)
	return err
}

// input builds the delivery input for msg signed by acc; spent is the SPARK
// the handler itself takes from acc, which the tx fee must leave in place.
func (e *simEnv) input(acc simtypes.Account, msg sdk.Msg, spent math.Int) simulation.OperationInput {
	coins := sdk.NewCoins()
	if spent.IsPositive() {
		coins = sdk.NewCoins(sdk.NewCoin(e.denom(), spent))
	}
	return simulation.OperationInput{
		R:               e.r,
		App:             e.app,
		TxGen:           e.txGen,
		Cdc:             nil,
		Msg:             msg,
		CoinsSpentInMsg: coins,
		Context:         e.ctx,
		SimAccount:      acc,
		AccountKeeper:   e.ak,
		Bankkeeper:      e.bk,
		ModuleName:      types.ModuleName,
	}
}

// deliver signs msg with acc and delivers it with a random fee drawn from
// what is left after spent.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	opMsg, fops, err := simulation.GenAndDeliverTxWithRandFees(e.input(acc, msg, spent))
	e.invalidate()
	return opMsg, fops, err
}

// usable reports why acc cannot sign a tx that takes spent SPARK ("" when
// it can): a tombstoned (migrated-away) signer is refused by the ante
// handler, and the fee must fit next to spent.
func (e *simEnv) usable(acc simtypes.Account, spent math.Int) string {
	if e.tombstoned(acc.Address.String()) {
		return "signer is a tombstoned address"
	}
	if spent.IsPositive() && e.spendable(acc).LTE(spent) {
		return "signer cannot pay " + spent.String() + e.denom()
	}
	return ""
}

// submit dry-runs msg and delivers it when the handler accepts it.
func (e *simEnv) submit(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if why := e.usable(acc, spent); why != "" {
		return noOp(msg, why)
	}
	if err := e.dryRun(msg); err != nil {
		return noOp(msg, "handler would reject: "+err.Error())
	}
	return e.deliver(acc, msg, spent)
}

// ensure delivers a prerequisite message from acc (dry-run first). ok is
// false when the handler would refuse it; opMsg is the delivery result.
func (e *simEnv) ensure(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, bool, error) {
	if e.usable(acc, spent) != "" || e.dryRun(msg) != nil {
		return simtypes.OperationMsg{}, false, nil
	}
	opMsg, _, err := e.deliver(acc, msg, spent)
	if err != nil {
		return opMsg, false, err
	}
	return opMsg, opMsg.OK, nil
}

// invalidate drops the state read so far: a delivered tx moved it.
func (e *simEnv) invalidate() {
	e.tombstones = nil
}

// denom is the chain's bond denom (deposits, transfers, tx fees).
func (e *simEnv) denom() string {
	return e.k.BondDenom(e.ctx)
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.denom())
}

// spendableOf returns addr's spendable SPARK.
func (e *simEnv) spendableOf(addr string) math.Int {
	bz, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return math.ZeroInt()
	}
	return e.bk.SpendableCoins(e.ctx, bz).AmountOf(e.denom())
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

// --- Grant views (keeper reads) ---

// grants returns every grant accepted by keep, in id order.
func (e *simEnv) grants(keep func(types.Grant) bool) ([]types.Grant, error) {
	var out []types.Grant
	err := e.k.Grants.Walk(e.ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
		if keep == nil || keep(g) {
			out = append(out, g)
		}
		return false, nil
	})
	return out, err
}

// pending reports whether a grant can still be acted on (not terminal).
func pending(g types.Grant) bool {
	return g.Status == types.GrantStatus_GRANT_STATUS_ACTIVE ||
		g.Status == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
}

// newestGrant returns the newest grant of granter with grantee and type
// (the one just created by a delivered MsgCreateGrant).
func (e *simEnv) newestGrant(granter, grantee string, t types.GrantType) (types.Grant, bool, error) {
	list, err := e.k.ListGrantsByGranter(e.ctx, granter, t)
	if err != nil {
		return types.Grant{}, false, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Id > list[j].Id })
	for _, g := range list {
		if g.Grantee == grantee && pending(g) {
			return g, true, nil
		}
	}
	return types.Grant{}, false, nil
}

// pair returns two distinct live sim accounts: a granter that can pay
// minSpend plus a fee, and a grantee.
func (e *simEnv) pair(minSpend math.Int) (simtypes.Account, simtypes.Account, bool) {
	granters := e.live(func(a simtypes.Account) bool { return e.spendable(a).GT(minSpend) })
	granter, ok := pick(e.r, granters)
	if !ok {
		return simtypes.Account{}, simtypes.Account{}, false
	}
	grantee, ok := pick(e.r, e.live(func(a simtypes.Account) bool { return !a.Address.Equals(granter.Address) }))
	if !ok {
		return simtypes.Account{}, simtypes.Account{}, false
	}
	return granter, grantee, true
}
