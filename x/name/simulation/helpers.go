package simulation

import (
	"errors"
	"fmt"
	"math/rand"
	"regexp"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commontypes "sparkdream/x/common/types"
	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
)

// The x/name simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions, dry-runs the message through its real handler on
// a throwaway cache of the block state, and returns a NoOp when it cannot
// hold. No op writes a store: a prerequisite (a name to dispute, a pending
// resolver target to accept) is built by delivering the real message first.
//
// MsgResolveDispute is signed by the Commons Council policy address, which
// only an executed x/commons proposal can sign for (council.go): a council
// seat submits the proposal, the seats vote it through, anyone executes it.
// Sim accounts are never made a policy and no other module's store is
// written.

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
	names        []types.NameRecord
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
		// stream, so how much randomness an op uses (which depends on name
		// state) never shifts the draws of the other modules' ops.
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

// submit dry-runs msg and delivers it when the handler accepts it. The
// signer must hold spent SPARK on top of the tx fee and must not be a
// tombstoned (migrated-away) address, which the ante handler refuses.
func (e *simEnv) submit(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if e.tombstoned(acc.Address.String()) {
		return noOp(msg, "signer is a tombstoned address")
	}
	if spent.IsPositive() && e.spendable(acc).LT(spent) {
		return noOp(msg, "signer cannot pay "+spent.String())
	}
	if err := e.dryRun(msg); err != nil {
		return noOp(msg, "handler would reject: "+err.Error())
	}
	return e.deliver(acc, msg, spent)
}

