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

// SimulateMsgRevoke revokes a random token of a revocable class (signed by the class owner).
func SimulateMsgRevoke(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRevoke{}
		t, owner, ok := randomTokenOfClassOwner(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return !c.IsEditions() && c.Flags.IssuerMayBurn()
		})
		if !ok {
			return noop(msg, "no revocable token")
		}
		msg = &types.MsgRevoke{Owner: owner.Address.String(), ClassId: t.ClassId, TokenIds: []uint64{t.Id}, Reason: "simulation"}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
