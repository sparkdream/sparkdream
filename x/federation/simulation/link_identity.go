package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgLinkIdentity(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need an active peer
		peer, err := getOrCreateActivePeer(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLinkIdentity{}), "failed to get/create active peer"), nil, nil
		}

		// Check if link already exists
		_, err = k.IdentityLinks.Get(ctx, collections.Join(addr, peer.Id))
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLinkIdentity{}), "link already exists"), nil, nil
		}

		// Check max links
		count, _ := k.IdentityLinkCount.Get(ctx, addr)
		if count >= types.DefaultParams().MaxIdentityLinksPerUser {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLinkIdentity{}), "max identity links reached"), nil, nil
		}

		remoteIdentity := fmt.Sprintf("remote-user-%s", simtypes.RandStringOfLength(r, 8))
		link := types.IdentityLink{
			LocalAddress:   addr,
			PeerId:         peer.Id,
			RemoteIdentity: remoteIdentity,
			Status:         types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED,
			LinkedAt:       ctx.BlockTime().Unix(),
		}

		if err := k.IdentityLinks.Set(ctx, collections.Join(addr, peer.Id), link); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLinkIdentity{}), "failed to set link"), nil, nil
		}
		_ = k.IdentityLinksByRemote.Set(ctx, collections.Join(peer.Id, remoteIdentity), addr)
		_ = k.IdentityLinkCount.Set(ctx, addr, count+1)

		// Set unverified link expiration
		expiry := ctx.BlockTime().Unix() + int64(types.DefaultParams().UnverifiedLinkTtl.Seconds())
		_ = k.UnverifiedLinkExp.Set(ctx, collections.Join3(expiry, addr, peer.Id))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLinkIdentity{}), "ok (direct keeper call)"), nil, nil
	}
}