// ensure delivers a prerequisite message from acc (dry-run first). It
// returns false when the handler would refuse it.
func (e *simEnv) ensure(acc simtypes.Account, msg sdk.Msg, spent math.Int) (bool, error) {
	if e.tombstoned(acc.Address.String()) {
		return false, nil
	}
	if spent.IsPositive() && e.spendable(acc).LT(spent) {
		return false, nil
	}
	if e.dryRun(msg) != nil {
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
	e.names = nil
}

// denom is the chain's bond denom (registration fees, tx fees).
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

// isActiveMember reports whether addr is an active x/rep member (the
// registration and transfer gate).
func (e *simEnv) isActiveMember(addr string) bool {
	m := e.member(addr)
	return m != nil && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE
}

// unlockedDream returns an active member's DREAM not locked in stakes
// (before pending decay, which the dry-run applies).
func (e *simEnv) unlockedDream(addr string) math.Int {
	m := e.member(addr)
	if m == nil || m.Status != reptypes.MemberStatus_MEMBER_STATUS_ACTIVE || m.DreamBalance == nil {
		return math.ZeroInt()
	}
	if m.StakedDream == nil {
		return *m.DreamBalance
	}
	return m.DreamBalance.Sub(*m.StakedDream)
}

// canStake reports whether addr can lock amount DREAM.
func (e *simEnv) canStake(addr string, amount math.Int) bool {
	return e.isActiveMember(addr) && e.unlockedDream(addr).GTE(amount)
}

// sameMember reports whether two addresses belong to the same member
// (x/rep alias contract, as the handlers check it).
func (e *simEnv) sameMember(a, b string) bool {
	if a == b {
		return true
	}
	rk := e.k.GetRepKeeper()
	return rk != nil && rk.SameMember(e.ctx, a, b)
}

// --- x/name views (keeper reads only) ---

// allNames returns every registered name record, read once per operation.
func (e *simEnv) allNames() ([]types.NameRecord, error) {
	if e.names != nil {
		return e.names, nil
	}
	names := []types.NameRecord{}
	err := e.k.Names.Walk(e.ctx, nil, func(_ string, rec types.NameRecord) (bool, error) {
		names = append(names, rec)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	e.names = names
	return names, nil
}

// namesWhere returns the name records accepted by keep.
func (e *simEnv) namesWhere(keep func(types.NameRecord) bool) ([]types.NameRecord, error) {
	all, err := e.allNames()
	if err != nil {
		return nil, err
	}
	var out []types.NameRecord
	for _, rec := range all {
		if keep(rec) {
			out = append(out, rec)
		}
	}
	return out, nil
}

// name returns a name record.
func (e *simEnv) name(name string) (types.NameRecord, bool, error) {
	rec, err := e.k.Names.Get(e.ctx, name)
	if errors.Is(err, collections.ErrNotFound) {
		return types.NameRecord{}, false, nil
	}
	return rec, err == nil, err
}

// dispute returns the dispute record of a name.
func (e *simEnv) dispute(name string) (types.Dispute, bool, error) {
	d, err := e.k.Disputes.Get(e.ctx, name)
	if errors.Is(err, collections.ErrNotFound) {
		return types.Dispute{}, false, nil
	}
	return d, err == nil, err
}

// activeDispute reports whether a name is under an active dispute.
func (e *simEnv) activeDispute(name string) (bool, error) {
	d, found, err := e.dispute(name)
	return found && d.Active, err
}

// activeDisputes returns the active disputes accepted by keep.
func (e *simEnv) activeDisputes(keep func(types.Dispute) bool) ([]types.Dispute, error) {
	var out []types.Dispute
	err := e.k.Disputes.Walk(e.ctx, nil, func(_ string, d types.Dispute) (bool, error) {
		if d.Active && keep(d) {
			out = append(out, d)
		}
		return false, nil
	})
	return out, err
}

// ownedCount returns how many names addr owns.
func (e *simEnv) ownedCount(addr sdk.AccAddress) (uint64, error) {
	return e.k.GetOwnedNamesCount(e.ctx, addr)
}

// belowCap reports whether addr may hold one more name.
func (e *simEnv) belowCap(addr sdk.AccAddress) (bool, error) {
	n, err := e.ownedCount(addr)
	return err == nil && n < e.params.MaxNamesPerAddress, err
}

// contestDeadline is the last block an owner may contest d.
func (e *simEnv) contestDeadline(d types.Dispute) int64 {
	return d.FiledAt + commontypes.Saturate[int64](e.params.DisputeTimeoutBlocks)
}

// --- Prerequisites ---

// registrationFee is the SPARK MsgRegisterName burns.
func (e *simEnv) registrationFee() math.Int {
	if e.params.RegistrationFeeAmount.IsNil() {
		return math.ZeroInt()
	}
	return e.params.RegistrationFeeAmount
}

// canRegister reports whether acc passes MsgRegisterName's signer gates:
// an active member below the name cap that can pay the fee.
func (e *simEnv) canRegister(acc simtypes.Account) (bool, error) {
	if !e.isActiveMember(acc.Address.String()) || e.spendable(acc).LT(e.registrationFee()) {
		return false, nil
	}
	return e.belowCap(acc.Address)
}

// registrants returns the live sim accounts accepted by keep that can
// register a name.
func (e *simEnv) registrants(keep func(simtypes.Account) bool) ([]simtypes.Account, error) {
	var out []simtypes.Account
	for _, acc := range e.live(keep) {
		ok, err := e.canRegister(acc)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, acc)
		}
	}
	return out, nil
}

// freshName returns a valid name nobody holds, or false when the draws
// collided.
func (e *simEnv) freshName() (string, bool, error) {
	for range 5 {
		n := randomName(e.r, e.params)
		if n == "" || blocked(n, e.params.BlockedNames) {
			continue
		}
		_, taken, err := e.name(n)
		if err != nil {
			return "", false, err
		}
		if !taken {
			return n, true, nil
		}
	}
	return "", false, nil
}

// registerFor delivers a MsgRegisterName for acc and returns the new name.
// ok is false when no name could be registered.
func (e *simEnv) registerFor(acc simtypes.Account) (types.NameRecord, bool, error) {
	n, ok, err := e.freshName()
	if err != nil || !ok {
		return types.NameRecord{}, false, err
	}
	msg := &types.MsgRegisterName{Authority: acc.Address.String(), Name: n, Data: randomData(e.r)}
	if ok, err := e.ensure(acc, msg, e.registrationFee()); err != nil || !ok {
		return types.NameRecord{}, false, err
	}
	rec, found, err := e.name(n)
	return rec, found, err
}

// ownedName returns a name whose owner is a live sim account accepted by
// keepOwner and which keepName accepts, registering one first (a real
// MsgRegisterName) when none exists. ok is false when there is neither.
func (e *simEnv) ownedName(keepOwner func(simtypes.Account) bool, keepName func(types.NameRecord) bool) (types.NameRecord, simtypes.Account, bool, error) {
	recs, err := e.namesWhere(func(rec types.NameRecord) bool {
		acc, ok := e.account(rec.Owner)
		return ok && keepOwner(acc) && (keepName == nil || keepName(rec))
	})
	if err != nil {
		return types.NameRecord{}, simtypes.Account{}, false, err
	}
	if rec, ok := pick(e.r, recs); ok {
		acc, _ := e.account(rec.Owner)
		return rec, acc, true, nil
	}
	regs, err := e.registrants(keepOwner)
	if err != nil {
		return types.NameRecord{}, simtypes.Account{}, false, err
	}
	acc, ok := pick(e.r, regs)
	if !ok {
		return types.NameRecord{}, simtypes.Account{}, false, nil
	}
	rec, ok, err := e.registerFor(acc)
	if err != nil || !ok || (keepName != nil && !keepName(rec)) {
		return types.NameRecord{}, simtypes.Account{}, false, err
	}
	return rec, acc, true, nil
}

// --- Random data ---

// fictionalNames are the generic names simulated handles and display names
// draw from.
var fictionalNames = []string{"phoenix", "aurora", "zenith", "nebula", "cascade", "ember", "horizon", "prism"}

// validNameRegex mirrors the MsgRegisterName character rule.
var validNameRegex = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?$`)

// randomLowerAlnum returns n random lowercase letters and digits.
func randomLowerAlnum(r *rand.Rand, n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[r.Intn(len(chars))]
	}
	return string(b)
}

// randomName returns a name valid under params (length and characters):
// a fictional stem with a random suffix, cut to a random allowed length.
// It returns "" when params allow no name.
func randomName(r *rand.Rand, params types.Params) string {
	minLen := max(commontypes.Saturate[int](params.MinNameLength), 1)
	maxLen := commontypes.Saturate[int](params.MaxNameLength)
	if maxLen < minLen {
		return ""
	}
	n := minLen + r.Intn(min(maxLen, minLen+16)-minLen+1)
	stem := fictionalNames[r.Intn(len(fictionalNames))]
	sep := []string{"", "_", "-"}[r.Intn(3)]
	s := stem + sep + randomLowerAlnum(r, n)
	s = s[:n]
	if !validNameRegex.MatchString(s) {
		// A cut can end on the separator: replace the last byte.
		s = s[:n-1] + randomLowerAlnum(r, 1)
	}
	if !validNameRegex.MatchString(s) {
		return ""
	}
	return s
}

// blocked reports whether name is on the blocked list (case-insensitive,
// as the handler compares).
func blocked(name string, list []string) bool {
	for _, b := range list {
		if normalize(b) == name {
			return true
		}
	}
	return false
}

// normalize lowercases and trims like the handlers do.
func normalize(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	start, end := 0, len(out)
	for start < end && (out[start] == ' ' || out[start] == '\t') {
		start++
	}
	for end > start && (out[end-1] == ' ' || out[end-1] == '\t') {
		end--
	}
	return string(out[start:end])
}

// randomData returns random name metadata (well under MaxNameDataLen).
func randomData(r *rand.Rand) string {
	return fmt.Sprintf("ipfs://%s", randomLowerAlnum(r, 10+r.Intn(40)))
}

// randomDisplayName returns a valid display name (ASCII, NFC, no edge
// whitespace, 1-32 code points).
func randomDisplayName(r *rand.Rand) string {
	stem := fictionalNames[r.Intn(len(fictionalNames))]
	switch r.Intn(3) {
	case 0:
		return stem
	case 1:
		return fmt.Sprintf("%s %d", stem, r.Intn(1000))
	default:
		return fmt.Sprintf("%s of %s", stem, fictionalNames[r.Intn(len(fictionalNames))])
	}
}

// randomReason returns a short dispute or contest reason.
func randomReason(r *rand.Rand) string {
	reasons := []string{
		"impersonates an established project handle",
		"squatted shortly after the community adopted it",
		"held in good faith since registration",
		"the claimant has prior public use of the name",
	}
	return reasons[r.Intn(len(reasons))]
}

// zero is a zero SPARK amount (a message that takes no SPARK).
func zero() math.Int { return math.ZeroInt() }

// anyone accepts every account.
func anyone(simtypes.Account) bool { return true }
