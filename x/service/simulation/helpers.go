package simulation

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commonstypes "sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// The x/service simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions, dry-runs the message through its real handler on
// a throwaway cache of the block state, and returns a NoOp when it cannot
// hold. No op writes a store: a prerequisite (an operator to top up, a
// report to resolve, a jury verdict to apply) is built by delivering the
// real message first.
//
// Controllers are x/commons group policy addresses, which no sim account
// holds: MsgResolveReport reaches the handler through an executed x/commons
// proposal (council.go). The jury-resolved messages (MsgResolveReportByJury,
// MsgFinalizeControllerTransfer) are signed by a Commons Operations
// Committee member once the x/rep jury seated on the case has voted; the
// seated sim jurors cast their votes with real MsgSubmitJurorVote txs.

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
		// service state) never shifts the draws of the other modules' ops.
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

// invalidate drops the state read so far: a delivered tx moved it.
func (e *simEnv) invalidate() {
	e.members = nil
	e.groups = nil
}

// denom is the chain's bond denom (bonds, deposits, tx fees).
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

// meetsReporterTrust reports whether addr passes the min_reporter_trust_level
// gate MsgReportOperator and a third-party MsgOpenControllerTransferCase
// apply.
func (e *simEnv) meetsReporterTrust(addr string) bool {
	if e.params.MinReporterTrustLevel == "" {
		return true
	}
	minLevel, ok := reptypes.TrustLevel_value[e.params.MinReporterTrustLevel]
	if !ok {
		return false
	}
	m := e.member(addr)
	return m != nil && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE && int32(m.TrustLevel) >= minLevel
}

// juryReview returns an x/rep jury review.
func (e *simEnv) juryReview(id uint64) (reptypes.JuryReview, bool) {
	res, err := e.repQuery.GetJuryReview(e.ctx, &reptypes.QueryGetJuryReviewRequest{Id: id})
	if err != nil {
		return reptypes.JuryReview{}, false
	}
	return res.JuryReview, true
}

// juryVerdict returns the tallied verdict of the jury case behind an
// escalated report or a controller-transfer case. While it is PENDING the
// seated live sim jurors that have not voted yet cast a vote first (real
// MsgSubmitJurorVote txs); vote is the last vote delivered (OK false when
// none was) and reason says why the jury cannot decide now.
func (e *simEnv) juryVerdict(caseID uint64) (verdict reptypes.Verdict, vote simtypes.OperationMsg, reason string, err error) {
	verdict = reptypes.Verdict_VERDICT_PENDING
	review, ok := e.juryReview(caseID)
	if !ok {
		return verdict, vote, "no x/rep jury review for the case", nil
	}
	if review.Verdict != reptypes.Verdict_VERDICT_PENDING {
		return review.Verdict, vote, "", nil
	}
	if len(review.Jurors) == 0 {
		return verdict, vote, "no juror was seated on the case", nil
	}
	if e.ctx.BlockHeight() > review.Deadline {
		return verdict, vote, "the jury's voting deadline has passed", nil
	}
	cast := map[string]bool{}
	for _, v := range review.Votes {
		if v != nil {
			cast[v.Juror] = true
		}
	}
	// One side per call: the jurors voting now agree, so a jury voting in
	// one call reaches a supermajority; split calls end INCONCLUSIVE.
	side := reptypes.Verdict_VERDICT_UPHOLD_CHALLENGE
	if e.r.Intn(2) == 0 {
		side = reptypes.Verdict_VERDICT_REJECT_CHALLENGE
	}
	for _, i := range e.r.Perm(len(review.Jurors)) {
		juror := review.Jurors[i]
		if cast[juror] {
			continue
		}
		acc, ok := e.account(juror)
		if !ok || !e.spendable(acc).IsPositive() {
			continue
		}
		confidence := math.LegacyNewDecWithPrec(int64(50+e.r.Intn(51)), 2)
		msg := &reptypes.MsgSubmitJurorVote{
			Juror:        juror,
			JuryReviewId: caseID,
			Verdict:      side,
			Confidence:   &confidence,
			Reasoning:    randomReason(e.r),
		}
		if e.dryRun(msg) != nil {
			continue
		}
		opMsg, err := e.deliverSmallFee(acc, msg)
		if err != nil {
			return verdict, opMsg, "", err
		}
		if opMsg.OK {
			vote = opMsg
		}
		if review, ok = e.juryReview(caseID); ok && review.Verdict != reptypes.Verdict_VERDICT_PENDING {
			return review.Verdict, vote, "", nil
		}
	}
	if vote.OK {
		return verdict, vote, "the seated sim jurors voted; the jury has not decided yet", nil
	}
	return verdict, vote, "no seated sim juror can vote", nil
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

// groupByPolicy returns the group whose policy address is policy.
func (e *simEnv) groupByPolicy(policy string) (commonstypes.Group, bool) {
	for _, g := range e.allGroups() {
		if g.PolicyAddress == policy {
			return g, true
		}
	}
	return commonstypes.Group{}, false
}

// groupMembers returns the member addresses of a group.
func (e *simEnv) groupMembers(name string) []string {
	res, err := e.commonsQuery.GetCouncilMembers(e.ctx, &commonstypes.QueryGetCouncilMembersRequest{CouncilName: name})
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(res.Members))
	for _, m := range res.Members {
		out = append(out, m.Address)
	}
	return out
}

