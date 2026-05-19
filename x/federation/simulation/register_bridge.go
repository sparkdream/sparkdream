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

func SimulateMsgRegisterBridge(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterBridge{}), "failed to get/create active peer"), nil, nil
		}

		// Check if bridge already exists for this operator+peer
		_, err = k.BridgeBindings.Get(ctx, collections.Join(addr, peer.Id))
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterBridge{}), "bridge already exists"), nil, nil
		}

		bridge := types.BridgeBinding{
			Address:      addr,
			PeerId:       peer.Id,
			Protocol:     randomProtocol(r),
			Endpoint:     randomEndpoint(r),
			RegisteredAt: ctx.BlockTime().Unix(),
		}

		if err := k.BridgeBindings.Set(ctx, collections.Join(addr, peer.Id), bridge); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterBridge{}), "failed to set bridge"), nil, nil
		}
		_ = k.BridgesByPeer.Set(ctx, collections.Join(peer.Id, addr))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRegisterBridge{}), "ok (direct keeper call)"), nil, nil
	}
}
