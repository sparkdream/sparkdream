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

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
)

// The collect simulation delivers real transactions through the message
// handlers. A delivered tx the handler rejects fails the whole simulation,
// so every op picks its signer and target from live state, checks the
// handler's preconditions against live state and params, and returns a
// NoOp when one cannot hold. Prerequisite state (a collection to add items
// to, a curator or sentinel bond) is only ever created by delivering the
// real message first.
//
// The x/rep side of a handler (membership, trust level, DREAM locks and
// burns, bonded-role eligibility and reservations, the tag registry, appeal
// bonds) is read through x/rep's query service, and because the collect
// keeper does not expose those calls one by one, every message is finally
// dry-run through its real handler on a throwaway cache of the block state
// (submit). Only a message that dry-run accepts is delivered.

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

	repQuery reptypes.QueryClient
	members  map[string]reptypes.Member
	tags     []string
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
		// stream, so how much randomness an op uses (which depends on collect
		// state) never shifts the draws of the other modules' ops.
		local := rand.New(rand.NewSource(r.Int63() ^ r.Int63()))
		e := &simEnv{r: local, app: app, ctx: ctx, accs: accs, txGen: txGen, ak: ak, bk: bk, k: k, params: params}
		e.repQuery = reptypes.NewQueryClient(&baseapp.QueryServiceTestHelper{GRPCQueryRouter: app.GRPCQueryRouter(), Ctx: ctx})
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
// the SPARK the handler takes from acc (deposits, fees, burns); the random
// tx fee is drawn only from what is left, so the handler still finds it.
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
	return opMsg.OK, nil
}

// spendable returns acc's spendable SPARK.
func (e *simEnv) spendable(acc simtypes.Account) math.Int {
	return e.bk.SpendableCoins(e.ctx, acc.Address).AmountOf(e.k.BondDenom(e.ctx))
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

// collectionsWhere returns the collections accepted by keep.
func (e *simEnv) collectionsWhere(keep func(types.Collection) bool) ([]types.Collection, error) {
	var out []types.Collection
	err := e.k.Collection.Walk(e.ctx, nil, func(_ uint64, c types.Collection) (bool, error) {
		if keep(c) {
			out = append(out, c)
		}
		return false, nil
	})
	return out, err
}

// itemsOf returns the items of a collection.
func (e *simEnv) itemsOf(collID uint64) ([]types.Item, error) {
	var out []types.Item
	err := e.k.ItemsByCollection.Walk(e.ctx, collections.NewPrefixedPairRange[uint64, uint64](collID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			item, err := e.k.Item.Get(e.ctx, key.K2())
			if err != nil {
				return true, err
			}
			out = append(out, item)
			return false, nil
		})
	return out, err
}

// collaborator returns addr's collaborator record on the collection.
func (e *simEnv) collaborator(collID uint64, addr string) (types.Collaborator, bool) {
	c, err := e.k.Collaborator.Get(e.ctx, keeper.CollaboratorCompositeKey(collID, addr))
	return c, err == nil
}

// hasSponsorshipRequest reports whether the collection has a pending
// sponsorship request (which locks its items).
func (e *simEnv) hasSponsorshipRequest(collID uint64) bool {
	has, err := e.k.SponsorshipRequest.Has(e.ctx, collID)
	return err == nil && has
}

// canWrite mirrors HasWriteAccess plus the item handlers' rule that a
// non-owner writer must be an active member: the owner, or an EDITOR/ADMIN
// collaborator who is an x/rep member.
func (e *simEnv) canWrite(c types.Collection, addr string, needMember bool) bool {
	if addr == c.Owner {
		return true
	}
	collab, ok := e.collaborator(c.Id, addr)
	if !ok {
		return false
	}
	if collab.Role != types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR &&
		collab.Role != types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
		return false
	}
	return !needMember || e.isActiveMember(addr)
}

// writers returns the sim accounts with write access to the collection.
func (e *simEnv) writers(c types.Collection, needMember bool) []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool {
		return e.canWrite(c, a.Address.String(), needMember)
	})
}