// isControllerMember reports whether addr sits on the group whose policy is
// policy (MsgReportOperator refuses such a reporter).
func (e *simEnv) isControllerMember(policy, addr string) bool {
	g, ok := e.groupByPolicy(policy)
	if !ok {
		return false
	}
	for _, m := range e.groupMembers(g.Index) {
		if m == addr {
			return true
		}
	}
	return false
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

// opsCommittee is the body whose allowlist carries MsgResolveReport and
// whose members relay jury verdicts (IsCouncilAuthorized commons/operations).
const opsCommittee = "Commons Operations Committee"

// controllerPolicy picks a controller for a new operator: usually the
// Commons Operations Committee (the body that can resolve reports through a
// proposal), sometimes another group with members.
func (e *simEnv) controllerPolicy() (string, bool) {
	var ops string
	var others []string
	for _, g := range e.allGroups() {
		if len(e.groupMembers(g.Index)) == 0 {
			continue
		}
		if g.Index == opsCommittee {
			ops = g.PolicyAddress
		} else {
			others = append(others, g.PolicyAddress)
		}
	}
	if ops != "" && (len(others) == 0 || e.r.Intn(4) != 0) {
		return ops, true
	}
	return pick(e.r, others)
}

// verdictRelayers returns the live sim accounts allowed to relay a jury
// verdict: members of the Commons Operations Committee.
func (e *simEnv) verdictRelayers() []simtypes.Account {
	var out []simtypes.Account
	for _, addr := range e.groupMembers(opsCommittee) {
		if acc, ok := e.account(addr); ok && e.spendable(acc).IsPositive() {
			out = append(out, acc)
		}
	}
	return out
}

// --- x/service views (keeper reads only) ---

// operators returns the live operator records accepted by keep.
func (e *simEnv) operators(keep func(types.Operator) bool) ([]types.Operator, error) {
	var out []types.Operator
	err := e.k.Operators.Walk(e.ctx, nil, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
		if keep == nil || keep(op) {
			out = append(out, op)
		}
		return false, nil
	})
	return out, err
}

// signedOperators returns the live operators accepted by keep whose address
// is a live sim account (the operator-signed messages).
func (e *simEnv) signedOperators(keep func(types.Operator) bool) ([]types.Operator, error) {
	return e.operators(func(op types.Operator) bool {
		_, ok := e.account(op.Address)
		return ok && (keep == nil || keep(op))
	})
}

// operator returns the live operator record of (addr, serviceType).
func (e *simEnv) operator(addr, serviceType string) (types.Operator, bool) {
	acc, err := sdk.AccAddressFromBech32(addr)
	if err != nil {
		return types.Operator{}, false
	}
	return e.k.GetOperator(e.ctx, acc, serviceType)
}

