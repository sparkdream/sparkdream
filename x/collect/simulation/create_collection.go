package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgCreateCollection delivers MsgCreateCollection from a random
// account below its tiered collection limit. Members create ACTIVE
// collections (permanent, burning the base deposit, or TTL, escrowing it);
// non-members create PENDING TTL collections within max_non_member_ttl_blocks
// and escrow the deposit plus the endorsement creation fee. A few member
// collections are private and encrypted.
func SimulateMsgCreateCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgCreateCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		acc, _ := simtypes.RandomAcc(e.r, e.accs)
		msg, spent, reason, err := e.newCollectionMsg(acc, collectionOpts{})
		if err != nil {
			return noOpErr(&types.MsgCreateCollection{}, err)
		}
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(acc, msg, spent)
	})
}

// collectionOpts narrows the collection newCollectionMsg builds.
type collectionOpts struct {
	public    bool // never private/encrypted
	ttl       bool // never permanent
	permanent bool // never TTL (members only)
}

// newCollectionMsg builds a MsgCreateCollection for acc that passes the
// handler's local checks, with the SPARK it costs. A non-empty reason means
// acc cannot create one now.
func (e *simEnv) newCollectionMsg(acc simtypes.Account, opts collectionOpts) (*types.MsgCreateCollection, math.Int, string, error) {
	addr := acc.Address.String()
	p := e.params
	msg := &types.MsgCreateCollection{
		Creator:    addr,
		Type:       randomCollectionType(e.r),
		Visibility: types.Visibility_VISIBILITY_PUBLIC,
		Tags:       e.randomTags(p.MaxTagsPerCollection),
	}

	// Tiered collection limit: base + trust level index x per-level for
	// active members.
	count, err := e.ownedCollections(addr)
	if err != nil {
		return msg, math.ZeroInt(), "", err
	}
	limit := p.MaxCollectionsBase
	member := e.isActiveMember(addr)
	if member {
		limit += commontypes.Saturate[uint32](int(e.loadMembers()[addr].TrustLevel)) * p.MaxCollectionsPerTrustLevel
	}
	if count >= limit {
		return msg, math.ZeroInt(), "collection limit reached", nil
	}

	if member && !opts.public && e.r.Intn(5) == 0 {
		msg.Visibility = types.Visibility_VISIBILITY_PRIVATE
		msg.Encrypted = true
		msg.EncryptedData = randomEncryptedData(e.r, p.MaxEncryptedDataSize)
	} else {
		msg.Name = randomCollectionName(e.r)
		msg.Description = randomDescription(e.r)
		if e.r.Intn(2) == 0 {
			msg.CoverUri = randomURI(e.r)
		}
	}

	height := e.ctx.BlockHeight()
	ttl := int64(50 + e.r.Intn(2000))
	spent := p.BaseCollectionDeposit
	if member {
		if opts.permanent || (!opts.ttl && e.r.Intn(2) == 0) {
			// Permanent: the deposit is burned.
			return msg, spent, "", nil
		}
		if p.MaxTtlBlocks > 0 && ttl > p.MaxTtlBlocks {
			ttl = p.MaxTtlBlocks
		}
	} else {
		if opts.permanent {
			return msg, math.ZeroInt(), "non-members cannot create permanent collections", nil
		}
		if p.MaxNonMemberTtlBlocks <= 0 {
			return msg, math.ZeroInt(), "non-member TTL cap is zero", nil
		}
		if ttl > p.MaxNonMemberTtlBlocks {
			ttl = p.MaxNonMemberTtlBlocks
		}
		spent = spent.Add(p.EndorsementCreationFee)
	}
	if ttl <= 0 {
		return msg, math.ZeroInt(), "max TTL is zero", nil
	}
	msg.ExpiresAt = height + ttl
	return msg, spent, "", nil
}

// ownedCollections counts the collections addr owns.
func (e *simEnv) ownedCollections(addr string) (uint32, error) {
	colls, err := e.collectionsWhere(func(c types.Collection) bool { return c.Owner == addr })
	return commontypes.Saturate[uint32](len(colls)), err
}

// ensureCollectionBy creates a collection by delivering MsgCreateCollection
// from a random account accepted by keep. It returns false when none could.
func (e *simEnv) ensureCollectionBy(keep func(simtypes.Account) bool, opts collectionOpts) (bool, error) {
	acc, ok := pick(e.r, accountsWhere(e.accs, keep))
	if !ok {
		return false, nil
	}
	msg, spent, reason, err := e.newCollectionMsg(acc, opts)
	if err != nil || reason != "" {
		return false, err
	}
	return e.ensure(acc, msg, spent)
}

// ensureMemberCollection creates an ACTIVE public collection (TTL when ttl is
// set) from a random active member.
func (e *simEnv) ensureMemberCollection(ttl bool) (bool, error) {
	return e.ensureCollectionBy(func(a simtypes.Account) bool { return e.isActiveMember(a.Address.String()) },
		collectionOpts{public: true, ttl: ttl})
}

// ensureAnyCollection creates a collection from a random account (ACTIVE for
// a member, PENDING for a non-member).
func (e *simEnv) ensureAnyCollection() (bool, error) {
	return e.ensureCollectionBy(func(simtypes.Account) bool { return true }, collectionOpts{})
}
