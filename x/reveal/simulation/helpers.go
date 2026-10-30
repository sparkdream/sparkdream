package simulation

import (
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

	commontypes "sparkdream/x/common/types"
	commonstypes "sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// The reveal simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions against live state and params, and returns a
// NoOp when one cannot hold. Prerequisite state (a proposed contribution, an
// approved one with a STAKING tranche, a BACKED or REVEALED tranche) is only
// ever created by delivering the real message first.
//
// Approve, Reject and ResolveDispute are signed only by the Commons Council
// policy address or x/gov. Sim accounts are never made a policy, so those
// messages reach their handlers the way the spec routes them: a Commons
// Council member (an Operations Committee member when one sits on the
// council) submits an x/commons proposal carrying the message, council
// members vote it through, and anyone executes it once accepted
// (council.go). Every step is a real x/commons transaction.
//
// The x/rep and x/commons side of a handler (membership, trust level, DREAM
// locks, council seats) is read through those modules' query services, and
// each message is finally dry-run through its real handler on a throwaway
// cache of the block state (submit). Only a message that dry-run accepts is
// delivered. No reveal handler takes SPARK, so only the x/commons proposal
// fee is ever reserved from a signer's balance.

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
		// stream, so how much randomness an op uses (which depends on reveal
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

// input builds the delivery input for msg signed by acc.
func (e *simEnv) input(acc simtypes.Account, msg sdk.Msg, spent sdk.Coins) simulation.OperationInput {
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

// deliver signs msg with acc and delivers it as a real transaction with a
// random fee. No reveal handler takes SPARK, so nothing is reserved.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(e.input(acc, msg, sdk.NewCoins()))
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

// ensure delivers a prerequisite message from acc (dry-run first). It
// returns false when the handler would refuse it.
func (e *simEnv) ensure(acc simtypes.Account, msg sdk.Msg) (bool, error) {
	if e.tombstoned(acc.Address.String()) {
		return false, nil
	}
	if e.dryRun(msg) != nil {
		return false, nil
	}
	opMsg, _, err := e.deliver(acc, msg)
	if err != nil {
		return false, err
	}
	e.members = nil // a delivered tx may have moved DREAM locks
	return opMsg.OK, nil
}

// height is the block height, the clock every reveal deadline uses.
func (e *simEnv) height() int64 {
	return e.ctx.BlockHeight()
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

// canonical resolves addr through x/rep's alias contract (addr itself when
// the query fails).
func (e *simEnv) canonical(addr string) string {
	res, err := e.repQuery.Canonical(e.ctx, &reptypes.QueryCanonicalRequest{Address: addr})
	if err != nil || res.Canonical == "" {
		return addr
	}
	return res.Canonical
}

// sameMember approximates x/rep's SameMember for sim accounts: equal
// addresses, or two addresses that resolve to the same canonical member.
// The dry-run applies the exact alias contract.
func (e *simEnv) sameMember(a, b string) bool {
	if a == b {
		return true
	}
	return e.canonical(a) == e.canonical(b)
}

// stakerLevel is the trust level the spec requires of a staker
// (docs/x-reveal-spec.md, Access Control). The handler only checks active
// membership; the simulation follows the spec.
const stakerLevel = reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL

// --- Reveal state views ---

// trancheRef is a tranche of a contribution.
type trancheRef struct {
	c types.Contribution
	t types.RevealTranche
}

// contributionsWhere returns the contributions accepted by keep.
func (e *simEnv) contributionsWhere(keep func(types.Contribution) bool) ([]types.Contribution, error) {
	var out []types.Contribution
	err := e.k.Contribution.Walk(e.ctx, nil, func(_ uint64, c types.Contribution) (bool, error) {
		if keep(c) {
			out = append(out, c)
		}
		return false, nil
	})
	return out, err
}

// contribution returns a contribution by id.
func (e *simEnv) contribution(id uint64) (types.Contribution, bool) {
	c, err := e.k.Contribution.Get(e.ctx, id)
	return c, err == nil
}

// tranchesIn returns the tranches in status of IN_PROGRESS contributions
// that keep accepts.
func (e *simEnv) tranchesIn(status types.TrancheStatus, keep func(trancheRef) bool) ([]trancheRef, error) {
	var out []trancheRef
	err := e.k.Contribution.Walk(e.ctx, nil, func(_ uint64, c types.Contribution) (bool, error) {
		if c.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
			return false, nil
		}
		for _, t := range c.Tranches {
			ref := trancheRef{c: c, t: t}
			if t.Status == status && keep(ref) {
				out = append(out, ref)
			}
		}
		return false, nil
	})
	return out, err
}

// trancheStakes returns the stakes on a tranche (StakesByTranche index).
func (e *simEnv) trancheStakes(contribID uint64, trancheID uint32) ([]types.RevealStake, error) {
	var out []types.RevealStake
	err := e.k.StakesByTranche.Walk(e.ctx,
		collections.NewPrefixedPairRange[string, uint64](keeper.TrancheKey(contribID, trancheID)),
		func(key collections.Pair[string, uint64]) (bool, error) {
			s, err := e.k.RevealStake.Get(e.ctx, key.K2())
			if err == nil {
				out = append(out, s)
			}
			return false, nil
		})
	return out, err
}

// hasVoted reports whether voter already voted on a tranche (read errors
// count as voted, so the op skips the voter).
func (e *simEnv) hasVoted(contribID uint64, trancheID uint32, voter string) bool {
	has, err := e.k.Vote.Has(e.ctx, keeper.VoteKey(contribID, trancheID, voter))
	return err != nil || has
}

// inCooldown mirrors Propose's cooldown scan: a contribution of the
// contributor whose proposal_eligible_at lies in the future.
func (e *simEnv) inCooldown(contributor string) bool {
	blocked := false
	err := e.k.ContributionsByContributor.Walk(e.ctx,
		collections.NewPrefixedPairRange[string, uint64](contributor),
		func(key collections.Pair[string, uint64]) (bool, error) {
			c, ok := e.contribution(key.K2())
			if ok && c.ProposalEligibleAt > 0 && e.height() < c.ProposalEligibleAt {
				blocked = true
				return true, nil
			}
			return false, nil
		})
	return err != nil || blocked
}

// --- Contribution lifecycle prerequisites ---

// proposeMsg builds a MsgPropose for acc that passes the handler's local
// checks: an active member at min_proposer_trust_level with no proposal
// cooldown, 1-3 tranches whose thresholds are whole multiples of
// min_stake_amount (so stakes can always fill them exactly), an open final
// license, and the bond (bond_rate * total) unlocked. A non-empty reason
// means acc cannot propose now.
func (e *simEnv) proposeMsg(acc simtypes.Account) (*types.MsgPropose, string) {
	addr := acc.Address.String()
	p := e.params
	msg := &types.MsgPropose{Contributor: addr}
	level := reptypes.TrustLevel(commontypes.Saturate[int32](p.MinProposerTrustLevel))
	if !e.trustAtLeast(addr, level) {
		return msg, "not an active member at the proposer trust level"
	}
	if e.inCooldown(addr) {
		return msg, "contributor is in a proposal cooldown"
	}
	unit := p.MinStakeAmount
	if !unit.IsPositive() {
		return msg, "min_stake_amount is not positive"
	}
	maxTranches := min(3, commontypes.Saturate[int](p.MaxTranches))
	if maxTranches < 1 {
		return msg, "max_tranches is zero"
	}
	n := 1 + e.r.Intn(maxTranches)
	total := math.ZeroInt()
	for i := range n {
		threshold := unit.MulRaw(int64(3 + e.r.Intn(6)))
		if threshold.GT(p.MaxTrancheValuation) {
			threshold = p.MaxTrancheValuation
		}
		if !threshold.IsPositive() {
			return msg, "max_tranche_valuation is not positive"
		}
		total = total.Add(threshold)
		msg.Tranches = append(msg.Tranches, types.TrancheDef{
			Name:           randomTrancheName(e.r),
			Description:    fmt.Sprintf("Tranche %d of a simulated contribution", i),
			Components:     randomComponents(e.r),
			StakeThreshold: threshold,
			PreviewUri:     randomURI(e.r, "preview"),
		})
	}
	if total.GT(p.MaxTotalValuation) {
		return msg, "valuation above max_total_valuation"
	}
	bond := p.BondRate.MulInt(total).TruncateInt()
	if !bond.IsPositive() || e.unlockedDream(addr).LT(bond) {
		return msg, "contributor cannot lock the bond"
	}
	msg.ProjectName = randomProjectName(e.r)
	msg.Description = "Simulated progressive open-source contribution"
	msg.TotalValuation = total
	msg.InitialLicense = randomInitialLicense(e.r)
	msg.FinalLicense = randomFinalLicense(e.r)
	return msg, ""
}

// ensureProposed delivers a MsgPropose from a random eligible member and
// returns the new contribution.
func (e *simEnv) ensureProposed() (types.Contribution, bool, error) {
	candidates := accountsWhere(e.accs, func(a simtypes.Account) bool {
		if e.tombstoned(a.Address.String()) {
			return false
		}
		_, reason := e.proposeMsg(a)
		return reason == ""
	})
	acc, ok := pick(e.r, candidates)
	if !ok {
		return types.Contribution{}, false, nil
	}
	msg, reason := e.proposeMsg(acc)
	if reason != "" {
		return types.Contribution{}, false, nil
	}
	ok, err := e.ensure(acc, msg)
	if err != nil || !ok {
		return types.Contribution{}, false, err
	}
	next, err := e.k.ContributionSeq.Peek(e.ctx)
	if err != nil || next == 0 {
		return types.Contribution{}, false, err
	}
	c, found := e.contribution(next - 1)
	return c, found, nil
}

// stakeAmount picks a stake for a tranche with remaining room: a whole
// number (one or two) of min_stake_amount, or the whole remainder when what
// would be left over is below min_stake_amount (it could then never be
// filled). ok is false when the remainder is itself below the minimum.
func (e *simEnv) stakeAmount(remaining math.Int) (math.Int, bool) {
	unit := e.params.MinStakeAmount
	if !unit.IsPositive() || remaining.LT(unit) {
		return math.Int{}, false
	}
	amount := unit.MulRaw(int64(1 + e.r.Intn(2)))
	if amount.GT(remaining) {
		amount = unit
	}
	if rest := remaining.Sub(amount); rest.IsPositive() && rest.LT(unit) {
		amount = remaining
	}
	return amount, true
}

// stakeMsgFor returns a stake on a STAKING tranche from a random eligible
// staker: a live member at the staker trust level, not the contributor,
// with the amount unlocked.
func (e *simEnv) stakeMsgFor(ref trancheRef) (simtypes.Account, *types.MsgStake, bool) {
	amount, ok := e.stakeAmount(ref.t.StakeThreshold.Sub(ref.t.DreamStaked))
	if !ok {
		return simtypes.Account{}, nil, false
	}
	stakers := accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		return !e.tombstoned(addr) && e.trustAtLeast(addr, stakerLevel) &&
			!e.sameMember(addr, ref.c.Contributor) && e.unlockedDream(addr).GTE(amount)
	})
	acc, ok := pick(e.r, stakers)
	if !ok {
		return simtypes.Account{}, nil, false
	}
	return acc, &types.MsgStake{
		Staker:         acc.Address.String(),
		ContributionId: ref.c.Id,
		TrancheId:      ref.t.Id,
		Amount:         amount,
	}, true
}

