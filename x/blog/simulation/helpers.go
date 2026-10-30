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

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
	commonstypes "sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
)

// The blog simulation delivers real transactions through the message
// handlers, so every record it leaves behind went through the write path
// (media labels, body hash, fee high-water mark, indexes). A delivered tx the
// handler rejects fails the whole simulation, so every op picks its signer
// and target from live state, checks the handler's preconditions against
// live state and params, and returns a NoOp when one cannot hold.
// Prerequisite state (a post to reply to, a reply to hide, a permanent post
// to pin) is only ever created by delivering the real message first.
//
// The x/rep side of a handler (membership, trust level, the alias contract,
// the tag registry, DREAM author bonds) is read through x/rep's query
// service, and private blog state the keeper does not expose (per-day rate
// limit counters) is covered by the final gate: each message is dry-run
// through its real handler on a throwaway cache of the block state, and only
// a message the dry-run accepts is delivered.
//
// Blog moderation is author-only (hide/unhide belong to the post author,
// pin/unpin to any member at pin_min_trust_level), so no op needs a
// sentinel or a committee seat. Anonymous (x/shield) posting is not
// simulated here: it needs a ZK proof the simulator cannot build.

// maxPrereqTries bounds how many candidate signers a prerequisite delivery
// dry-runs before giving up (per-day rate limits refuse many of them).
const maxPrereqTries = 8

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
	denom  string

	repQuery     reptypes.QueryClient
	blogQuery    types.QueryClient
	commonsQuery commonstypes.QueryClient
	members      map[string]reptypes.Member
	tags         []string
	tombstones   map[string]bool
	canonicals   map[string]string
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
		// stream, so how much randomness an op uses (which depends on blog
		// state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{
			r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k,
			params: params, denom: k.BondDenom(ctx),
		}
		helper := &baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx}
		e.repQuery = reptypes.NewQueryClient(helper)
		e.blogQuery = types.NewQueryClient(helper)
		e.commonsQuery = commonstypes.NewQueryClient(helper)
		return body(e)
	}
}

// noOp returns a skipped operation for msg's type.
func noOp(msg sdk.Msg, reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), reason), nil, nil
}

// noOpErr returns a skipped operation together with an error, which fails
// the simulation: a failing store read or a refused prerequisite delivery
// is a real bug.
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
// the SPARK the handler takes from acc (storage, media scan and reaction
// fees); the random tx fee is drawn only from what is left, so the handler
// still finds it.
func (e *simEnv) deliver(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simulation.GenAndDeliverTxWithRandFees(e.input(acc, msg, spent))
}

// input is the simulation.OperationInput for msg signed by acc, reserving
// spent SPARK for the handler.
func (e *simEnv) input(acc simtypes.Account, msg sdk.Msg, spent math.Int) simulation.OperationInput {
	coins := sdk.NewCoins()
	if spent.IsPositive() {
		coins = sdk.NewCoins(sdk.NewCoin(e.denom, spent))
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

// invalidate drops the per-operation x/rep caches after a delivered tx.
func (e *simEnv) invalidate() {
	e.members = nil
}

// gate reports why acc cannot send msg now ("" when it can): a tombstoned
// signer (the ante handler refuses it), SPARK short of spent, or a handler
// that rejects msg on a dry-run.
func (e *simEnv) gate(acc simtypes.Account, msg sdk.Msg, spent math.Int) string {
	if e.tombstoned(acc.Address.String()) {
		return "signer is a tombstoned address"
	}
	if spent.IsPositive() && e.spendable(acc).LT(spent) {
		return "signer cannot pay " + spent.String() + e.denom
	}
	if err := e.dryRun(msg); err != nil {
		return "handler would reject: " + err.Error()
	}
	return ""
}

// submit dry-runs msg and delivers it when the handler accepts it.
func (e *simEnv) submit(acc simtypes.Account, msg sdk.Msg, spent math.Int) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if reason := e.gate(acc, msg, spent); reason != "" {
		return noOp(msg, reason)
	}
	return e.deliver(acc, msg, spent)
}

// ensure delivers a prerequisite message from acc (dry-run first). It
// returns false when the handler would refuse it. A prerequisite the
// dry-run accepted but delivery rejected is returned as an error.
func (e *simEnv) ensure(acc simtypes.Account, msg sdk.Msg, spent math.Int) (bool, error) {
	if e.gate(acc, msg, spent) != "" {
		return false, nil
	}
	opMsg, _, err := e.deliver(acc, msg, spent)
	if err != nil {
		return false, err
	}
	e.invalidate() // an author bond moved DREAM
	return opMsg.OK, nil
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.denom)
}

