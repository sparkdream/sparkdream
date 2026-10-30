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

// SimulateMsgUpdateClass rewrites a random visible, unfrozen class's description.
func SimulateMsgUpdateClass(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateClass{}
		c, owner, ok := randomClassWhere(r, ctx, k, accs, func(c types.Class) bool {
			return c.Status != types.ContentStatus_CONTENT_STATUS_HIDDEN && !c.MetadataFrozen
		})
		if !ok {
			return noop(msg, "no updatable class")
		}
		msg = &types.MsgUpdateClass{Owner: owner.Address.String(), ClassId: c.Id,
			Description: "zenith " + simtypes.RandStringOfLength(r, 12), UpdateMask: []string{"description"}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