// itemsEditable mirrors the shared item-handler gates on the collection:
// mutable and not hidden.
func itemsEditable(c types.Collection) bool {
	return !c.Immutable && c.Status != types.CollectionStatus_COLLECTION_STATUS_HIDDEN
}

// dailyCount returns addr's counter for a daily-limited category today.
func (e *simEnv) dailyCount(addr, category string) uint32 {
	n, err := e.k.ReactionLimit.Get(e.ctx, keeper.ReactionLimitCompositeKey(addr, e.ctx.BlockHeight(), category))
	if err != nil {
		return 0
	}
	return n
}

// expired reports whether a TTL collection has reached its expiry block.
func (e *simEnv) expired(c types.Collection) bool {
	return c.ExpiresAt > 0 && c.ExpiresAt <= e.ctx.BlockHeight()
}

// publicActive mirrors ValidatePublicActiveTarget (and, with feedback,
// ValidatePublicActiveFeedbackTarget) for a collection target.
func publicActive(c types.Collection, feedback bool) bool {
	return c.Visibility == types.Visibility_VISIBILITY_PUBLIC &&
		c.Status == types.CollectionStatus_COLLECTION_STATUS_ACTIVE &&
		(!feedback || c.CommunityFeedbackEnabled)
}

// target is a collection or item a reaction, flag or hide points at.
type target struct {
	typ  types.FlagTargetType
	id   uint64
	coll types.Collection
}

// publicTargets returns the collections and items that pass
// ValidatePublicActiveTarget (with feedback: ...FeedbackTarget). When there
// is none, a member first creates a public collection (a real
// MsgCreateCollection).
func (e *simEnv) publicTargets(feedback bool) ([]target, error) {
	out, err := e.listPublicTargets(feedback)
	if err != nil || len(out) > 0 {
		return out, err
	}
	if ok, err := e.ensureMemberCollection(false); err != nil || !ok {
		return nil, err
	}
	return e.listPublicTargets(feedback)
}

func (e *simEnv) listPublicTargets(feedback bool) ([]target, error) {
	colls, err := e.collectionsWhere(func(c types.Collection) bool { return publicActive(c, feedback) })
	if err != nil {
		return nil, err
	}
	var out []target
	for _, c := range colls {
		out = append(out, target{types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, c.Id, c})
		items, err := e.itemsOf(c.Id)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Status != types.ItemStatus_ITEM_STATUS_HIDDEN {
				out = append(out, target{types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, it.Id, c})
			}
		}
	}
	return out, nil
}

// hasUnresolvedHide reports whether the target has an unresolved hide record.
func (e *simEnv) hasUnresolvedHide(t types.FlagTargetType, id uint64) (bool, error) {
	found := false
	err := e.k.HideRecordByTarget.Walk(e.ctx,
		collections.NewPrefixedPairRange[string, uint64](keeper.HideRecordTargetCompositeKey(t, id)),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, err := e.k.HideRecord.Get(e.ctx, key.K2())
			if err != nil {
				return true, err
			}
			if !hr.Resolved {
				found = true
				return true, nil
			}
			return false, nil
		})
	return found, err
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

// isActiveMember mirrors x/rep's IsActiveMember.
func (e *simEnv) isActiveMember(addr string) bool {
	m, ok := e.loadMembers()[addr]
	return ok && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE
}

// trustAtLeast reports whether addr is an active member at or above level.
func (e *simEnv) trustAtLeast(addr string, level reptypes.TrustLevel) bool {
	m, ok := e.loadMembers()[addr]
	return ok && m.Status == reptypes.MemberStatus_MEMBER_STATUS_ACTIVE && m.TrustLevel >= level
}

// trustAtLeastName is trustAtLeast for a trust level param string; an
// unknown name is never met (the handler's meetsMinTrustLevel).
func (e *simEnv) trustAtLeastName(addr, level string) bool {
	tl, ok := keeper.ParseTrustLevel(level)
	return ok && e.trustAtLeast(addr, tl)
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
	res, err := e.repQuery.Canonical(e.ctx, &reptypes.QueryCanonicalRequest{Address: addr})
	return err == nil && res.Tombstoned
}