// now is the block time in unix seconds, the clock every blog handler uses.
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

// live returns the sim accounts that are not tombstoned and are accepted by
// keep.
func (e *simEnv) live(keep func(simtypes.Account) bool) []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool {
		return !e.tombstoned(a.Address.String()) && keep(a)
	})
}

// account returns the live (not tombstoned) sim account for addr.
func (e *simEnv) account(addr string) (simtypes.Account, bool) {
	acc, ok := getAccountForAddress(addr, e.accs)
	if !ok || e.tombstoned(addr) {
		return simtypes.Account{}, false
	}
	return acc, true
}

// anyone accepts every sim account.
func anyone(simtypes.Account) bool { return true }

// owner returns the live sim account that may act for a stored author
// address under the alias contract (rep CanActAs): the address's current
// canonical identity, never a tombstone.
func (e *simEnv) owner(stored string) (simtypes.Account, bool) {
	return e.account(e.canonical(stored))
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

// isActiveMember mirrors the blog keeper's isActiveMember for a plain
// (non-shield) address.
func (e *simEnv) isActiveMember(addr string) bool {
	m, ok := e.loadMembers()[addr]
	return ok && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE
}

// trustLevel returns an active member's trust level.
func (e *simEnv) trustLevel(addr string) (int32, bool) {
	m, ok := e.loadMembers()[addr]
	if !ok || m.Status != reptypes.MemberStatus_MEMBER_STATUS_ACTIVE {
		return 0, false
	}
	return int32(m.TrustLevel), true
}

// meetsTrust mirrors the keeper's meetsReplyTrustLevel: -1 is open to all,
// 0 any active member, higher values a trust level at or above minLevel.
func (e *simEnv) meetsTrust(addr string, minLevel int32) bool {
	if minLevel == -1 {
		return true
	}
	tl, ok := e.trustLevel(addr)
	if !ok {
		return false
	}
	return minLevel <= 0 || tl >= minLevel
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

// resolve reads addr's alias record once per operation.
func (e *simEnv) resolve(addr string) {
	if _, ok := e.tombstones[addr]; ok {
		return
	}
	if e.tombstones == nil {
		e.tombstones = map[string]bool{}
		e.canonicals = map[string]string{}
	}
	res, err := e.repQuery.Canonical(e.ctx, &reptypes.QueryCanonicalRequest{Address: addr})
	if err != nil || res.Canonical == "" {
		e.tombstones[addr], e.canonicals[addr] = false, addr
		return
	}
	e.tombstones[addr], e.canonicals[addr] = res.Tombstoned, res.Canonical
}

// tombstoned reports whether addr migrated away (x/rep refuses most
// messages from it at the ante handler, and CanActAs never passes it).
func (e *simEnv) tombstoned(addr string) bool {
	e.resolve(addr)
	return e.tombstones[addr]
}

// canonical returns addr's current identity address.
func (e *simEnv) canonical(addr string) string {
	e.resolve(addr)
	return e.canonicals[addr]
}

// liveTags returns the x/rep registry tags a post may carry: existing, not
// reserved, well-formed and within the blog's tag length cap.
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
		if len(t.Name) > commontypes.Saturate[int](e.params.MaxTagLength) || !commontypes.ValidateTagFormat(t.Name) {
			continue
		}
		if _, err := e.repQuery.GetReservedTag(e.ctx, &reptypes.QueryGetReservedTagRequest{Name: t.Name}); err == nil {
			continue
		}
		e.tags = append(e.tags, t.Name)
	}
	return e.tags
}

// randomTags returns up to two distinct live tags, within max_tags_per_post.
func (e *simEnv) randomTags() []string {
	tags := e.liveTags()
	n := e.r.Intn(3)
	n = min(n, len(tags), commontypes.Saturate[int](e.params.MaxTagsPerPost))
	out := make([]string, 0, n)
	for _, i := range e.r.Perm(len(tags))[:n] {
		out = append(out, tags[i])
	}
	return out
}