// stakingTranches returns the STAKING tranches some member could stake on.
func (e *simEnv) stakingTranches() ([]trancheRef, error) {
	return e.tranchesIn(types.TrancheStatus_TRANCHE_STATUS_STAKING, func(ref trancheRef) bool {
		_, _, ok := e.stakeMsgFor(ref)
		return ok
	})
}

// ensureStaking returns the STAKING tranches some member could stake on.
// When there are none it advances the approval of a proposed contribution
// one step through the Commons Council (proposing one first when none is
// waiting), and an approval executed now opens tranche 0 for staking.
func (e *simEnv) ensureStaking() ([]trancheRef, error) {
	refs, err := e.stakingTranches()
	if err != nil || len(refs) > 0 {
		return refs, err
	}
	if _, _, err := e.advanceCouncil(kindApprove); err != nil {
		return nil, err
	}
	return e.stakingTranches()
}

// ensureBacked fills a STAKING tranche (from ensureStaking) by delivering
// stakes from different members until it is BACKED, and returns it.
func (e *simEnv) ensureBacked() (trancheRef, bool, error) {
	refs, err := e.ensureStaking()
	if err != nil {
		return trancheRef{}, false, err
	}
	ref, ok := pick(e.r, refs)
	if !ok {
		return trancheRef{}, false, nil
	}
	for range 12 {
		c, found := e.contribution(ref.c.Id)
		if !found || int(ref.t.Id) >= len(c.Tranches) {
			return trancheRef{}, false, nil
		}
		ref = trancheRef{c: c, t: c.Tranches[ref.t.Id]}
		if ref.t.Status == types.TrancheStatus_TRANCHE_STATUS_BACKED {
			return ref, true, nil
		}
		if ref.t.Status != types.TrancheStatus_TRANCHE_STATUS_STAKING {
			return trancheRef{}, false, nil
		}
		acc, msg, ok := e.stakeMsgFor(ref)
		if !ok {
			return trancheRef{}, false, nil
		}
		if ok, err := e.ensure(acc, msg); err != nil || !ok {
			return trancheRef{}, false, err
		}
	}
	return trancheRef{}, false, nil
}

