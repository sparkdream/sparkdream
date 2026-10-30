package simulation

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"slices"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	commontypes "sparkdream/x/common/types"
	commonstypes "sparkdream/x/commons/types"
	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
)

// The federation simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions against live state and params, and returns a
// NoOp when one cannot hold. Prerequisite state (a bridge binding, a bonded
// verifier, pending or verified content) is only ever created by delivering
// the real message first.
//
// Two facts bound what the simulation can reach:
//
//   - Peers are registered, suspended and their policies edited by a
//     Commons Operations Committee member (or a policy address), and
//     activated, removed or re-moderated only by a policy address (an
//     executed committee/council proposal or x/gov). Sim accounts are never
//     made a group or committee policy, so a peer registered here stays
//     PENDING. The simulated genesis (x/federation/module) therefore seeds
//     ACTIVE peers, and seats sim accounts on the Operations Committee
//     (x/commons/module), so registration, suspension and policy edits
//     have signers.
//   - The Spark Dream (IBC) peer paths send packets, and the simulation has
//     no IBC channel.
//
// Every bridge, content, verification, arbitration and identity-link op
// needs an ACTIVE external peer, so they are NoOps whenever none exists, and
// deliver as soon as one does.
//
// The x/rep, x/commons and x/service side of a handler (membership, trust
// level, bonded-role eligibility, committee seats, operator bonds) is read
// through those modules' query services, and each message is finally
// dry-run through its real handler on a throwaway cache of the block state
// (submit). Only a message that dry-run accepts is delivered.

// opsCommitteeName is the commons group whose members (besides gov and the
// council/committee policies) pass the federation's IsCouncilAuthorized gate.
const opsCommitteeName = "Commons Operations Committee"

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
	serviceQuery servicetypes.QueryClient
	members      map[string]reptypes.Member
	tombstones   map[string]bool
}

// operation builds a simulation operation: it loads params and hands the
// body a simEnv. msg is only used to label a NoOp when params are missing.
func operation(
	ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig, msg sdk.Msg,
	body func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error),
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, _ string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return noOpErr(msg, err)
		}
		// Each operation seeds its own source from two draws of the shared
		// stream, so how much randomness an op uses (which depends on
		// federation state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k, params: params}
		helper := &baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx}
		e.repQuery = reptypes.NewQueryClient(helper)
		e.commonsQuery = commonstypes.NewQueryClient(helper)
		e.serviceQuery = servicetypes.NewQueryClient(helper)
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

// deliver signs msg with acc and delivers it as a real transaction. spent is
// the SPARK the handler takes from acc (fees, escrows, bonds); the random tx
// fee is drawn only from what is left, so the handler still finds it.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	coins := sdk.NewCoins()
	if spent.IsPositive() {
		coins = sdk.NewCoins(sdk.NewCoin(e.k.BondDenom(e.ctx), spent))
	}
	return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
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
	})
}

// submit dry-runs msg and delivers it when the handler accepts it. The
// signer must hold spent SPARK on top of the tx fee, and must not be a
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
	e.members = nil // a delivered tx may have moved DREAM or membership state
	return opMsg.OK, nil
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.k.BondDenom(e.ctx))
}

// now is the block time in unix seconds, the clock every federation handler
// uses.
func (e *simEnv) now() int64 {
	return e.ctx.BlockTime().Unix()
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

// getAccountForAddress finds a sim account that matches the given address string.
func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// live returns the sim accounts that are not tombstoned.
func (e *simEnv) live() []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool { return !e.tombstoned(a.Address.String()) })
}

// account returns the live (not tombstoned) sim account for addr.
func (e *simEnv) account(addr string) (simtypes.Account, bool) {
	acc, ok := getAccountForAddress(addr, e.accs)
	if !ok || e.tombstoned(addr) {
		return simtypes.Account{}, false
	}
	return acc, true
}

// --- x/commons views ---

