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

// SimulateMsgRevokeUnits revokes units of an issuer-burnable edition.
func SimulateMsgRevokeUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRevokeUnits{}
		c, b, _, ok := randomBalance(r, ctx, k, accs, func(c types.Class, b types.Balance) bool {
			return c.Flags.IssuerMayBurn() && b.Amount > 0
		})
		if !ok {
			return noop(msg, "no revocable units")
		}
		owner, ok := findAccount(accs, c.Owner)
		if !ok {
			return noop(msg, "class owner is not a sim account")
		}
		msg = &types.MsgRevokeUnits{Owner: owner.Address.String(), ClassId: c.Id, EditionId: b.EditionId, Holder: b.Owner,
			Amount: uint64(1 + r.Intn(int(b.Amount))), Reason: "simulated"}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
