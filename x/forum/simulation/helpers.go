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
	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// The forum simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions against live state and params, and returns a
// NoOp when one cannot hold. Prerequisite state (a thread to reply to, a
// bonded sentinel, a bounty to award) is only ever created by delivering the
// real message first.
//
// The x/rep and x/commons side of a handler (membership, trust level,
// bonded-role eligibility and reservations, the tag registry, categories,
// appeal bonds) is read through those modules' query services, and because
// the forum keeper does not expose every check one by one, each message is
// finally dry-run through its real handler on a throwaway cache of the block
// state (submit). Only a message that dry-run accepts is delivered.

// opsCommitteeName is the commons group whose members (with gov and the
// council/committee policies) pass the forum's isCouncilAuthorized gate.
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
	members      map[string]reptypes.Member
	tags         []string
	categories   []commonstypes.Category
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
		// stream, so how much randomness an op uses (which depends on forum
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

// deliver signs msg with acc and delivers it as a real transaction. spent is
// the SPARK the handler takes from acc (fees, burns, escrows, bonds); the
// random tx fee is drawn only from what is left, so the handler still finds
// it.
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

// now is the block time in unix seconds, the clock every forum handler uses.
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

// --- Forum state views ---

// postsWhere returns the posts accepted by keep.
func (e *simEnv) postsWhere(keep func(types.Post) bool) ([]types.Post, error) {
	var out []types.Post
	err := e.k.Post.Walk(e.ctx, nil, func(_ uint64, p types.Post) (bool, error) {
		if keep(p) {
			out = append(out, p)
		}
		return false, nil
	})
	return out, err
}

// post returns a post by id.
func (e *simEnv) post(id uint64) (types.Post, bool) {
	p, err := e.k.Post.Get(e.ctx, id)
	return p, err == nil
}

// rootID returns the id of a post's thread root.
func rootID(p types.Post) uint64 {
	if p.ParentId == 0 {
		return p.PostId
	}
	return p.RootId
}

// visible reports whether a post is neither hidden, deleted nor archived:
// the status every reaction, flag, edit and moderation handler requires.
func visible(p types.Post) bool {
	switch p.Status {
	case types.PostStatus_POST_STATUS_HIDDEN, types.PostStatus_POST_STATUS_DELETED, types.PostStatus_POST_STATUS_ARCHIVED:
		return false
	}
	return true
}

// activeRoot reports whether p is an ACTIVE thread root.
func activeRoot(p types.Post) bool {
	return p.ParentId == 0 && p.Status == types.PostStatus_POST_STATUS_ACTIVE
}

// threadReplies returns the replies of a thread (PostsByRoot index).
func (e *simEnv) threadReplies(root uint64) ([]types.Post, error) {
	var out []types.Post
	err := e.k.PostsByRoot.Walk(e.ctx, collections.NewPrefixedPairRange[uint64, uint64](root),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			if p, ok := e.post(key.K2()); ok {
				out = append(out, p)
			}
			return false, nil
		})
	return out, err
}

// metadata returns a thread's metadata (zero value when absent).
func (e *simEnv) metadata(threadID uint64) (types.ThreadMetadata, bool) {
	m, err := e.k.ThreadMetadata.Get(e.ctx, threadID)
	return m, err == nil
}

// hasVoted reports whether addr already reacted to the post.
func (e *simEnv) hasVoted(postID uint64, addr string) bool {
	has, err := e.k.PostVote.Has(e.ctx, collections.Join(postID, addr))
	return err != nil || has
}

// openBounty returns the open bounty of a thread, if any.
func (e *simEnv) openBounty(threadID uint64) (types.Bounty, bool) {
	id, err := e.k.ActiveBountyByThread.Get(e.ctx, threadID)
	if err != nil {
		return types.Bounty{}, false
	}
	b, err := e.k.Bounty.Get(e.ctx, id)
	if err != nil || !b.IsOpen() {
		return types.Bounty{}, false
	}
	return b, true
}

// bountiesWhere returns the bounties accepted by keep.
func (e *simEnv) bountiesWhere(keep func(types.Bounty) bool) ([]types.Bounty, error) {
	var out []types.Bounty
	err := e.k.Bounty.Walk(e.ctx, nil, func(_ uint64, b types.Bounty) (bool, error) {
		if keep(b) {
			out = append(out, b)
		}
		return false, nil
	})
	return out, err
}