// opsCommittee returns the live sim accounts that are members of the
// Commons Operations Committee, which pass IsCouncilAuthorized for
// "commons"/"operations" directly. Sim accounts are never made a group or
// committee policy.
func (e *simEnv) opsCommittee() []simtypes.Account {
	res, err := e.commonsQuery.GetCouncilMembers(e.ctx, &commonstypes.QueryGetCouncilMembersRequest{CouncilName: opsCommitteeName})
	if err != nil {
		return nil
	}
	var out []simtypes.Account
	for _, m := range res.Members {
		if acc, ok := e.account(m.Address); ok {
			out = append(out, acc)
		}
	}
	return out
}

// --- x/rep views ---

// loadMembers reads every x/rep member once per operation.
func (e *simEnv) loadMembers() map[string]reptypes.Member {
	if e.members != nil {
		return e.members
	}
	e.members = map[string]reptypes.Member{}
	var next []byte
	for {
		res, err := e.repQuery.ListMember(e.ctx, &reptypes.QueryAllMemberRequest{
			Pagination: &query.PageRequest{Key: next, Limit: 500},
		})
		if err != nil {
			return e.members
		}
		for _, m := range res.Member {
			e.members[m.Address] = m
		}
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			return e.members
		}
		next = res.Pagination.NextKey
	}
}

// isActiveMember mirrors x/rep's IsActiveMember for a plain address.
func (e *simEnv) isActiveMember(addr string) bool {
	m, ok := e.loadMembers()[addr]
	return ok && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE
}

// trustAtLeast reports whether addr is an active member at or above level.
func (e *simEnv) trustAtLeast(addr string, level reptypes.TrustLevel) bool {
	m, ok := e.loadMembers()[addr]
	return ok && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE && m.TrustLevel >= level
}

// unlockedDream returns an active member's DREAM not locked in stakes (before
// pending decay, which the dry-run applies).
func (e *simEnv) unlockedDream(addr string) math.Int {
	m, ok := e.loadMembers()[addr]
	if !ok || m.Status != reptypes.MemberStatus_MEMBER_STATUS_ACTIVE || m.DreamBalance == nil {
		return math.ZeroInt()
	}
	if m.StakedDream == nil {
		return *m.DreamBalance
	}
	return m.DreamBalance.Sub(*m.StakedDream)
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

// sameMember approximates x/rep's SameMember for sim accounts: equal
// addresses, or two addresses that resolve to the same canonical member.
// The dry-run applies the exact alias contract.
func (e *simEnv) sameMember(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return e.canonical(a) == e.canonical(b)
}

// canonical resolves addr through x/rep's alias contract (addr itself when
// the query fails).
func (e *simEnv) canonical(addr string) string {
	res, err := e.repQuery.Canonical(e.ctx, &reptypes.QueryCanonicalRequest{Address: addr})
	if err != nil || res.Canonical == "" {
		return addr
	}
	return res.Canonical
}

// bondedRole returns addr's bonded role record.
func (e *simEnv) bondedRole(role reptypes.RoleType, addr string) (reptypes.BondedRole, bool) {
	res, err := e.repQuery.BondedRole(e.ctx, &reptypes.QueryBondedRoleRequest{RoleType: role, Address: addr})
	if err != nil {
		return reptypes.BondedRole{}, false
	}
	return res.BondedRole, true
}

// verifiers returns the live sim accounts holding FEDERATION_VERIFIER in a
// status that can act (the handler's EligibleForRole is dry-run).
func (e *simEnv) verifiers() []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		br, ok := e.bondedRole(reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, addr)
		return ok && br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED &&
			e.isActiveMember(addr) && !e.tombstoned(addr)
	})
}

