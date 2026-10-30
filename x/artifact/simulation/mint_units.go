package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgMintUnits mints units of a random edition to a random account.
func SimulateMsgMintUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMintUnits{}
		c, e, owner, ok := randomEdition(r, ctx, k, accs, func(c types.Class, e types.Edition) bool {
			return c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && !c.MintingClosed &&
				e.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && (e.MaxSupply == 0 || e.Supply+e.Burned+e.Reserved+10 <= e.MaxSupply)
		})
		if !ok {
			return noop(msg, "no mintable edition")
		}
		to, _ := simtypes.RandomAcc(r, accs)
		p := k.GetParams(ctx)
		msg = &types.MsgMintUnits{Minter: owner.Address.String(), ClassId: c.Id, EditionId: e.Id,
			Entries: []types.UnitMintEntry{{Recipient: to.Address.String(), Amount: commontypes.Saturate[uint64](1 + r.Intn(10))}}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, p.TokenDeposit.Add(p.InboxFee).Add(p.TokenDeposit))
	}
}
