package simulation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commontypes "sparkdream/x/common/types"
	commonstypes "sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
	"sparkdream/x/storage/keeper"
	"sparkdream/x/storage/types"
)

// The x/storage simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, dry-runs the
// message through its real handler on a throwaway cache of the block state,
// and returns a NoOp when it cannot hold. No op writes a store.
//
// Storage operators and scanners are x/service operators, registered by the
// x/service simulation (it registers sim accounts under every enabled
// service type, storage-arweave and content-scanner included). They count
// here only once their controller approves them, which the approve ops do
// through an executed x/commons proposal (council.go).

// councilBodies are the x/commons bodies that hold the x/storage council
// messages and so can be the controller of an operator or scanner here.
var councilBodies = []string{"Commons Operations Committee", "Commons Council"}

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
	query        types.QueryServer
	serviceQuery servicetypes.QueryClient
	tombstones   map[string]bool
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
		// storage state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k, params: params}
		helper := &baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx}
		e.repQuery = reptypes.NewQueryClient(helper)
		e.commonsQuery = commonstypes.NewQueryClient(helper)
		e.query = keeper.NewQueryServerImpl(k)
		e.serviceQuery = servicetypes.NewQueryClient(helper)
		return body(e)
	}
}

// skipped is an operation that is never simulated; reason says why.
func skipped(msg sdk.Msg, reason string) simtypes.Operation {
	return func(*rand.Rand, *baseapp.BaseApp, sdk.Context, []simtypes.Account, string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		return noOp(msg, reason)
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

// smallFeeCap bounds the fee of a worker's tx, a vote or an execution (0.01
// SPARK): the same few accounts sign them over and over, and a random fee
// (any share of the balance) would drain them within a few messages.
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
	if e.spendable(acc).LTE(spent) {
		return "signer cannot pay " + spent.String() + e.denom() + " and a fee"
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

// submitSmall is submit for a worker message that takes no SPARK.
func (e *simEnv) submitSmall(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if why := e.usable(acc, math.ZeroInt()); why != "" {
		return noOp(msg, why)
	}
	if err := e.dryRun(msg); err != nil {
		return noOp(msg, "handler would reject: "+err.Error())
	}
	opMsg, err := e.deliverSmallFee(acc, msg)
	return opMsg, nil, err
}

// invalidate drops the state read so far: a delivered tx moved it.
func (e *simEnv) invalidate() {}

// denom is the chain's bond denom (fees and escrow).
func (e *simEnv) denom() string {
	return e.k.BondDenom(e.ctx)
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.denom())
}

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

// eligible reports whether addr is an active member at the requester trust
// level (spec section 3.3).
func (e *simEnv) eligible(addr string) bool {
	res, err := e.repQuery.GetMember(e.ctx, &reptypes.QueryGetMemberRequest{Address: addr})
	if err != nil {
		return false
	}
	m := res.Member
	return m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE && commontypes.Saturate[uint32](int32(m.TrustLevel)) >= e.params.MinRequesterTrustLevel
}

// activeMember reports whether addr is an active x/rep member.
func (e *simEnv) activeMember(addr string) bool {
	res, err := e.repQuery.GetMember(e.ctx, &reptypes.QueryGetMemberRequest{Address: addr})
	return err == nil && res.Member.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE
}

// requests returns up to limit stored requests in one of the given statuses,
// newest first (the newest are the ones still moving).
func (e *simEnv) requests(limit int, statuses ...types.RequestStatus) ([]types.Request, error) {
	want := map[types.RequestStatus]bool{}
	for _, s := range statuses {
		want[s] = true
	}
	var out []types.Request
	rng := new(collections.Range[uint64]).Descending()
	scanned := 0
	err := e.k.Requests.Walk(e.ctx, rng, func(_ uint64, r types.Request) (bool, error) {
		scanned++
		if want[r.Status] {
			out = append(out, r)
		}
		return len(out) >= limit || scanned >= 500, nil
	})
	return out, err
}

// backends returns the enabled backends.
func (e *simEnv) backends() ([]types.Backend, error) {
	var out []types.Backend
	err := e.k.Backends.Walk(e.ctx, nil, func(_ string, b types.Backend) (bool, error) {
		if b.Enabled {
			out = append(out, b)
		}
		return false, nil
	})
	return out, err
}

// operators returns the sim accounts that could claim on a backend now.
func (e *simEnv) operators(backend string) []simtypes.Account {
	var out []simtypes.Account
	res, err := e.query.ApprovedOperators(e.ctx, &types.QueryApprovedOperatorsRequest{Backend: backend})
	if err != nil {
		return nil
	}
	for _, o := range res.Operators {
		if o.Stale {
			continue
		}
		if acc, ok := e.account(o.Operator); ok {
			out = append(out, acc)
		}
	}
	return out
}

// scanners returns the approved scanners that are sim accounts.
func (e *simEnv) scanners() []simtypes.Account {
	var out []simtypes.Account
	res, err := e.query.ApprovedScanners(e.ctx, &types.QueryApprovedScannersRequest{})
	if err != nil {
		return nil
	}
	for _, s := range res.Scanners {
		if s.Stale {
			continue
		}
		if acc, ok := e.account(s.Scanner); ok {
			out = append(out, acc)
		}
	}
	return out
}

// randomHash returns a random sha256 in hex.
func randomHash(r *rand.Rand) string {
	var b [32]byte
	r.Read(b[:])
	h := sha256.Sum256(b[:])
	return hex.EncodeToString(h[:])
}

const base64url = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

// randomLocator returns a well-formed Arweave id.
func randomLocator(r *rand.Rand) string {
	b := make([]byte, 43)
	for i := range b {
		b[i] = base64url[r.Intn(len(base64url))]
	}
	return string(b)
}

// decodeAs unpacks a proposal message of the op's type.
func decodeAs[T any, PT interface {
	*T
	sdk.Msg
	Unmarshal([]byte) error
}](a *codectypes.Any) (sdk.Msg, bool) {
	m := PT(new(T))
	if a == nil || m.Unmarshal(a.Value) != nil {
		return nil, false
	}
	return m, true
}
