package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgFundEdition pays retro funding to a random circulating edition.
func SimulateMsgFundEdition(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFundEdition{}
		c, e, _, ok := randomEdition(r, ctx, k, accs, func(c types.Class, e types.Edition) bool {
			return e.Supply > 0 && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && e.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no fundable edition")
		}
		funder, _ := simtypes.RandomAcc(r, accs)
		amount := sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(10))*1_000_000)
		msg = &types.MsgFundEdition{Funder: funder.Address.String(), ClassId: c.Id, EditionId: e.Id, Amount: amount}
		return deliver(r, app, ctx, ak, bk, txGen, funder, msg, amount.Amount)
	}
}
