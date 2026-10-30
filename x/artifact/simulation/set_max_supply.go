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

// SimulateMsgSetMaxSupply lowers (or first sets) a random open unique class's supply cap, never below what was issued.
func SimulateMsgSetMaxSupply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetMaxSupply{}
		c, owner, ok := randomClassWhere(r, ctx, k, accs, func(c types.Class) bool {
			issued := c.Supply + c.Burned + c.ReservedSupply
			return !c.MintingClosed && !c.IsEditions() && (c.MaxSupply == 0 || c.MaxSupply > issued)
		})
		if !ok {
			return noop(msg, "no class whose cap can be lowered")
		}
		issued := c.Supply + c.Burned + c.ReservedSupply
		ceiling := c.MaxSupply
		if ceiling == 0 {
			ceiling = issued + 100
		}
		next := issued + commontypes.Saturate[uint64](r.Int63n(commontypes.Saturate[int64](ceiling-issued)))
		if next == 0 {
			next = 1
		}
		if c.MaxSupply != 0 && next >= c.MaxSupply {
			return noop(msg, "cap already at the floor")
		}
		msg = &types.MsgSetMaxSupply{Owner: owner.Address.String(), ClassId: c.Id, MaxSupply: next}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
