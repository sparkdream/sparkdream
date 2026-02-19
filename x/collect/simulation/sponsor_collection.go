package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func SimulateMsgSponsorCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgSponsorCollection{
			Creator: simAccount.Address.String(),
		}

		req, collID, err := findSponsorshipRequest(r, ctx, k)
		if err != nil || req == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no sponsorship request found"), nil, nil
		}

		// Get the collection
		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get collection: "+err.Error()), nil, nil
		}

		// Remove sponsorship request and its expiry index
		if err := k.SponsorshipRequest.Remove(ctx, collID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove sponsorship request: "+err.Error()), nil, nil
		}
		if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, collID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove expiry index: "+err.Error()), nil, nil
		}

		// Update collection: set sponsor, clear expiry
		coll.SponsoredBy = simAccount.Address.String()
		if coll.ExpiresAt > 0 {
			k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, collID)) //nolint:errcheck
			coll.ExpiresAt = 0
		}

		if err := k.Collection.Set(ctx, collID, coll); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update collection: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