// ensureVerifiers returns the sim accounts holding FEDERATION_VERIFIER,
// bonding one first through x/rep's MsgBondRole when none accepted by keep
// does: an active member at the role's trust level with min_bond unlocked
// DREAM bonds exactly min_bond (the role config is a projection of the
// federation verifier params).
func (e *simEnv) ensureVerifiers(keep func(simtypes.Account) bool) ([]simtypes.Account, error) {
	role := reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER
	if holders := accountsWhere(e.verifiers(), keep); len(holders) > 0 {
		return holders, nil
	}
	res, err := e.repQuery.BondedRoleConfig(e.ctx, &reptypes.QueryBondedRoleConfigRequest{RoleType: role})
	if err != nil {
		return nil, nil
	}
	cfg := res.BondedRoleConfig
	minBond, ok := math.NewIntFromString(cfg.MinBond)
	if !ok || !minBond.IsPositive() {
		return nil, nil
	}
	level := reptypes.TrustLevel_TRUST_LEVEL_NEW
	if cfg.MinTrustLevel != "" {
		v, known := reptypes.TrustLevel_value[cfg.MinTrustLevel]
		if !known {
			return nil, nil
		}
		level = reptypes.TrustLevel(v)
	}
	candidates := accountsWhere(e.live(), func(a simtypes.Account) bool {
		addr := a.Address.String()
		if _, has := e.bondedRole(role, addr); has {
			return false
		}
		return keep(a) && e.trustAtLeast(addr, level) && e.unlockedDream(addr).GTE(minBond)
	})
	acc, ok := pick(e.r, candidates)
	if !ok {
		return nil, nil
	}
	msg := &reptypes.MsgBondRole{Creator: acc.Address.String(), RoleType: role, Amount: minBond.String()}
	if ok, err := e.ensure(acc, msg, math.ZeroInt()); err != nil || !ok {
		return nil, err
	}
	return accountsWhere(e.verifiers(), keep), nil
}

// --- x/service views ---

// minBond returns the x/service min_bond of a service type.
func (e *simEnv) minBond(serviceType string) (math.Int, bool) {
	res, err := e.serviceQuery.ServiceType(e.ctx, &servicetypes.QueryServiceTypeRequest{ServiceType: serviceType})
	if err != nil || res.Config.MinBondAmount.IsNil() {
		return math.Int{}, false
	}
	return res.Config.MinBondAmount, true
}

// serviceOperator returns addr's x/service operator record for a service
// type.
func (e *simEnv) serviceOperator(addr, serviceType string) (servicetypes.Operator, bool) {
	res, err := e.serviceQuery.Operator(e.ctx, &servicetypes.QueryOperatorRequest{Address: addr, ServiceType: serviceType})
	if err != nil {
		return servicetypes.Operator{}, false
	}
	return res.Operator, true
}

// --- Federation state views ---

// serviceTypeFor maps a bridged peer type to its x/service service_type
// (the handler's serviceTypeForPeer); false for Spark Dream (IBC) peers.
func serviceTypeFor(t types.PeerType) (string, bool) {
	switch t {
	case types.PeerType_PEER_TYPE_ACTIVITYPUB:
		return types.ServiceTypeFederationBridgeActivityPub, true
	case types.PeerType_PEER_TYPE_ATPROTO:
		return types.ServiceTypeFederationBridgeATProto, true
	case types.PeerType_PEER_TYPE_NOSTR:
		return types.ServiceTypeFederationBridgeNOSTR, true
	case types.PeerType_PEER_TYPE_LENS:
		return types.ServiceTypeFederationBridgeLENS, true
	}
	return "", false
}

// peersWhere returns the peers accepted by keep.
func (e *simEnv) peersWhere(keep func(types.Peer) bool) ([]types.Peer, error) {
	var out []types.Peer
	err := e.k.Peers.Walk(e.ctx, nil, func(_ string, p types.Peer) (bool, error) {
		if keep(p) {
			out = append(out, p)
		}
		return false, nil
	})
	return out, err
}

// peer returns a peer by id.
func (e *simEnv) peer(id string) (types.Peer, bool) {
	p, err := e.k.Peers.Get(e.ctx, id)
	return p, err == nil
}

// policy returns a peer's policy.
func (e *simEnv) policy(peerID string) (types.PeerPolicy, bool) {
	p, err := e.k.PeerPolicies.Get(e.ctx, peerID)
	return p, err == nil
}