// --- x/commons views ---

// liveCategories returns the commons categories, read once per operation.
func (e *simEnv) liveCategories() []commonstypes.Category {
	if e.categories != nil {
		return e.categories
	}
	e.categories = []commonstypes.Category{}
	var next []byte
	for {
		res, err := e.commonsQuery.ListCategory(e.ctx, &commonstypes.QueryAllCategoryRequest{
			Pagination: &query.PageRequest{Key: next, Limit: 200},
		})
		if err != nil {
			return e.categories
		}
		e.categories = append(e.categories, res.Category...)
		if res.Pagination == nil || len(res.Pagination.NextKey) == 0 {
			return e.categories
		}
		next = res.Pagination.NextKey
	}
}

// category returns a commons category by id.
func (e *simEnv) category(id uint64) (commonstypes.Category, bool) {
	for _, c := range e.liveCategories() {
		if c.CategoryId == id {
			return c, true
		}
	}
	return commonstypes.Category{}, false
}

// canWrite mirrors CreatePost's category write gate for a non-council
// signer: no admin-only categories, members-only needs an active member.
func (e *simEnv) canWrite(c commonstypes.Category, addr string) bool {
	return !c.AdminOnlyWrite && (!c.MembersOnlyWrite || e.isActiveMember(addr))
}

// opsCommittee returns the sim accounts that are members of the Commons
// Operations Committee, which pass the forum's council gate directly. Sim
// accounts are never made a group or committee policy.
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

// isActiveMember mirrors the forum's IsMember for a plain address.
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

// bondedRole returns addr's bonded role record.
func (e *simEnv) bondedRole(role reptypes.RoleType, addr string) (reptypes.BondedRole, bool) {
	res, err := e.repQuery.BondedRole(e.ctx, &reptypes.QueryBondedRoleRequest{RoleType: role, Address: addr})
	if err != nil {
		return reptypes.BondedRole{}, false
	}
	return res.BondedRole, true
}

// sentinels returns the sim accounts holding CONTENT_SENTINEL in a status
// that can act (the handlers' EligibleForRole is dry-run).
func (e *simEnv) sentinels() []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		br, ok := e.bondedRole(reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, addr)
		return ok && br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED &&
			e.isActiveMember(addr) && !e.tombstoned(addr)
	})
}

// ensureSentinels returns the sim accounts holding CONTENT_SENTINEL, bonding
// one first through x/rep's MsgBondRole when none does: an active member at
// the role's trust level with min_bond unlocked DREAM bonds exactly
// min_bond (the role config is a projection of the forum sentinel params).
func (e *simEnv) ensureSentinels() ([]simtypes.Account, error) {
	role := reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL
	if holders := e.sentinels(); len(holders) > 0 {
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
	candidates := accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		if _, has := e.bondedRole(role, addr); has {
			return false
		}
		return e.trustAtLeast(addr, level) && e.unlockedDream(addr).GTE(minBond)
	})
	acc, ok := pick(e.r, candidates)
	if !ok {
		return nil, nil
	}
	msg := &reptypes.MsgBondRole{Creator: acc.Address.String(), RoleType: role, Amount: minBond.String()}
	if ok, err := e.ensure(acc, msg, math.ZeroInt()); err != nil || !ok {
		return nil, err
	}
	return e.sentinels(), nil
}

// sentinelActionsLeft reports whether a sentinel is under the per-epoch cap
// for an action kind and outside its overturn cooldown.
func (e *simEnv) sentinelActionsLeft(addr, kind string, limit uint64) bool {
	res, err := e.repQuery.RoleActivity(e.ctx, &reptypes.QueryRoleActivityRequest{
		RoleType: reptypes.RoleType_ROLE_TYPE_CONTENT_SENTINEL, Address: addr,
	})
	if err != nil {
		return true // no activity record yet
	}
	act := res.RoleActivity
	if act.OverturnCooldownUntil > e.now() {
		return false
	}
	return act.EpochActions[kind] < limit
}