// --- Blog state views ---

// postsWhere returns the posts accepted by keep. Post ids start at 1.
func (e *simEnv) postsWhere(keep func(types.Post) bool) []types.Post {
	var out []types.Post
	count := e.k.GetPostCount(e.ctx)
	for id := uint64(1); id < count; id++ {
		if p, found := e.k.GetPost(e.ctx, id); found && keep(p) {
			out = append(out, p)
		}
	}
	return out
}

// repliesWhere returns the replies accepted by keep. Reply ids start at 1.
func (e *simEnv) repliesWhere(keep func(types.Reply) bool) []types.Reply {
	var out []types.Reply
	count := e.k.GetReplyCount(e.ctx)
	for id := uint64(1); id < count; id++ {
		if rp, found := e.k.GetReply(e.ctx, id); found && keep(rp) {
			out = append(out, rp)
		}
	}
	return out
}

// activePost reports whether a post is ACTIVE (neither hidden nor deleted).
func activePost(p types.Post) bool { return p.Status == types.PostStatus_POST_STATUS_ACTIVE }

// activeReply reports whether a reply is ACTIVE.
func activeReply(rp types.Reply) bool { return rp.Status == types.ReplyStatus_REPLY_STATUS_ACTIVE }

// unexpired reports whether ephemeral content (expiresAt > 0) is still
// before its expiry; the EndBlocker tombstones it at expiresAt.
func (e *simEnv) unexpired(expiresAt int64) bool {
	return expiresAt == 0 || expiresAt > e.now()
}

// canReply mirrors CreateReply's trust gate: the post's author (by alias)
// always passes, anyone else must meet min_reply_trust_level. Reactions
// share the same gate.
func (e *simEnv) canReply(addr string, post types.Post) bool {
	if e.canonical(post.Creator) == addr && !e.tombstoned(addr) {
		return true
	}
	return e.meetsTrust(addr, post.MinReplyTrustLevel)
}

// replyable reports whether a post takes replies (active, replies enabled,
// not about to expire).
func (e *simEnv) replyable(p types.Post) bool {
	return activePost(p) && p.RepliesEnabled && e.unexpired(p.ExpiresAt)
}

// nestable reports whether a reply can take a nested reply under
// max_reply_depth.
func (e *simEnv) nestable(rp types.Reply) bool {
	return activeReply(rp) && e.unexpired(rp.ExpiresAt) && rp.Depth+1 <= e.params.MaxReplyDepth
}

// --- Message builders ---

// storageFee is cost_per_byte_amount times n bytes (zero when exempt).
func (e *simEnv) storageFee(n int) math.Int {
	if e.params.CostPerByteExempt || !e.params.CostPerByteAmount.IsPositive() {
		return math.ZeroInt()
	}
	return e.params.CostPerByteAmount.MulRaw(int64(n))
}

// scanFee is the media_scan_fee a media-labelled write burns.
func (e *simEnv) scanFee(contentType commontypes.ContentType, body string) math.Int {
	if commontypes.LabelBody(contentType, body).Flags == 0 {
		return math.ZeroInt()
	}
	if e.params.MediaScanFee.IsNil() || !e.params.MediaScanFee.IsPositive() {
		return math.ZeroInt()
	}
	return e.params.MediaScanFee
}

