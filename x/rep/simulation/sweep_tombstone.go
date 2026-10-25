package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgSweepTombstone(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var found *types.Tombstone
		_ = k.Tombstones.Walk(ctx, nil, func(_ string, t types.Tombstone) (bool, error) {
			if _, ok := accountFor(accs, t.Canonical); ok {
				found = &t
				return true, nil
			}
			return false, nil
		})
		if found == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSweepTombstone{}), "no tombstone owned by a sim account"), nil, nil
		}
		acc, _ := accountFor(accs, found.Canonical)
		msg := &types.MsgSweepTombstone{Signer: found.Canonical, Tombstone: found.Address}
		return deliver(r, app, ctx, msg, acc, ak, bk, txGen)
	}
}
