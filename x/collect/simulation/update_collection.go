package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// SimulateMsgUpdateCollection delivers MsgUpdateCollection from the owner of
// a mutable ACTIVE or PENDING collection. It rewrites the content (ciphertext
// only for encrypted collections) and tags, toggles community feedback now
// and then, and either keeps the expiry, extends a live TTL within the
// owner's cap, or (member owners) converts a TTL collection to permanent.
func SimulateMsgUpdateCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUpdateCollection{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateCollection{}
		colls, err := e.collectionsWhere(func(c types.Collection) bool {
			if c.Immutable || (c.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE &&
				c.Status != types.CollectionStatus_COLLECTION_STATUS_PENDING) {
				return false
			}
			_, ok := getAccountForAddress(c.Owner, e.accs)
			return ok
		})
		if err != nil {
			return noOpErr(msg, err)
		}
		coll, ok := pick(e.r, colls)
		if !ok {
			return noOp(msg, "no mutable collection owned by a sim account")
		}
		owner, _ := getAccountForAddress(coll.Owner, e.accs)
		p := e.params
		msg.Creator, msg.Id, msg.Type = coll.Owner, coll.Id, randomCollectionType(e.r)
		msg.Tags = e.randomTags(p.MaxTagsPerCollection)
		if coll.Encrypted {
			msg.EncryptedData = randomEncryptedData(e.r, p.MaxEncryptedDataSize)
		} else {
			msg.Name, msg.Description = randomCollectionName(e.r), randomDescription(e.r)
		}
		if e.r.Intn(4) == 0 {
			msg.UpdateCommunityFeedback = true
			msg.CommunityFeedbackEnabled = e.r.Intn(3) != 0
		}

		height := e.ctx.BlockHeight()
		member := e.isActiveMember(coll.Owner)
		msg.ExpiresAt = coll.ExpiresAt
		if coll.ExpiresAt > 0 {
			switch {
			case member && e.r.Intn(4) == 0:
				msg.ExpiresAt = 0 // TTL -> permanent, burns the held deposits
			case e.r.Intn(2) == 0:
				msg.ExpiresAt = coll.ExpiresAt + int64(1+e.r.Intn(500))
			}
			if msg.ExpiresAt > 0 {
				if msg.ExpiresAt <= height {
					return noOp(msg, "collection expiry already reached")
				}
				if p.MaxTtlBlocks > 0 && msg.ExpiresAt-height > p.MaxTtlBlocks {
					msg.ExpiresAt = coll.ExpiresAt
				}
				if !member && msg.ExpiresAt-coll.CreatedAt > p.MaxNonMemberTtlBlocks {
					msg.ExpiresAt = coll.ExpiresAt
				}
				if msg.ExpiresAt <= height || (!member && msg.ExpiresAt-coll.CreatedAt > p.MaxNonMemberTtlBlocks) ||
					(p.MaxTtlBlocks > 0 && msg.ExpiresAt-height > p.MaxTtlBlocks) {
					return noOp(msg, "no valid expiry for this collection")
				}
			}
		}
		return e.submit(owner, msg, math.ZeroInt())
	})
}
