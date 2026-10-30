package simulation

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
)

// The commons simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions, dry-runs the message through its real handler on
// a throwaway cache of the block state, and returns a NoOp when it cannot
// hold. No op writes a commons collection directly.
//
// Every policy-gated message (group registration, renewal, membership,
// config and deletion, vetoes, spends, permission changes) is signed by a
// group policy address, which only an executed x/commons proposal can sign
// for. Sim accounts are never made a policy: those messages travel the way
// the spec routes them (council.go). A member of the authorized body submits
// an x/commons proposal carrying the message, members vote it through, and
// anyone executes it once accepted. The bodies are the real "Three Pillars"
// groups the simulated genesis bootstraps around sim accounts
// (module/simulation_genesis.go), plus the groups the simulation itself
// registers through the Commons Council (prefix simGroupPrefix).

// simEnv carries the arguments every operation needs.
type simEnv struct {
	r     *rand.Rand
	app   *baseapp.BaseApp
	ctx   sdk.Context
	accs  []simtypes.Account
	txGen client.TxConfig
	ak    types.AuthKeeper
	bk    types.BankKeeper
	k     keeper.Keeper

	repQuery   reptypes.QueryClient
	tombstones map[string]bool
}

// operation builds a simulation operation that hands the body a simEnv.
func operation(
	ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig,
	body func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error),
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Each operation seeds its own source from two draws of the shared
		// stream, so how much randomness an op uses (which depends on commons
		// state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k}
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

// input builds the delivery input for msg signed by acc. No x/commons
// message a sim account signs takes SPARK from it (a proposal's fee is a tx
// fee, and a registration fee is paid by the council policy), so nothing is
// reserved.
func (e *simEnv) input(acc simtypes.Account, msg sdk.Msg) simulation.OperationInput {
	return simulation.OperationInput{
		R:               e.r,
		App:             e.app,
		TxGen:           e.txGen,
		Cdc:             nil,
		Msg:             msg,
		CoinsSpentInMsg: sdk.NewCoins(),
		Context:         e.ctx,
		SimAccount:      acc,
		AccountKeeper:   e.ak,
		Bankkeeper:      e.bk,
		ModuleName:      types.ModuleName,
	}
}

// smallFeeCap bounds the fee of a delivery signed by a group seat.
const smallFeeCap = 10_000

// deliver signs msg with acc and delivers it with a small random fee (at
// most 0.01 SPARK). Every commons signer is a seat on some body (proposers,
// voters, poll creators, Operations Committee members), and the same few
// seats sign every proposal: the fee GenAndDeliverTxWithRandFees draws (any
// share of the balance) would drain them within a few proposals, leaving no
// seat able to pay the next proposal fee.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	denom := sdk.DefaultBondDenom
	spendable := e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(denom)
	capAmt := math.MinInt(spendable, math.NewInt(smallFeeCap))
	fee := sdk.NewCoins()
	if capAmt.IsPositive() {
		amt, err := simtypes.RandPositiveInt(e.r, capAmt)
		if err == nil {
			fee = sdk.NewCoins(sdk.NewCoin(denom, amt))
		}
	}
	return simulation.GenAndDeliverTx(e.input(acc, msg), fee)
}