// bondedRole returns addr's bonded role record.
func (e *simEnv) bondedRole(role reptypes.RoleType, addr string) (reptypes.BondedRole, bool) {
	res, err := e.repQuery.BondedRole(e.ctx, &reptypes.QueryBondedRoleRequest{RoleType: role, Address: addr})
	if err != nil {
		return reptypes.BondedRole{}, false
	}
	return res.BondedRole, true
}

// roleHolders returns the sim accounts holding a bonded role in a status
// that can act (the handlers' EligibleForRole is dry-run).
func (e *simEnv) roleHolders(role reptypes.RoleType) []simtypes.Account {
	return accountsWhere(e.accs, func(a simtypes.Account) bool {
		br, ok := e.bondedRole(role, a.Address.String())
		return ok && br.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED &&
			e.isActiveMember(a.Address.String())
	})
}

// ensureRoleHolder returns the sim accounts holding the role, bonding one
// first through x/rep's MsgBondRole when none does: an active member at the
// role's trust level with min_bond unlocked DREAM bonds exactly min_bond.
func (e *simEnv) ensureRoleHolder(role reptypes.RoleType) ([]simtypes.Account, error) {
	if holders := e.roleHolders(role); len(holders) > 0 {
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
	candidates := accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		if _, has := e.bondedRole(role, addr); has {
			return false
		}
		if cfg.MinTrustLevel != "" && !e.trustAtLeastName(addr, cfg.MinTrustLevel) {
			return false
		}
		return e.unlockedDream(addr).GTE(minBond)
	})
	acc, ok := pick(e.r, candidates)
	if !ok {
		return nil, nil
	}
	msg := &reptypes.MsgBondRole{Creator: acc.Address.String(), RoleType: role, Amount: minBond.String()}
	if ok, err := e.ensure(acc, msg, math.ZeroInt()); err != nil || !ok {
		return nil, err
	}
	e.members = nil // the bond locked DREAM
	return e.roleHolders(role), nil
}

// liveTags returns the x/rep registry tags a collection or review may carry:
// existing, not reserved, well-formed and within max_tag_length.
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
		if len(t.Name) > int(e.params.MaxTagLength) || !commontypes.ValidateTagFormat(t.Name) {
			continue
		}
		if _, err := e.repQuery.GetReservedTag(e.ctx, &reptypes.QueryGetReservedTagRequest{Name: t.Name}); err == nil {
			continue
		}
		e.tags = append(e.tags, t.Name)
	}
	return e.tags
}

// randomTags returns up to max distinct live tags.
func (e *simEnv) randomTags(maxTags uint32) []string {
	tags := e.liveTags()
	n := e.r.Intn(3)
	if n > len(tags) {
		n = len(tags)
	}
	if n > int(maxTags) {
		n = int(maxTags)
	}
	out := make([]string, 0, n)
	for _, i := range e.r.Perm(len(tags))[:n] {
		out = append(out, tags[i])
	}
	return out
}

// --- Random data generators ---

func randomCollectionName(r *rand.Rand) string {
	names := []string{
		"phoenix-gallery", "aurora-vault", "zenith-archive", "nebula-catalog",
		"prism-set", "vortex-shelf", "cascade-library", "ember-trove",
		"atlas-digest", "horizon-collection", "pulse-registry", "forge-index",
	}
	return names[r.Intn(len(names))]
}

func randomTitle(r *rand.Rand) string {
	titles := []string{
		"sunrise-entry", "moonlight-piece", "starfall-item", "dewdrop-record",
		"thunder-artifact", "coral-fragment", "crystal-shard", "amber-relic",
	}
	return titles[r.Intn(len(titles))]
}

func randomDescription(r *rand.Rand) string {
	descriptions := []string{
		"A simulation-generated entry for testing",
		"Sample content created during chain simulation",
		"Auto-generated description for sim coverage",
		"Test description from simulation framework",
	}
	return descriptions[r.Intn(len(descriptions))]
}

func randomURI(r *rand.Rand) string {
	return fmt.Sprintf("ipfs://Qm%s", simtypes.RandStringOfLength(r, 40))
}