// activeBridged reports whether p is an ACTIVE external-protocol peer, the
// only kind a bridge can bind to.
func activeBridged(p types.Peer) bool {
	_, bridged := serviceTypeFor(p.Type)
	return bridged && p.Status == types.PeerStatus_PEER_STATUS_ACTIVE
}

// bindingsWhere returns the bridge bindings accepted by keep.
func (e *simEnv) bindingsWhere(keep func(types.BridgeBinding) bool) ([]types.BridgeBinding, error) {
	var out []types.BridgeBinding
	err := e.k.BridgeBindings.Walk(e.ctx, nil, func(_ collections.Pair[string, string], b types.BridgeBinding) (bool, error) {
		if keep(b) {
			out = append(out, b)
		}
		return false, nil
	})
	return out, err
}

// binding returns the binding of operator on peerID.
func (e *simEnv) binding(operator, peerID string) (types.BridgeBinding, bool) {
	b, err := e.k.BridgeBindings.Get(e.ctx, collections.Join(operator, peerID))
	return b, err == nil
}

// workingBinding reports whether b can act for its peer under the handlers'
// shared gates: not suspended, ACTIVE peer, a live sim operator that is an
// active member and whose x/service record is ACTIVE at or above min_bond.
func (e *simEnv) workingBinding(b types.BridgeBinding) bool {
	if b.Suspended {
		return false
	}
	p, ok := e.peer(b.PeerId)
	if !ok || !activeBridged(p) {
		return false
	}
	if _, ok := e.account(b.Address); !ok || !e.isActiveMember(b.Address) {
		return false
	}
	st, _ := serviceTypeFor(p.Type)
	op, ok := e.serviceOperator(b.Address, st)
	if !ok || op.Status != servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE {
		return false
	}
	if mb, ok := e.minBond(st); ok && op.BondAmount.LT(mb) {
		return false
	}
	return true
}

// workingBindings returns the working bindings accepted by keep.
func (e *simEnv) workingBindings(keep func(types.BridgeBinding) bool) ([]types.BridgeBinding, error) {
	return e.bindingsWhere(func(b types.BridgeBinding) bool { return keep(b) && e.workingBinding(b) })
}

// registerBridgeMsg builds a MsgRegisterBridge binding acc to an ACTIVE
// bridged peer, with the SPARK it escrows (the service min_bond for a new
// x/service operator, nothing for one that already exists). A non-empty
// reason means acc cannot register it now.
func (e *simEnv) registerBridgeMsg(acc simtypes.Account, p types.Peer) (*types.MsgRegisterBridge, math.Int, string) {
	addr := acc.Address.String()
	msg := &types.MsgRegisterBridge{
		Operator: addr,
		PeerId:   p.Id,
		Protocol: protocolFor(p.Type),
		Endpoint: randomEndpoint(e.r),
	}
	if !activeBridged(p) {
		return msg, math.ZeroInt(), "peer is not an ACTIVE bridged peer"
	}
	if !e.isActiveMember(addr) {
		return msg, math.ZeroInt(), "operator is not an active member"
	}
	if _, has := e.binding(addr, p.Id); has {
		return msg, math.ZeroInt(), "binding already exists"
	}
	st, _ := serviceTypeFor(p.Type)
	if op, exists := e.serviceOperator(addr, st); exists {
		if op.Status != servicetypes.OperatorStatus_OPERATOR_STATUS_ACTIVE {
			return msg, math.ZeroInt(), "operator's x/service record is " + op.Status.String()
		}
		msg.StakeAmount = math.ZeroInt()
		return msg, math.ZeroInt(), ""
	}
	mb, ok := e.minBond(st)
	if !ok || !mb.IsPositive() {
		return msg, math.ZeroInt(), "service type " + st + " has no min_bond"
	}
	msg.StakeAmount = mb
	return msg, mb, ""
}

