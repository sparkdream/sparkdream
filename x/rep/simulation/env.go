package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonstypes "sparkdream/x/commons/types"
	forumtypes "sparkdream/x/forum/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// The x/rep simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions, dry-runs the message through its real handler on
// a throwaway cache of the block state, and returns a NoOp when nothing
// valid exists. No op writes a store: a prerequisite (a project to put an
// initiative under, an invitation to accept, a challenge for a jury to
// decide) is built by delivering the real message first (prereqs.go).
//
// Messages only an x/commons group policy may sign (project completion,
// interim approval, member-report resolution, tag budgets...) reach the
// handler through an executed x/commons proposal (council.go). Jury
// verdicts come from the seated sim jurors casting real MsgSubmitJurorVote
// txs (jury.go).

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

	commonsQuery commonstypes.QueryClient
	forumQuery   forumtypes.QueryClient
	groups       []commonstypes.Group
	seatCache    map[string][]string
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
		// stream, so how much randomness an op uses (which depends on rep
		// state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k, params: params}
		helper := &baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx}
		e.commonsQuery = commonstypes.NewQueryClient(helper)
		e.forumQuery = forumtypes.NewQueryClient(helper)
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
func (e *simEnv) dryRun(msg sdk.Msg) error {
	_, err := e.dryRunCtx(msg)
	return err
}

// dryRunCtx is dryRun returning the throwaway context, so a caller can read
// what the message would leave behind.
func (e *simEnv) dryRunCtx(msg sdk.Msg) (cacheCtx sdk.Context, err error) {
	handler := e.app.MsgServiceRouter().Handler(msg)
	if handler == nil {
		return e.ctx, fmt.Errorf("no handler for %s", sdk.MsgTypeURL(msg))
	}
	cacheCtx, _ = e.ctx.CacheContext()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("handler panicked: %v", p)
		}
	}()
	_, err = handler(cacheCtx, msg)
	return cacheCtx, err
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
//
// A seat holder of an x/commons group pays a small fee instead: the few seats
// sign every committee action, proposal and vote, and random fees (any share
// of the balance) would leave the committees unable to pay a proposal fee
// within a few dozen blocks.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if e.seated(acc.Address.String()) {
		left := e.spendable(acc)
		if spent.IsPositive() {
			left = left.Sub(spent)
		}
		fee := sdk.NewCoins()
		if capAmt := math.MinInt(left, math.NewInt(smallFeeCap)); capAmt.IsPositive() {
			if amt, err := simtypes.RandPositiveInt(e.r, capAmt); err == nil {
				fee = sdk.NewCoins(sdk.NewCoin(e.denom(), amt))
			}
		}
		opMsg, fops, err := simulation.GenAndDeliverTx(e.input(acc, msg, spent), fee)
		e.invalidate()
		return opMsg, fops, err
	}
	opMsg, fops, err := simulation.GenAndDeliverTxWithRandFees(e.input(acc, msg, spent))
	e.invalidate()
	return opMsg, fops, err
}

// smallFeeCap bounds the fee of a vote, juror vote or execution (0.01
// SPARK): the same few seats sign every vote, and a random fee (any share
// of the balance) would drain them within a few proposals.
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
	opMsg, _, err := simulation.GenAndDeliverTx(e.input(acc, msg, math.ZeroInt()), fee)
	e.invalidate()
	return opMsg, err
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

// ensure delivers a prerequisite message from acc (dry-run first). It
// returns false when the handler would refuse it.
func (e *simEnv) ensure(acc simtypes.Account, msg sdk.Msg, spent math.Int) (bool, error) {
	if e.usable(acc, spent) != "" || e.dryRun(msg) != nil {
		return false, nil
	}
	opMsg, _, err := e.deliver(acc, msg, spent)
	if err != nil {
		return false, err
	}
	return opMsg.OK, nil
}

// candidate is one (signer, message) pair an op may deliver.
type candidate struct {
	acc   simtypes.Account
	msg   sdk.Msg
	spent math.Int
}

// firstValid delivers the first candidate whose signer is usable and whose
// message the handler accepts, in random order, trying at most limit of
// them. reason labels the NoOp when none qualifies.
func (e *simEnv) firstValid(label sdk.Msg, cands []candidate, limit int, reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if len(cands) == 0 {
		return noOp(label, reason)
	}
	last := ""
	tried := 0
	for _, i := range e.r.Perm(len(cands)) {
		c := cands[i]
		spent := c.spent
		if spent.IsNil() {
			spent = math.ZeroInt()
		}
		if e.usable(c.acc, spent) != "" {
			continue
		}
		if tried >= limit {
			break
		}
		tried++
		if err := e.dryRun(c.msg); err != nil {
			last = err.Error()
			continue
		}
		return e.deliver(c.acc, c.msg, spent)
	}
	if last != "" {
		return noOp(label, reason+"; handler would reject: "+last)
	}
	return noOp(label, reason+"; no usable signer")
}

// invalidate drops the cached state read so far: a delivered tx moved it.
func (e *simEnv) invalidate() {
	e.groups = nil
	e.seatCache = nil
}

// denom is the chain's bond denom (fees, deposits, bonds).
func (e *simEnv) denom() string {
	return e.k.BondDenom(e.ctx)
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.denom())
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

// live returns the sim accounts accepted by keep that are not tombstoned
// and can pay a fee.
func (e *simEnv) live(keep func(simtypes.Account) bool) []simtypes.Account {
	var out []simtypes.Account
	for _, acc := range e.accs {
		if e.tombstoned(acc.Address.String()) || !e.spendable(acc).IsPositive() {
			continue
		}
		if keep == nil || keep(acc) {
			out = append(out, acc)
		}
	}
	return out
}

// tombstoned reports whether addr migrated away (the ante handler refuses
// most messages from it).
func (e *simEnv) tombstoned(addr string) bool {
	return e.k.IsTombstoned(e.ctx, addr)
}

// --- Random content ---

// randomReason returns a short free-text reason.
func randomReason(r *rand.Rand) string {
	return "sim: " + simtypes.RandStringOfLength(r, 4+r.Intn(24))
}

// randomURI returns a deliverable or evidence URI.
func randomURI(r *rand.Rand) string {
	return "ipfs://sim-" + simtypes.RandStringOfLength(r, 16)
}

// dream returns n whole DREAM in base units.
func dream(n int64) math.Int {
	return math.NewInt(n).MulRaw(1_000_000)
}

// randBetween returns a random amount in [lo, hi] (lo when hi <= lo).
func randBetween(r *rand.Rand, lo, hi math.Int) math.Int {
	if hi.LTE(lo) {
		return lo
	}
	span := hi.Sub(lo)
	amt, err := simtypes.RandPositiveInt(r, span)
	if err != nil {
		return lo
	}
	return lo.Add(amt)
}
