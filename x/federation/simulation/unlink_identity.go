package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgUnlinkIdentity(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Get or create an identity link to unlink
		link, err := getOrCreateIdentityLink(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnlinkIdentity{}), "failed to get/create identity link"), nil, nil
		}

		// Remove the link
		if err := k.IdentityLinks.Remove(ctx, collections.Join(link.LocalAddress, link.PeerId)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnlinkIdentity{}), "failed to remove link"), nil, nil
		}
		_ = k.IdentityLinksByRemote.Remove(ctx, collections.Join(link.PeerId, link.RemoteIdentity))

		// Decrement link count
		count, _ := k.IdentityLinkCount.Get(ctx, link.LocalAddress)
		if count > 0 {
			_ = k.IdentityLinkCount.Set(ctx, link.LocalAddress, count-1)
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnlinkIdentity{}), "ok (direct keeper call)"), nil, nil
	}
}
