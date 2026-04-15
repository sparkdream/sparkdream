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

func SimulateMsgUnbondBridge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find a revoked bridge to unbond, or create one
		bridge, err := findRevokedBridge(r, ctx, k)
		if err != nil || bridge == nil {
			// Create active bridge then revoke it
			b, err := getOrCreateActiveBridge(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondBridge{}), "failed to get/create bridge"), nil, nil
			}
			b.Status = types.BridgeStatus_BRIDGE_STATUS_REVOKED
			b.RevokedAt = ctx.BlockTime().Unix()
			_ = k.BridgeOperators.Set(ctx, collections.Join(b.Address, b.PeerId), b)
			bridge = &b
		}

		bridge.Status = types.BridgeStatus_BRIDGE_STATUS_UNBONDING
		bridge.UnbondingEndTime = ctx.BlockTime().Unix() + int64(types.DefaultParams().BridgeUnbondingPeriod.Seconds())
		if err := k.BridgeOperators.Set(ctx, collections.Join(bridge.Address, bridge.PeerId), *bridge); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondBridge{}), "failed to update bridge"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnbondBridge{}), "ok (direct keeper call)"), nil, nil
	}
}