func randomCollectionType(r *rand.Rand) types.CollectionType {
	validTypes := []types.CollectionType{
		types.CollectionType_COLLECTION_TYPE_NFT,
		types.CollectionType_COLLECTION_TYPE_LINK,
		types.CollectionType_COLLECTION_TYPE_ONCHAIN,
		types.CollectionType_COLLECTION_TYPE_MIXED,
	}
	return validTypes[r.Intn(len(validTypes))]
}

func randomModerationReason(r *rand.Rand) commontypes.ModerationReason {
	reasons := []commontypes.ModerationReason{
		commontypes.ModerationReason_MODERATION_REASON_SPAM,
		commontypes.ModerationReason_MODERATION_REASON_HARASSMENT,
		commontypes.ModerationReason_MODERATION_REASON_MISINFORMATION,
		commontypes.ModerationReason_MODERATION_REASON_INAPPROPRIATE,
		commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION,
	}
	return reasons[r.Intn(len(reasons))]
}

func randomCurationVerdict(r *rand.Rand) types.CurationVerdict {
	if r.Intn(2) == 0 {
		return types.CurationVerdict_CURATION_VERDICT_UP
	}
	return types.CurationVerdict_CURATION_VERDICT_DOWN
}

// randomEncryptedData returns an opaque ciphertext stand-in within the
// encrypted-data size cap.
func randomEncryptedData(r *rand.Rand, maxSize uint32) []byte {
	n := 16 + r.Intn(48)
	if n > int(maxSize) {
		n = int(maxSize)
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(r.Intn(256))
	}
	return out
}

// itemContent is the content of an item for MsgAddItem / MsgUpdateItem /
// AddItemEntry. Public items carry a short title and a link, NFT or custom
// reference; items of an encrypted collection carry only ciphertext.
type itemContent struct {
	title, description, imageURI string
	refType                      types.ReferenceType
	nft                          *types.NftReference
	link                         *types.LinkReference
	custom                       *types.CustomReference
	attributes                   []*types.KeyValuePair
	encrypted                    []byte
}

func randomItemContent(r *rand.Rand, encrypted bool, params types.Params) itemContent {
	if encrypted {
		return itemContent{encrypted: randomEncryptedData(r, params.MaxEncryptedDataSize)}
	}
	c := itemContent{title: randomTitle(r), description: randomDescription(r)}
	switch r.Intn(4) {
	case 0:
		c.refType = types.ReferenceType_REFERENCE_TYPE_NFT
		c.nft = &types.NftReference{ChainId: "sparkdream", ContractAddress: "phoenix-class", TokenId: fmt.Sprintf("%d", r.Intn(1000))}
	case 1:
		c.refType = types.ReferenceType_REFERENCE_TYPE_CUSTOM
		c.custom = &types.CustomReference{TypeLabel: "note", Value: "aurora-" + simtypes.RandStringOfLength(r, 8)}
	default:
		c.refType = types.ReferenceType_REFERENCE_TYPE_LINK
		c.link = &types.LinkReference{Uri: randomURI(r)}
	}
	if r.Intn(3) == 0 && params.MaxAttributesPerItem > 0 {
		c.attributes = []*types.KeyValuePair{{Key: "zenith", Value: simtypes.RandStringOfLength(r, 6)}}
	}
	return c
}

// writable is a collection together with a sim account that may write it.
type writable struct {
	coll   types.Collection
	writer simtypes.Account
}

// writableCollections returns (collection, writer) pairs for collections
// passing keep, where writer has write access (a non-owner writer must be a
// member when needMember is set). When there is none, a random account
// first creates a collection (a real MsgCreateCollection).
func (e *simEnv) writableCollections(needMember bool, keep func(types.Collection) bool) ([]writable, error) {
	out, err := e.listWritable(needMember, keep)
	if err != nil || len(out) > 0 {
		return out, err
	}
	if ok, err := e.ensureAnyCollection(); err != nil || !ok {
		return nil, err
	}
	return e.listWritable(needMember, keep)
}

func (e *simEnv) listWritable(needMember bool, keep func(types.Collection) bool) ([]writable, error) {
	colls, err := e.collectionsWhere(keep)
	if err != nil {
		return nil, err
	}
	var out []writable
	for _, c := range colls {
		for _, w := range e.writers(c, needMember) {
			out = append(out, writable{c, w})
		}
	}
	return out, nil
}