// revealMsgFor returns the contributor's reveal of a BACKED tranche, when
// the contributor is a live sim account.
func (e *simEnv) revealMsgFor(ref trancheRef) (simtypes.Account, *types.MsgReveal, bool) {
	acc, ok := e.account(ref.c.Contributor)
	if !ok {
		return simtypes.Account{}, nil, false
	}
	return acc, &types.MsgReveal{
		Contributor:    acc.Address.String(),
		ContributionId: ref.c.Id,
		TrancheId:      ref.t.Id,
		CodeUri:        randomURI(e.r, "code"),
		DocsUri:        randomURI(e.r, "docs"),
		CommitHash:     randomCommitHash(e.r),
	}, true
}

// backedTranches returns the BACKED tranches whose contributor can reveal.
func (e *simEnv) backedTranches() ([]trancheRef, error) {
	return e.tranchesIn(types.TrancheStatus_TRANCHE_STATUS_BACKED, func(ref trancheRef) bool {
		_, ok := e.account(ref.c.Contributor)
		return ok
	})
}

// ensureRevealed reveals a BACKED tranche (filling a STAKING one first when
// none is BACKED) and returns it.
func (e *simEnv) ensureRevealed() (trancheRef, bool, error) {
	refs, err := e.backedTranches()
	if err != nil {
		return trancheRef{}, false, err
	}
	ref, ok := pick(e.r, refs)
	if !ok {
		if ref, ok, err = e.ensureBacked(); err != nil || !ok {
			return trancheRef{}, false, err
		}
	}
	acc, msg, ok := e.revealMsgFor(ref)
	if !ok {
		return trancheRef{}, false, nil
	}
	if ok, err := e.ensure(acc, msg); err != nil || !ok {
		return trancheRef{}, false, err
	}
	c, found := e.contribution(ref.c.Id)
	if !found || int(ref.t.Id) >= len(c.Tranches) {
		return trancheRef{}, false, nil
	}
	return trancheRef{c: c, t: c.Tranches[ref.t.Id]}, true, nil
}