// liveTags returns the x/rep registry tags a post may carry: existing, not
// reserved, well-formed and within the forum's tag length cap.
func (e *simEnv) liveTags() []string {
	if e.tags != nil {
		return e.tags
	}
	e.tags = []string{}
	res, err := e.repQuery.ListTag(e.ctx, &reptypes.QueryAllTagRequest{Pagination: &query.PageRequest{Limit: 100}})
	if err != nil {
		return e.tags
	}
	for _, t := range res.Tag {
		if uint64(len(t.Name)) > types.DefaultMaxTagLength || !commontypes.ValidateTagFormat(t.Name) {
			continue
		}
		if _, err := e.repQuery.GetReservedTag(e.ctx, &reptypes.QueryGetReservedTagRequest{Name: t.Name}); err == nil {
			continue
		}
		e.tags = append(e.tags, t.Name)
	}
	return e.tags
}

// randomTags returns up to two distinct live tags.
func (e *simEnv) randomTags() []string {
	tags := e.liveTags()
	n := e.r.Intn(3)
	if n > len(tags) {
		n = len(tags)
	}
	if commontypes.Saturate[uint64](n) > types.DefaultMaxTagsPerPost {
		n = commontypes.Saturate[int](types.DefaultMaxTagsPerPost)
	}
	out := make([]string, 0, n)
	for _, i := range e.r.Perm(len(tags))[:n] {
		out = append(out, tags[i])
	}
	return out
}

// appealBond is the SPARK bond x/rep's CreateGovActionAppeal escrows from
// the appellant of a hide, lock, move or pin.
func appealBond() math.Int {
	return math.NewInt(reptypes.DefaultAppealBondAmount)
}

// --- Posts ---

// replyable reports whether a post can take a reply under CreatePost's
// status, lock and depth gates (its root resolved from state).
func (e *simEnv) replyable(parent types.Post) bool {
	if !visible(parent) {
		return false
	}
	root := parent
	if parent.ParentId != 0 {
		r, ok := e.post(parent.RootId)
		if !ok {
			return false
		}
		root = r
	}
	if !visible(root) || root.Locked {
		return false
	}
	maxDepth := e.params.MaxReplyDepth
	if maxDepth == 0 {
		maxDepth = types.DefaultMaxReplyDepth
	}
	return parent.Depth+1 <= uint64(maxDepth)
}

// postOpts narrows the post newPostMsg builds.
type postOpts struct {
	parent *types.Post // reply to this post; nil for a new thread
	noTags bool
}

// newPostMsg builds a MsgCreatePost for acc that passes the handler's local
// checks, with the SPARK it costs (storage fee burned, plus the spam tax for
// a non-member). A non-empty reason means acc cannot post it now.
func (e *simEnv) newPostMsg(acc simtypes.Account, opts postOpts) (*types.MsgCreatePost, math.Int, string) {
	addr := acc.Address.String()
	p := e.params
	msg := &types.MsgCreatePost{
		Creator:     addr,
		Content:     randomContent(e.r),
		ContentType: commontypes.ContentType_CONTENT_TYPE_TEXT,
	}
	if p.ForumPaused {
		return msg, math.ZeroInt(), "forum is paused"
	}
	if opts.parent != nil {
		root := *opts.parent
		if root.ParentId != 0 {
			r, ok := e.post(root.RootId)
			if !ok {
				return msg, math.ZeroInt(), "parent has no root"
			}
			root = r
		}
		c, ok := e.category(root.CategoryId)
		if !ok || !e.canWrite(c, addr) {
			return msg, math.ZeroInt(), "thread category is gone or not writable"
		}
		msg.ParentId = opts.parent.PostId
	} else {
		cats := make([]commonstypes.Category, 0)
		for _, c := range e.liveCategories() {
			if e.canWrite(c, addr) {
				cats = append(cats, c)
			}
		}
		c, ok := pick(e.r, cats)
		if !ok {
			return msg, math.ZeroInt(), "no category the signer may write to"
		}
		msg.CategoryId = c.CategoryId
	}
	if !opts.noTags {
		msg.Tags = e.randomTags()
	}
	spent := math.ZeroInt()
	if !p.CostPerByteExempt && p.CostPerByteAmount.IsPositive() {
		spent = spent.Add(p.CostPerByteAmount.MulRaw(int64(len(msg.Content))))
	}
	if !e.isActiveMember(addr) && p.SpamTaxAmount.IsPositive() {
		spent = spent.Add(p.SpamTaxAmount)
	}
	return msg, spent, ""
}