// serviceTypes returns the enabled service types.
func (e *simEnv) serviceTypes() ([]types.ServiceTypeConfig, error) {
	var out []types.ServiceTypeConfig
	err := e.k.ServiceTypes.Walk(e.ctx, nil, func(_ string, cfg types.ServiceTypeConfig) (bool, error) {
		if cfg.Enabled {
			out = append(out, cfg)
		}
		return false, nil
	})
	return out, err
}

// serviceType returns a service type's config.
func (e *simEnv) serviceType(name string) (types.ServiceTypeConfig, bool, error) {
	cfg, err := e.k.ServiceTypes.Get(e.ctx, name)
	if errors.Is(err, collections.ErrNotFound) {
		return types.ServiceTypeConfig{}, false, nil
	}
	return cfg, err == nil, err
}

// reports returns the reports with status accepted by keep.
func (e *simEnv) reports(status types.ReportStatus, keep func(types.Report) bool) ([]types.Report, error) {
	var out []types.Report
	err := e.k.Reports.Walk(e.ctx, nil, func(_ uint64, rep types.Report) (bool, error) {
		if rep.Status == status && (keep == nil || keep(rep)) {
			out = append(out, rep)
		}
		return false, nil
	})
	return out, err
}

// transferCases returns the open controller-transfer cases.
func (e *simEnv) transferCases() ([]types.ControllerTransferCase, error) {
	var out []types.ControllerTransferCase
	err := e.k.ControllerTransferCases.Walk(e.ctx, nil, func(_ uint64, c types.ControllerTransferCase) (bool, error) {
		out = append(out, c)
		return false, nil
	})
	return out, err
}

// pendingWithdraw is an operator's queued partial withdrawal.
func pendingWithdraw(op types.Operator) math.Int {
	if op.PendingWithdrawAmount.IsNil() {
		return math.ZeroInt()
	}
	return op.PendingWithdrawAmount
}

// --- Prerequisites ---

// registerMsg builds a MsgRegisterOperator for acc under controller, or a
// reason why acc cannot register. It returns the bond the handler escrows.
func (e *simEnv) registerMsg(acc simtypes.Account, controller string) (*types.MsgRegisterOperator, math.Int, string, error) {
	cfgs, err := e.serviceTypes()
	if err != nil {
		return nil, math.ZeroInt(), "", err
	}
	var open []types.ServiceTypeConfig
	for _, cfg := range cfgs {
		if _, exists := e.k.GetOperator(e.ctx, acc.Address, cfg.ServiceType); exists {
			continue
		}
		slashed, err := e.k.HasSlashedRecord(e.ctx, acc.Address, cfg.ServiceType)
		if err != nil {
			return nil, math.ZeroInt(), "", err
		}
		if !slashed {
			open = append(open, cfg)
		}
	}
	cfg, ok := pick(e.r, open)
	if !ok {
		return nil, math.ZeroInt(), "signer holds or lost a record under every enabled service type", nil
	}
	minBond := cfg.MinBondAmount
	if minBond.IsNil() || !minBond.IsPositive() {
		minBond = math.OneInt()
	}
	// Bond: min_bond plus up to one more min_bond, so most operators have
	// spare bond to withdraw and room above the floor for a tier-1 slash.
	bond := minBond.Add(simtypes.RandomAmount(e.r, minBond))
	if e.spendable(acc).LTE(bond) {
		bond = minBond
	}
	msg := &types.MsgRegisterOperator{
		Creator:     acc.Address.String(),
		ServiceType: cfg.ServiceType,
		Controller:  controller,
		BondAmount:  bond,
		Metadata:    randomMetadata(e.r),
	}
	return msg, bond, "", nil
}