// ensureBinding returns a working binding accepted by keep, registering
// one first through MsgRegisterBridge from an active member (accepted by
// operatorOK) on an ACTIVE bridged peer (accepted by peerOK) when none
// exists.
func (e *simEnv) ensureBinding(
	keep func(types.BridgeBinding) bool, peerOK func(types.Peer) bool, operatorOK func(simtypes.Account) bool,
) (types.BridgeBinding, bool, error) {
	existing, err := e.workingBindings(keep)
	if err != nil {
		return types.BridgeBinding{}, false, err
	}
	if b, ok := pick(e.r, existing); ok {
		return b, true, nil
	}
	peers, err := e.peersWhere(func(p types.Peer) bool { return activeBridged(p) && peerOK(p) })
	if err != nil {
		return types.BridgeBinding{}, false, err
	}
	p, ok := pick(e.r, peers)
	if !ok {
		return types.BridgeBinding{}, false, nil
	}
	candidates := accountsWhere(e.live(), func(a simtypes.Account) bool {
		addr := a.Address.String()
		_, has := e.binding(addr, p.Id)
		return !has && operatorOK(a) && e.isActiveMember(addr)
	})
	acc, ok := pick(e.r, candidates)
	if !ok {
		return types.BridgeBinding{}, false, nil
	}
	msg, spent, reason := e.registerBridgeMsg(acc, p)
	if reason != "" {
		return types.BridgeBinding{}, false, nil
	}
	if ok, err := e.ensure(acc, msg, spent); err != nil || !ok {
		return types.BridgeBinding{}, false, err
	}
	b, found := e.binding(acc.Address.String(), p.Id)
	if !found || !keep(b) || !e.workingBinding(b) {
		return types.BridgeBinding{}, false, nil
	}
	return b, true, nil
}

// contentsWhere returns the federated content records accepted by keep.
func (e *simEnv) contentsWhere(keep func(types.FederatedContent) bool) ([]types.FederatedContent, error) {
	var out []types.FederatedContent
	err := e.k.Content.Walk(e.ctx, nil, func(_ uint64, c types.FederatedContent) (bool, error) {
		if keep(c) {
			out = append(out, c)
		}
		return false, nil
	})
	return out, err
}

// content returns a content record by id.
func (e *simEnv) content(id uint64) (types.FederatedContent, bool) {
	c, err := e.k.Content.Get(e.ctx, id)
	return c, err == nil
}

// record returns the verification record of a content id.
func (e *simEnv) record(id uint64) (types.VerificationRecord, bool) {
	r, err := e.k.VerificationRecords.Get(e.ctx, id)
	return r, err == nil
}

// escalated reports whether a content id has an open jury escalation.
func (e *simEnv) escalated(id uint64) bool {
	has, err := e.k.EscalatedChallenges.Has(e.ctx, id)
	return err != nil || has
}

// pendingVerifiable reports whether c is PENDING_VERIFICATION with its
// verification window still open.
func (e *simEnv) pendingVerifiable(c types.FederatedContent) bool {
	return c.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION &&
		e.now() <= c.ReceivedAt+int64(e.params.VerificationWindow.Seconds())
}