// randomContent returns a content type and body for addr. Plain text most
// of the time; one write in five carries media (an off-chain reference or an
// inline data URI) when addr passes the media gate, so the chain's media
// labels and the scan fee are exercised. bond is the DREAM author bond a
// create attaches when the gate needs it (or, now and then, voluntarily).
func (e *simEnv) randomContent(addr string, maxLen uint64, create bool) (commontypes.ContentType, string, *math.Int) {
	textTypes := []commontypes.ContentType{
		commontypes.ContentType_CONTENT_TYPE_TEXT,
		commontypes.ContentType_CONTENT_TYPE_MARKDOWN,
	}
	ct, _ := pick(e.r, textTypes)
	body := truncate(randomBody(e.r), maxLen)
	var bond *math.Int
	unlocked := e.unlockedDream(addr)
	if create && e.r.Intn(6) == 0 {
		// A small voluntary author bond (1-5 DREAM) within the rep cap.
		amt := math.NewInt(int64(1+e.r.Intn(5)) * 1_000_000)
		if unlocked.GTE(amt) {
			bond = &amt
		}
	}
	if e.r.Intn(5) != 0 || !e.isActiveMember(addr) {
		return ct, body, bond
	}
	tl, _ := e.trustLevel(addr)
	if commontypes.Saturate[uint32](tl) < e.params.MediaMinTrustLevel {
		// Below the media trust bar: only a create with a large enough
		// author bond may carry media.
		minBond := e.params.MediaAuthorBondMin
		if !create || minBond.IsNil() || !minBond.IsPositive() || unlocked.LT(minBond) {
			return ct, body, bond
		}
		bond = &minBond
	}
	if e.r.Intn(2) == 0 {
		return commontypes.ContentType_CONTENT_TYPE_IPFS, truncate("ipfs://bafyphoenixaurorazenith"+fmt.Sprint(e.r.Intn(1000)), maxLen), bond
	}
	return commontypes.ContentType_CONTENT_TYPE_MARKDOWN, truncate("![zenith](data:image/png;base64,iVBORw0KGgo=)", maxLen), bond
}

// newPostMsg builds a MsgCreatePost for acc that passes the handler's local
// checks, with the SPARK it burns (storage fee plus media scan fee).
func (e *simEnv) newPostMsg(acc simtypes.Account) (*types.MsgCreatePost, math.Int) {
	addr := acc.Address.String()
	ct, body, bond := e.randomContent(addr, e.params.MaxBodyLength, true)
	levels := []int32{-1, 0, 0, 0, 1, 2}
	lvl, _ := pick(e.r, levels)
	msg := &types.MsgCreatePost{
		Creator:            addr,
		Title:              truncate(randomTitle(e.r), e.params.MaxTitleLength),
		Body:               body,
		ContentType:        ct,
		MinReplyTrustLevel: lvl,
		AuthorBond:         bond,
		Tags:               e.randomTags(),
	}
	spent := e.storageFee(len(msg.Title) + len(msg.Body)).Add(e.scanFee(ct, body))
	return msg, spent
}

// newReplyMsg builds a MsgCreateReply from acc on post (nested under parent
// when non-nil), with the SPARK it burns.
func (e *simEnv) newReplyMsg(acc simtypes.Account, post types.Post, parent *types.Reply) (*types.MsgCreateReply, math.Int) {
	addr := acc.Address.String()
	ct, body, bond := e.randomContent(addr, e.params.MaxReplyLength, true)
	msg := &types.MsgCreateReply{
		Creator:     addr,
		PostId:      post.Id,
		Body:        body,
		ContentType: ct,
		AuthorBond:  bond,
	}
	if parent != nil {
		msg.ParentReplyId = parent.Id
	}
	spent := e.storageFee(len(msg.Body)).Add(e.scanFee(ct, body))
	return msg, spent
}

// ensurePost creates a post by delivering MsgCreatePost from a random live
// account accepted by keep and returns it. ok is false when none could.
func (e *simEnv) ensurePost(keep func(simtypes.Account) bool) (types.Post, bool, error) {
	tries := 0
	for _, i := range e.r.Perm(len(e.accs)) {
		acc := e.accs[i]
		if e.tombstoned(acc.Address.String()) || !keep(acc) {
			continue
		}
		if tries++; tries > maxPrereqTries {
			break
		}
		msg, spent := e.newPostMsg(acc)
		next := e.k.GetPostCount(e.ctx)
		ok, err := e.ensure(acc, msg, spent)
		if err != nil {
			return types.Post{}, false, err
		}
		if !ok {
			continue
		}
		p, found := e.k.GetPost(e.ctx, next)
		return p, found, nil
	}
	return types.Post{}, false, nil
}