// registerOperator delivers a MsgRegisterOperator under controller (a
// random controller when empty) from a live sim account and returns the new
// record.
func (e *simEnv) registerOperator(controller string) (types.Operator, bool, error) {
	if controller == "" {
		c, ok := e.controllerPolicy()
		if !ok {
			return types.Operator{}, false, nil
		}
		controller = c
	}
	for _, i := range e.r.Perm(len(e.accs)) {
		acc := e.accs[i]
		if acc.Address.String() == controller || e.usable(acc, math.NewInt(1)) != "" {
			continue
		}
		msg, bond, reason, err := e.registerMsg(acc, controller)
		if err != nil {
			return types.Operator{}, false, err
		}
		if reason != "" || e.spendable(acc).LTE(bond) {
			continue
		}
		ok, err := e.ensure(acc, msg, bond)
		if err != nil || !ok {
			return types.Operator{}, false, err
		}
		op, found := e.operator(msg.Creator, msg.ServiceType)
		return op, found, nil
	}
	return types.Operator{}, false, nil
}

// signedOperator returns a live operator accepted by keep whose address is
// a live sim account, registering one first (a real MsgRegisterOperator)
// when none exists. A freshly registered operator is ACTIVE.
func (e *simEnv) signedOperator(keep func(types.Operator) bool) (types.Operator, simtypes.Account, bool, error) {
	ops, err := e.signedOperators(keep)
	if err != nil {
		return types.Operator{}, simtypes.Account{}, false, err
	}
	if op, ok := pick(e.r, ops); ok {
		acc, _ := e.account(op.Address)
		return op, acc, true, nil
	}
	op, ok, err := e.registerOperator("")
	if err != nil || !ok || (keep != nil && !keep(op)) {
		return types.Operator{}, simtypes.Account{}, false, err
	}
	acc, ok := e.account(op.Address)
	return op, acc, ok, nil
}

// reportMsg builds a MsgReportOperator against op from a reporter who
// passes the handler's gates (trust level, not on the controller group, can
// pay the deposit), or a reason why there is none.
func (e *simEnv) reportMsg(op types.Operator) (*types.MsgReportOperator, simtypes.Account, string) {
	deposit := e.params.ReportDepositAmount
	for _, i := range e.r.Perm(len(e.accs)) {
		acc := e.accs[i]
		addr := acc.Address.String()
		if addr == op.Address || e.usable(acc, deposit) != "" || !e.meetsReporterTrust(addr) || e.isControllerMember(op.Controller, addr) {
			continue
		}
		msg := &types.MsgReportOperator{
			Reporter:    addr,
			Operator:    op.Address,
			ServiceType: op.ServiceType,
			Reason:      randomReason(e.r),
		}
		if e.dryRun(msg) != nil {
			continue
		}
		return msg, acc, ""
	}
	return nil, simtypes.Account{}, "no sim account may report the operator"
}

// --- Random data ---

// fictionalNames are the generic names simulated metadata draws from.
var fictionalNames = []string{"phoenix", "aurora", "zenith", "nebula", "cascade", "ember", "horizon", "prism"}

// randomMetadata returns an operator metadata blob (well under the cap).
func randomMetadata(r *rand.Rand) []byte {
	name := fictionalNames[r.Intn(len(fictionalNames))]
	return []byte(fmt.Sprintf(`{"moniker":"%s-%d","endpoint":"https://%s.example/%d"}`, name, r.Intn(1000), name, r.Intn(1000)))
}

// randomReason returns a short report, case or juror reason.
func randomReason(r *rand.Rand) string {
	reasons := []string{
		"missed checkpoints for several windows",
		"served stale results to clients",
		"endpoint unreachable during the audit",
		"controller no longer reviews this operator",
		"verdicts disagreed with independent workers",
	}
	return reasons[r.Intn(len(reasons))]
}

// randomRoot returns a 32-byte checkpoint root.
func randomRoot(r *rand.Rand) []byte {
	seed := make([]byte, 16)
	r.Read(seed)
	sum := sha256.Sum256(seed)
	return sum[:]
}

// operatorBytes decodes an operator's address.
func (e *simEnv) operatorBytes(op types.Operator) (sdk.AccAddress, error) {
	return sdk.AccAddressFromBech32(op.Address)
}