// submitContentMsg builds a MsgSubmitFederatedContent for a working binding
// that passes the handler's local checks: an inbound content type, a
// public-domain license, a content_uri and creator handle on the peer's own
// host, and an author the peer policy admits. Sometimes it supersedes one of
// the operator's still-pending records. A non-empty reason means the
// binding cannot submit now.
func (e *simEnv) submitContentMsg(b types.BridgeBinding) (*types.MsgSubmitFederatedContent, string) {
	msg := &types.MsgSubmitFederatedContent{Operator: b.Address, PeerId: b.PeerId}
	pol, ok := e.policy(b.PeerId)
	if !ok {
		return msg, "peer has no policy"
	}
	ct, ok := pick(e.r, pol.InboundContentTypes)
	if !ok {
		return msg, "peer policy admits no inbound content type"
	}
	creator := fmt.Sprintf("@%s@%s", randomCreatorName(e.r), b.PeerId)
	if slices.Contains(pol.BlockedIdentities, creator) {
		return msg, "creator identity is blocked"
	}
	if err := e.k.CheckAuthorAdmitted(e.ctx, pol, creator); err != nil {
		// fall back to an author the policy lists by name
		listed := ""
		for _, id := range pol.AllowedIdentities {
			if id != types.AllIdentities && types.CreatorIdentityHostAllowed(b.PeerId, pol.ContentHosts, id) == nil &&
				e.k.CheckAuthorAdmitted(e.ctx, pol, id) == nil && !slices.Contains(pol.BlockedIdentities, id) {
				listed = id
				break
			}
		}
		if listed == "" {
			return msg, "peer policy admits no author the simulation can name"
		}
		creator = listed
	}
	body := randomContentBody(e.r)
	licenses := commontypes.UnencumberedLicenses()
	msg.RemoteContentId = fmt.Sprintf("remote-%d", e.r.Intn(1_000_000))
	msg.ContentType = ct
	msg.CreatorIdentity = creator
	msg.CreatorName = randomCreatorName(e.r)
	msg.Title = randomContentTitle(e.r)
	msg.Body = body
	msg.ContentUri = fmt.Sprintf("https://%s/users/sim/statuses/%d", b.PeerId, e.r.Intn(1_000_000_000))
	msg.RemoteCreatedAt = e.now() - int64(e.r.Intn(86400))
	msg.ContentHash = contentHash(e.r, body)
	msg.License = licenses[e.r.Intn(len(licenses))]
	if e.r.Intn(4) == 0 {
		if prev, ok := e.supersedable(b.Address, b.PeerId); ok {
			msg.Supersedes = &types.ContentRef{ContentId: prev.Id}
			msg.ContentUri = prev.ContentUri
		}
	}
	return msg, ""
}

// supersedable returns one of operator's PENDING_VERIFICATION records on
// peerID with a content_uri and no successor yet.
func (e *simEnv) supersedable(operator, peerID string) (types.FederatedContent, bool) {
	maxURI := e.params.MaxContentUriSize
	found, err := e.contentsWhere(func(c types.FederatedContent) bool {
		return c.SubmittedBy == operator && c.PeerId == peerID && c.ContentUri != "" && c.SupersededBy == 0 &&
			commontypes.Saturate[uint64](len(c.ContentUri)) <= maxURI &&
			c.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION
	})
	if err != nil {
		return types.FederatedContent{}, false
	}
	return pick(e.r, found)
}

// ensurePendingContent returns PENDING_VERIFICATION content accepted by
// keep whose window is open, submitting one first through
// MsgSubmitFederatedContent from a working binding when none exists.
func (e *simEnv) ensurePendingContent(keep func(types.FederatedContent) bool) (types.FederatedContent, bool, error) {
	pending, err := e.contentsWhere(func(c types.FederatedContent) bool { return keep(c) && e.pendingVerifiable(c) })
	if err != nil {
		return types.FederatedContent{}, false, err
	}
	if c, ok := pick(e.r, pending); ok {
		return c, true, nil
	}
	b, ok, err := e.ensureBinding(e.inboundBinding, e.inboundPeer, anyone)
	if err != nil || !ok {
		return types.FederatedContent{}, false, err
	}
	acc, ok := e.account(b.Address)
	if !ok {
		return types.FederatedContent{}, false, nil
	}
	msg, reason := e.submitContentMsg(b)
	if reason != "" {
		return types.FederatedContent{}, false, nil
	}
	msg.Supersedes = nil
	next, err := e.k.ContentSeq.Peek(e.ctx)
	if err != nil {
		return types.FederatedContent{}, false, err
	}
	if ok, err := e.ensure(acc, msg, math.ZeroInt()); err != nil || !ok {
		return types.FederatedContent{}, false, err
	}
	c, found := e.content(next)
	if !found || !keep(c) || !e.pendingVerifiable(c) {
		return types.FederatedContent{}, false, nil
	}
	return c, true, nil
}

