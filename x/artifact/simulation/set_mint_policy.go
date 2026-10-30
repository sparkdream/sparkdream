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
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgSetMintPolicy changes a random open class's per-address limit, which is allowed even once the curve opened.
func SimulateMsgSetMintPolicy(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetMintPolicy{}
		c, owner, ok := randomClassWhere(r, ctx, k, accs, func(c types.Class) bool {
			return !c.MintingClosed && c.Status != types.ContentStatus_CONTENT_STATUS_HIDDEN && !c.IsEditions()
		})
		if !ok {
			return noop(msg, "no open class owned by a sim account")
		}
		mp := c.MintPolicy
		mp.PerAddressLimit = commontypes.Saturate[uint32](1 + r.Intn(10))
		msg = &types.MsgSetMintPolicy{Owner: owner.Address.String(), ClassId: c.Id, MintPolicy: mp}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