// submit dry-runs msg and delivers it when the handler accepts it. The
// signer must not be a tombstoned (migrated-away) address, which the ante
// handler refuses.
func (e *simEnv) submit(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if e.tombstoned(acc.Address.String()) {
		return noOp(msg, "signer is a tombstoned address")
	}
	if err := e.dryRun(msg); err != nil {
		return noOp(msg, "handler would reject: "+err.Error())
	}
	return e.deliver(acc, msg)
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

// accountsWhere returns the sim accounts accepted by keep.
func accountsWhere(accs []simtypes.Account, keep func(simtypes.Account) bool) []simtypes.Account {
	var out []simtypes.Account
	for _, acc := range accs {
		if keep(acc) {
			out = append(out, acc)
		}
	}
	return out
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

// liveAccounts returns the sim accounts that are not tombstoned.
func (e *simEnv) liveAccounts() []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool { return !e.tombstoned(a.Address.String()) })
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

// canPay reports whether acc can pay fee from its spendable balance.
func (e *simEnv) canPay(acc simtypes.Account, fee sdk.Coins) bool {
	return e.bk.SpendableCoins(e.ctx, acc.Address).IsAllGTE(fee)
}

// --- Commons state views (keeper reads only) ---

// simGroupPrefix names the groups the simulation registers. Only these are
// ever deleted, vetoed, renewed, reconfigured or shrunk: the bootstrapped
// "Three Pillars" bodies are what every other module's simulation routes its
// council-gated messages through.
const simGroupPrefix = "sim-"

// isSimGroup reports whether name is a group the simulation registered.
func isSimGroup(name string) bool {
	return strings.HasPrefix(name, simGroupPrefix)
}

// namedGroup is a group with its name.
type namedGroup struct {
	name string
	g    types.Group
}

// groups returns every group accepted by keep.
func (e *simEnv) groups(keep func(name string, g types.Group) bool) ([]namedGroup, error) {
	var out []namedGroup
	err := e.k.Groups.Walk(e.ctx, nil, func(name string, g types.Group) (bool, error) {
		if keep(name, g) {
			out = append(out, namedGroup{name: name, g: g})
		}
		return false, nil
	})
	return out, err
}

// group returns a group by name.
func (e *simEnv) group(name string) (types.Group, bool, error) {
	g, err := e.k.Groups.Get(e.ctx, name)
	if errors.Is(err, collections.ErrNotFound) {
		return types.Group{}, false, nil
	}
	return g, err == nil, err
}

// termExpired mirrors the keeper's isTermExpired.
func (e *simEnv) termExpired(g types.Group) bool {
	return g.CurrentTermExpiration > 0 && e.ctx.BlockTime().Unix() > g.CurrentTermExpiration
}

// seat is a council member that is a live sim account.
type seat struct {
	acc    simtypes.Account
	weight math.LegacyDec
}

// seats returns a body's members and its total weight; live lists the
// members that are live sim accounts (policy seats such as the golden share
// are members no sim account can vote for).
func (e *simEnv) seats(council string) (members []types.Member, live []seat, total math.LegacyDec, err error) {
	members, err = e.k.GetCouncilMembers(e.ctx, council)
	total = math.LegacyZeroDec()
	if err != nil {
		return nil, nil, total, err
	}
	for _, m := range members {
		w, perr := math.LegacyNewDecFromStr(m.Weight)
		if perr != nil {
			continue // the tally skips unparseable weights too
		}
		total = total.Add(w)
		if acc, ok := e.account(m.Address); ok {
			live = append(live, seat{acc: acc, weight: w})
		}
	}
	return members, live, total, nil
}

// liveSeatAccounts returns the live sim accounts seated on a body.
func (e *simEnv) liveSeatAccounts(council string) ([]simtypes.Account, error) {
	_, live, _, err := e.seats(council)
	out := make([]simtypes.Account, 0, len(live))
	for _, s := range live {
		out = append(out, s.acc)
	}
	return out, err
}

// --- Random data ---

// fictionalNames are the generic names simulated groups, polls and
// categories draw from.
var fictionalNames = []string{"phoenix", "aurora", "zenith", "nebula", "cascade", "ember", "horizon", "prism"}

// randomFictional returns a random fictional name.
func randomFictional(r *rand.Rand) string {
	return fictionalNames[r.Intn(len(fictionalNames))]
}

// randomLowerAlnum returns n random lowercase letters and digits.
func randomLowerAlnum(r *rand.Rand, n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	return string(b)
}

// day is one day in seconds. The SDK simulator advances block time by
// 5000-10000 seconds a block, so a day passes about every 12 blocks.
const day int64 = 24 * 3600