// verifyMsg builds a MsgVerifyContent by a bonded verifier for pending
// content: mostly the matching hash (VERIFIED), sometimes a different one
// (DISPUTED, which opens arbitration).
func (e *simEnv) verifyMsg(verifier simtypes.Account, c types.FederatedContent) *types.MsgVerifyContent {
	hash := c.ContentHash
	if e.r.Intn(5) == 0 {
		hash = contentHash(e.r, "disputed")
	}
	return &types.MsgVerifyContent{Creator: verifier.Address.String(), ContentId: c.Id, ContentHash: hash}
}

// ensureVerifiedContent returns VERIFIED content accepted by keep that
// still has a verification record, verifying a pending record first through
// MsgVerifyContent when none exists.
func (e *simEnv) ensureVerifiedContent(keep func(types.FederatedContent, types.VerificationRecord) bool) (types.FederatedContent, types.VerificationRecord, bool, error) {
	accept := func(c types.FederatedContent) bool {
		if c.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED {
			return false
		}
		rec, ok := e.record(c.Id)
		return ok && keep(c, rec)
	}
	verified, err := e.contentsWhere(accept)
	if err != nil {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	if c, ok := pick(e.r, verified); ok {
		rec, _ := e.record(c.Id)
		return c, rec, true, nil
	}
	c, ok, err := e.ensurePendingContent(func(types.FederatedContent) bool { return true })
	if err != nil || !ok {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	vs, err := e.ensureVerifiers(func(a simtypes.Account) bool { return !e.sameMember(a.Address.String(), c.SubmittedBy) })
	if err != nil {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	v, ok := pick(e.r, vs)
	if !ok {
		return types.FederatedContent{}, types.VerificationRecord{}, false, nil
	}
	msg := &types.MsgVerifyContent{Creator: v.Address.String(), ContentId: c.Id, ContentHash: c.ContentHash}
	if ok, err := e.ensure(v, msg, math.ZeroInt()); err != nil || !ok {
		return types.FederatedContent{}, types.VerificationRecord{}, false, err
	}
	c, found := e.content(c.Id)
	if !found || !accept(c) {
		return types.FederatedContent{}, types.VerificationRecord{}, false, nil
	}
	rec, _ := e.record(c.Id)
	return c, rec, true, nil
}

// challengeFee mirrors ChallengeVerification's fee: the base fee doubled for
// each prior rejected challenge (capped at 2^20).
func (e *simEnv) challengeFee(rec types.VerificationRecord) math.Int {
	fee := e.params.ChallengeFeeAmount
	if rec.PriorRejectedChallenges > 0 {
		shifts := min(rec.PriorRejectedChallenges, 20)
		fee = fee.MulRaw(commontypes.Saturate[int64](uint64(1) << shifts))
	}
	return fee
}

// inDispute reports whether c is CHALLENGED or DISPUTED, the statuses the
// arbitration and escalation handlers act on.
func inDispute(c types.FederatedContent) bool {
	return c.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED ||
		c.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED
}

// arbitrationOpen mirrors the keeper's arbitrationOpen: no escalation, no
// stashed quorum verdict, resolution deadline not passed.
func (e *simEnv) arbitrationOpen(rec types.VerificationRecord) bool {
	return !e.escalated(rec.ContentId) &&
		rec.PendingVerifierVerdict == types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED &&
		(rec.ArbiterResolutionDeadline == 0 || e.now() <= rec.ArbiterResolutionDeadline)
}

// linksWhere returns the identity links accepted by keep.
func (e *simEnv) linksWhere(keep func(types.IdentityLink) bool) ([]types.IdentityLink, error) {
	var out []types.IdentityLink
	err := e.k.IdentityLinks.Walk(e.ctx, nil, func(_ collections.Pair[string, string], l types.IdentityLink) (bool, error) {
		if keep(l) {
			out = append(out, l)
		}
		return false, nil
	})
	return out, err
}

// linkCount returns addr's identity link count.
func (e *simEnv) linkCount(addr string) uint32 {
	n, err := e.k.IdentityLinkCount.Get(e.ctx, addr)
	if err != nil {
		return 0
	}
	return n
}

// takesInbound reports whether a peer's policy admits some inbound content
// type (MsgSubmitFederatedContent needs one).
func (e *simEnv) takesInbound(peerID string) bool {
	pol, ok := e.policy(peerID)
	return ok && len(pol.InboundContentTypes) > 0
}

// inboundBinding accepts bindings whose peer admits inbound content.
func (e *simEnv) inboundBinding(b types.BridgeBinding) bool { return e.takesInbound(b.PeerId) }

// inboundPeer accepts peers that admit inbound content.
func (e *simEnv) inboundPeer(p types.Peer) bool { return e.takesInbound(p.Id) }

// anyone accepts every sim account.
func anyone(simtypes.Account) bool { return true }

// --- Random data generators ---

// randomPeerID returns a fresh lowercase host-like peer id.
func randomPeerID(r *rand.Rand) string {
	hosts := []string{"phoenix", "aurora", "zenith", "nebula", "cascade", "ember"}
	return fmt.Sprintf("%s-%d.example", hosts[r.Intn(len(hosts))], r.Intn(1_000_000))
}

// randomPeerType returns a specified peer type. Spark Dream (IBC) peers are
// rarer: nothing in the simulation can use them.
func randomPeerType(r *rand.Rand) types.PeerType {
	pts := []types.PeerType{
		types.PeerType_PEER_TYPE_ACTIVITYPUB,
		types.PeerType_PEER_TYPE_ACTIVITYPUB,
		types.PeerType_PEER_TYPE_ATPROTO,
		types.PeerType_PEER_TYPE_NOSTR,
		types.PeerType_PEER_TYPE_LENS,
		types.PeerType_PEER_TYPE_SPARK_DREAM,
	}
	return pts[r.Intn(len(pts))]
}

// protocolFor names the bridge protocol of a peer type.
func protocolFor(t types.PeerType) string {
	switch t {
	case types.PeerType_PEER_TYPE_ACTIVITYPUB:
		return "activitypub"
	case types.PeerType_PEER_TYPE_ATPROTO:
		return "atproto"
	case types.PeerType_PEER_TYPE_NOSTR:
		return "nostr"
	case types.PeerType_PEER_TYPE_LENS:
		return "lens"
	}
	return "spark-ibc"
}

func randomEndpoint(r *rand.Rand) string {
	endpoints := []string{
		"https://bridge.phoenix.example/api",
		"https://relay.aurora.example/v1",
		"https://node.zenith.example/bridge",
		"https://gateway.nebula.example/fed",
	}
	return endpoints[r.Intn(len(endpoints))]
}

func randomContentTitle(r *rand.Rand) string {
	titles := []string{
		"Federated Post from Remote",
		"Cross-Chain Content Update",
		"Bridge Relay Article",
		"Remote Community Discussion",
		"Federation Test Content",
	}
	return titles[r.Intn(len(titles))]
}

func randomContentBody(r *rand.Rand) string {
	bodies := []string{
		"This is federated content from a remote peer.",
		"Cross-chain article discussing governance.",
		"Bridge-relayed post about community coordination.",
		"Remote content submitted via federation bridge.",
		"Simulation-generated federated content body.",
	}
	return bodies[r.Intn(len(bodies))]
}

func randomCreatorName(r *rand.Rand) string {
	names := []string{"phoenix", "aurora", "zenith", "nebula", "cascade", "ember"}
	return names[r.Intn(len(names))]
}

// contentHash returns a fresh 32-byte hash over seed and random bytes.
func contentHash(r *rand.Rand, seed string) []byte {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", seed, r.Int63(), r.Int63())))
	return sum[:]
}

// randomContentTypeSubset returns a random non-empty subset of known.
func randomContentTypeSubset(r *rand.Rand, known []string) []string {
	if len(known) == 0 {
		return nil
	}
	n := r.Intn(len(known)) + 1
	out := make([]string, 0, n)
	for _, i := range r.Perm(len(known))[:n] {
		out = append(out, known[i])
	}
	return out
}
