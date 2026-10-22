package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgClaimEditionRewards claims a random holder's retro funding.
func SimulateMsgClaimEditionRewards(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgClaimEditionRewards{}
		_, b, holder, ok := randomBalance(r, ctx, k, accs, func(_ types.Class, b types.Balance) bool { return true })
		if !ok {
			return noop(msg, "no balance")
		}
		msg = &types.MsgClaimEditionRewards{Holder: holder.Address.String(), Refs: []types.EditionRef{{ClassId: b.ClassId, EditionId: b.EditionId}}}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}
