package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgSlashBridge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		bridge, err := getOrCreateActiveBridge(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSlashBridge{}), "failed to get/create active bridge"), nil, nil
		}

		if bridge.Stake.Amount.IsZero() {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSlashBridge{}), "bridge has no stake"), nil, nil
		}

		// Slash 1-10% of stake
		slashPct := int64(r.Intn(10) + 1)
		slashAmount := bridge.Stake.Amount.Quo(math.NewInt(100)).Mul(math.NewInt(slashPct))
		if slashAmount.IsZero() {
			slashAmount = math.OneInt()
		}

		bridge.Stake.Amount = bridge.Stake.Amount.Sub(slashAmount)
		bridge.SlashCount++
		if err := k.BridgeOperators.Set(ctx, collections.Join(bridge.Address, bridge.PeerId), bridge); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSlashBridge{}), "failed to update bridge"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSlashBridge{}), "ok (direct keeper call)"), nil, nil
	}
}