// ensureReply creates a top-level reply on post by delivering
// MsgCreateReply from a random live account accepted by keep (and passing
// the post's reply gate) and returns it.
func (e *simEnv) ensureReply(post types.Post, keep func(simtypes.Account) bool) (types.Reply, bool, error) {
	if !e.replyable(post) {
		return types.Reply{}, false, nil
	}
	tries := 0
	for _, i := range e.r.Perm(len(e.accs)) {
		acc := e.accs[i]
		addr := acc.Address.String()
		if e.tombstoned(addr) || !keep(acc) || !e.canReply(addr, post) {
			continue
		}
		if tries++; tries > maxPrereqTries {
			break
		}
		msg, spent := e.newReplyMsg(acc, post, nil)
		next := e.k.GetReplyCount(e.ctx)
		ok, err := e.ensure(acc, msg, spent)
		if err != nil {
			return types.Reply{}, false, err
		}
		if !ok {
			continue
		}
		rp, found := e.k.GetReply(e.ctx, next)
		return rp, found, nil
	}
	return types.Reply{}, false, nil
}

// ownedPostOrCreate returns a random post accepted by keep whose author is a
// live sim account, creating one (from an account accepted by author) when
// none exists, then re-checking keep on it.
func (e *simEnv) ownedPostOrCreate(keep func(types.Post) bool, author func(simtypes.Account) bool) (types.Post, simtypes.Account, bool, error) {
	posts := e.postsWhere(func(p types.Post) bool {
		if !keep(p) {
			return false
		}
		acc, ok := e.owner(p.Creator)
		return ok && author(acc)
	})
	if p, ok := pick(e.r, posts); ok {
		acc, _ := e.owner(p.Creator)
		return p, acc, true, nil
	}
	p, ok, err := e.ensurePost(author)
	if err != nil || !ok || !keep(p) {
		return types.Post{}, simtypes.Account{}, false, err
	}
	acc, ok := e.owner(p.Creator)
	return p, acc, ok, nil
}

// replyOrCreate returns a random reply accepted by keep, creating a reply on
// a replyable post (from an account accepted by author) when none exists,
// then re-checking keep on it.
func (e *simEnv) replyOrCreate(keep func(types.Reply) bool, author func(simtypes.Account) bool) (types.Reply, bool, error) {
	if rp, ok := pick(e.r, e.repliesWhere(keep)); ok {
		return rp, true, nil
	}
	authors := e.live(author)
	post, ok := pick(e.r, e.postsWhere(func(p types.Post) bool {
		if !e.replyable(p) {
			return false
		}
		for _, a := range authors {
			if e.canReply(a.Address.String(), p) {
				return true
			}
		}
		return false
	}))
	if !ok {
		// A new post from an author can always take its author's reply.
		var err error
		post, ok, err = e.ensurePost(author)
		if err != nil || !ok {
			return types.Reply{}, false, err
		}
	}
	rp, ok, err := e.ensureReply(post, author)
	if err != nil || !ok || !keep(rp) {
		return types.Reply{}, false, err
	}
	return rp, true, nil
}

// --- Random data generators ---

// truncate cuts s to at most n bytes (n == 0 leaves s unchanged).
func truncate(s string, n uint64) string {
	if n > 0 && uint64(len(s)) > n {
		return s[:n]
	}
	return s
}

func randomReactionType(r *rand.Rand) types.ReactionType {
	choices := []types.ReactionType{
		types.ReactionType_REACTION_TYPE_LIKE,
		types.ReactionType_REACTION_TYPE_INSIGHTFUL,
		types.ReactionType_REACTION_TYPE_DISAGREE,
		types.ReactionType_REACTION_TYPE_FUNNY,
	}
	return choices[r.Intn(len(choices))]
}

func randomBody(r *rand.Rand) string {
	bodies := []string{
		"Simulation blog body content.",
		"Testing the blog module with random content.",
		"Phoenix aurora zenith: notes from the simulation.",
		"This was created during simulation testing.",
		"Sample content for blog simulation.",
	}
	return bodies[r.Intn(len(bodies))]
}

func randomTitle(r *rand.Rand) string {
	titles := []string{
		"Simulation Post",
		"Test Blog Entry",
		"Random Thoughts",
		"Sample Article",
		"Quick Update",
	}
	return titles[r.Intn(len(titles))]
}

// zero is a zero SPARK amount (a message that spends nothing).
func zero() math.Int { return math.ZeroInt() }