// canPost reports whether a can write a new thread (parent nil) or a reply
// to parent under the category gate: some writable category for a thread,
// the thread's own category for a reply.
func (e *simEnv) canPost(a simtypes.Account, parent *types.Post) bool {
	addr := a.Address.String()
	if e.tombstoned(addr) {
		return false
	}
	if parent == nil {
		for _, c := range e.liveCategories() {
			if e.canWrite(c, addr) {
				return true
			}
		}
		return false
	}
	root := *parent
	if root.ParentId != 0 {
		r, ok := e.post(root.RootId)
		if !ok {
			return false
		}
		root = r
	}
	c, ok := e.category(root.CategoryId)
	return ok && e.canWrite(c, addr)
}

// ensureThread creates a thread by delivering MsgCreatePost from a random
// account accepted by keep and returns it. ok is false when none could.
func (e *simEnv) ensureThread(keep func(simtypes.Account) bool) (types.Post, bool, error) {
	acc, ok := pick(e.r, accountsWhere(e.accs, func(a simtypes.Account) bool {
		return keep(a) && e.canPost(a, nil)
	}))
	if !ok {
		return types.Post{}, false, nil
	}
	msg, spent, reason := e.newPostMsg(acc, postOpts{})
	if reason != "" {
		return types.Post{}, false, nil
	}
	return e.createAndFetch(acc, msg, spent)
}

// ensureReply creates a reply to parent by delivering MsgCreatePost from a
// random account accepted by keep and returns it.
func (e *simEnv) ensureReply(parent types.Post, keep func(simtypes.Account) bool) (types.Post, bool, error) {
	if !e.replyable(parent) {
		return types.Post{}, false, nil
	}
	acc, ok := pick(e.r, accountsWhere(e.accs, func(a simtypes.Account) bool {
		return keep(a) && e.canPost(a, &parent)
	}))
	if !ok {
		return types.Post{}, false, nil
	}
	msg, spent, reason := e.newPostMsg(acc, postOpts{parent: &parent})
	if reason != "" {
		return types.Post{}, false, nil
	}
	return e.createAndFetch(acc, msg, spent)
}

// createAndFetch delivers a MsgCreatePost and returns the post it created
// (the post sequence's last id).
func (e *simEnv) createAndFetch(acc simtypes.Account, msg *types.MsgCreatePost, spent math.Int) (types.Post, bool, error) {
	ok, err := e.ensure(acc, msg, spent)
	if err != nil || !ok {
		return types.Post{}, false, err
	}
	next, err := e.k.PostSeq.Peek(e.ctx)
	if err != nil || next == 0 {
		return types.Post{}, false, err
	}
	p, found := e.post(next - 1)
	return p, found, nil
}

// member reports whether a sim account is an active x/rep member.
func (e *simEnv) member(a simtypes.Account) bool {
	return e.isActiveMember(a.Address.String())
}

// anyone accepts every sim account.
func anyone(simtypes.Account) bool { return true }

// notAddr accepts every sim account but addr.
func notAddr(addr string) func(simtypes.Account) bool {
	return func(a simtypes.Account) bool { return a.Address.String() != addr }
}

// --- Random data generators ---

// randomContent generates random post content
func randomContent(r *rand.Rand) string {
	contents := []string{
		"This is a simulation generated post content.",
		"Testing the forum module with random content.",
		"Phoenix aurora zenith: a short note from the simulation.",
		"This post was created during simulation testing.",
		"Sample content for forum simulation.",
	}
	return contents[r.Intn(len(contents))]
}

// randomReason generates a random reason string
func randomReason(r *rand.Rand) string {
	reasons := []string{
		"Spam content",
		"Inappropriate content",
		"Off-topic",
		"Low quality",
		"Violation of rules",
	}
	return reasons[r.Intn(len(reasons))]
}

// randomModerationReason returns a valid, specified moderation reason.
func randomModerationReason(r *rand.Rand) commontypes.ModerationReason {
	reasons := []commontypes.ModerationReason{
		commontypes.ModerationReason_MODERATION_REASON_SPAM,
		commontypes.ModerationReason_MODERATION_REASON_HARASSMENT,
		commontypes.ModerationReason_MODERATION_REASON_MISINFORMATION,
		commontypes.ModerationReason_MODERATION_REASON_INAPPROPRIATE,
		commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION,
		commontypes.ModerationReason_MODERATION_REASON_OTHER,
	}
	return reasons[r.Intn(len(reasons))]
}
