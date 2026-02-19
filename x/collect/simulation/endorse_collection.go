package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func SimulateMsgEndorseCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgEndorseCollection{}

		// Find a seeking-endorsement collection
		coll, collID, err := findSeekingEndorsementCollection(r, ctx, k)
		if err != nil || coll == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no seeking-endorsement collection found"), nil, nil
		}

		// Endorser must be different from owner
		endorser, ok := pickDifferentAccount(r, accs, coll.Owner)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "not enough accounts"), nil, nil
		}

		// Create endorsement
		endorsement := types.Endorsement{
			CollectionId:   collID,
			Endorser:       endorser.Address.String(),
			DreamStake:     math.NewInt(int64(r.Intn(10000) + 1000)),
			EndorsedAt:     ctx.BlockHeight(),
			StakeReleaseAt: ctx.BlockHeight() + int64(r.Intn(10000)+1000),
		}

		if err := k.Endorsement.Set(ctx, collID, endorsement); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to set endorsement: "+err.Error()), nil, nil
		}

		// Set stake expiry index
		if err := k.EndorsementStakeExpiry.Set(ctx, collections.Join(endorsement.StakeReleaseAt, collID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to set stake expiry: "+err.Error()), nil, nil
		}

		// Remove from EndorsementPending (walk to find the key for this collID)
		k.EndorsementPending.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			if key.K2() == collID {
				k.EndorsementPending.Remove(ctx, key) //nolint:errcheck
				return true, nil                      // stop
			}
			return false, nil
		}) //nolint:errcheck

		// Update collection: Status -> ACTIVE, EndorsedBy -> endorser address
		oldStatus := coll.Status
		coll.Status = types.CollectionStatus_COLLECTION_STATUS_ACTIVE
		coll.EndorsedBy = endorser.Address.String()
		coll.SeekingEndorsement = false

		// Remove old status index, add new
		k.CollectionsByStatus.Remove(ctx, collections.Join(int32(oldStatus), collID)) //nolint:errcheck
		if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(coll.Status), collID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update status index: "+err.Error()), nil, nil
		}

		if err := k.Collection.Set(ctx, collID, *coll); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update collection: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