// itemDepositCost returns the SPARK a writer pays for n new items: the
// per-item deposit (held or burned) plus, for a non-member writer, the
// per-item spam tax.
func (e *simEnv) itemDepositCost(writer string, n int64) math.Int {
	cost := e.params.PerItemDeposit.MulRaw(n)
	if !e.isActiveMember(writer) {
		cost = cost.Add(e.params.PerItemSpamTax.MulRaw(n))
	}
	return cost
}

// editableItem is an item, its collection and a sim account with write
// access to the collection.
type editableItem struct {
	item   types.Item
	coll   types.Collection
	writer simtypes.Account
}

// editableItems returns the non-hidden items of collections passing keep,
// each with a writer (a non-owner writer must be a member when needMember
// is set). When there is none, a writer first adds an item to such a
// collection (a real MsgAddItem).
func (e *simEnv) editableItems(needMember bool, keep func(types.Collection) bool) ([]editableItem, error) {
	out, err := e.listEditableItems(needMember, keep)
	if err != nil || len(out) > 0 {
		return out, err
	}
	pairs, err := e.writableCollections(true, func(c types.Collection) bool {
		return itemsEditable(c) && keep(c) && !e.hasSponsorshipRequest(c.Id) && c.ItemCount < uint64(e.params.MaxItemsPerCollection)
	})
	if err != nil {
		return nil, err
	}
	w, ok := pick(e.r, pairs)
	if !ok {
		return nil, nil
	}
	msg := e.newItemMsg(w)
	if ok, err := e.ensure(w.writer, msg, e.itemDepositCost(msg.Creator, 1)); err != nil || !ok {
		return nil, err
	}
	return e.listEditableItems(needMember, keep)
}

func (e *simEnv) listEditableItems(needMember bool, keep func(types.Collection) bool) ([]editableItem, error) {
	pairs, err := e.listWritable(needMember, func(c types.Collection) bool { return itemsEditable(c) && keep(c) })
	if err != nil {
		return nil, err
	}
	var out []editableItem
	for _, w := range pairs {
		items, err := e.itemsOf(w.coll.Id)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.Status != types.ItemStatus_ITEM_STATUS_HIDDEN {
				out = append(out, editableItem{it, w.coll, w.writer})
			}
		}
	}
	return out, nil
}

// reactionTarget picks a member and a public, active, feedback-enabled target
// they may vote on: not the owner or a collaborator of its collection, not
// already reacted to, and under the daily cap for category.
func (e *simEnv) reactionTarget(category string, maxPerDay uint32) (simtypes.Account, target, string, error) {
	targets, err := e.publicTargets(true)
	if err != nil {
		return simtypes.Account{}, target{}, "", err
	}
	t, ok := pick(e.r, targets)
	if !ok {
		return simtypes.Account{}, target{}, "no public active feedback target", nil
	}
	voters := accountsWhere(e.accs, func(a simtypes.Account) bool {
		addr := a.Address.String()
		if addr == t.coll.Owner || !e.isActiveMember(addr) || e.dailyCount(addr, category) >= maxPerDay {
			return false
		}
		if _, collab := e.collaborator(t.coll.Id, addr); collab {
			return false
		}
		has, err := e.k.ReactionDedup.Has(e.ctx, keeper.ReactionDedupCompositeKey(addr, t.typ, t.id))
		return err == nil && !has
	})
	voter, ok := pick(e.r, voters)
	if !ok {
		return simtypes.Account{}, t, "no member may react to the target", nil
	}
	return voter, t, "", nil
}

// hideTargetExists reports whether a hide record's target still exists.
func (e *simEnv) hideTargetExists(hr types.HideRecord) bool {
	var has bool
	var err error
	switch hr.TargetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		has, err = e.k.Collection.Has(e.ctx, hr.TargetId)
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		has, err = e.k.Item.Has(e.ctx, hr.TargetId)
	}
	return err == nil && has
}

// countCollaborators counts collaborator records.
func (e *simEnv) countCollaborators() (int, error) {
	n := 0
	err := e.k.Collaborator.Walk(e.ctx, nil, func(string, types.Collaborator) (bool, error) {
		n++
		return false, nil
	})
	return n, err
}