// --- Random data generators ---

// randomProjectName generates a random project name.
func randomProjectName(r *rand.Rand) string {
	names := []string{
		"aurora-engine", "zenith-core", "phoenix-sdk", "nebula-chain",
		"prism-net", "vortex-io", "cascade-db", "ember-node",
		"atlas-bridge", "horizon-relay", "pulse-api", "forge-tools",
	}
	return names[r.Intn(len(names))]
}

// randomTrancheName generates a random tranche name.
func randomTrancheName(r *rand.Rand) string {
	names := []string{
		"core-module", "api-layer", "storage-engine", "auth-service",
		"indexer", "cli-tools", "documentation", "test-suite",
		"migration-scripts", "monitoring-hooks", "plugin-system", "sdk-bindings",
	}
	return names[r.Intn(len(names))]
}

// randomInitialLicense returns the terms while tranches are being revealed
// (free text, the contributor's choice).
func randomInitialLicense(r *rand.Rand) string {
	licenses := []string{"Proprietary", "BUSL-1.1", "MIT", "Apache-2.0", "All rights reserved"}
	return licenses[r.Intn(len(licenses))]
}

// randomFinalLicense returns an open license revealed code may end under
// (x/common IsOpenCodeLicense).
func randomFinalLicense(r *rand.Rand) string {
	licenses := []string{commontypes.LicenseCC0, "Unlicense", "0BSD", "MIT", "Apache-2.0", "BSD-3-Clause"}
	return licenses[r.Intn(len(licenses))]
}

// randomComponents returns a random list of component names.
func randomComponents(r *rand.Rand) []string {
	pool := []string{
		"keeper", "types", "cli", "genesis", "module", "simulation",
		"ante", "middleware", "query", "msg_server",
	}
	n := r.Intn(3) + 1
	components := make([]string, n)
	for i := range n {
		components[i] = pool[r.Intn(len(pool))]
	}
	return components
}

// randomURI generates a random URI.
func randomURI(r *rand.Rand, prefix string) string {
	return fmt.Sprintf("ipfs://Qm%s/%s", simtypes.RandStringOfLength(r, 40), prefix)
}

// randomCommitHash generates a random git commit hash.
func randomCommitHash(r *rand.Rand) string {
	const hex = "0123456789abcdef"
	hash := make([]byte, 40)
	for i := range hash {
		hash[i] = hex[r.Intn(len(hex))]
	}
	return string(hash)
}

// randomDisputeVerdict returns a random non-unspecified dispute verdict.
func randomDisputeVerdict(r *rand.Rand) types.DisputeVerdict {
	verdicts := []types.DisputeVerdict{
		types.DisputeVerdict_DISPUTE_VERDICT_ACCEPT,
		types.DisputeVerdict_DISPUTE_VERDICT_IMPROVE,
		types.DisputeVerdict_DISPUTE_VERDICT_REJECT,
	}
	return verdicts[r.Intn(len(verdicts))]
}

// randomReason returns a short council or contributor reason.
func randomReason(r *rand.Rand) string {
	reasons := []string{
		"Scope no longer matches the preview",
		"Delivered code needs another pass on the storage layer",
		"Council reviewed the revealed tranche",
		"Contributor withdrew the reveal plan",
		"Duplicate of an existing community project",
	}
	return reasons[r.Intn(len(reasons))]
}
