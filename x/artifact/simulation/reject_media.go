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

// SimulateMsgRejectMedia rejects a random open media proposal as the holder.
func SimulateMsgRejectMedia(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRejectMedia{}
		mp, _, holder, ok := randomProposal(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no open media proposal")
		}
		msg = &types.MsgRejectMedia{Signer: holder.Address.String(), ClassId: mp.ClassId, TokenId: mp.TokenId, ProposalId: mp.Id}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}
