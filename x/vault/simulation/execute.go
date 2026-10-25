package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

func SimulateMsgExecute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgExecute{})
		v, p, ok := randomPendingProposal(r, ctx, k)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pending proposal"), nil, nil
		}
		if k.Tally(ctx, v, p) < v.Threshold {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "threshold not met"), nil, nil
		}
		signers := signerAccounts(v, accs)
		if len(signers) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no simulated signer"), nil, nil
		}
		signer := signers[r.Intn(len(signers))]
		msg := &types.MsgExecute{Signer: signer.Address.String(), ProposalId: p.ProposalId}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg, CoinsSpentInMsg: sdk.NewCoins(),
			Context: ctx, SimAccount: signer, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
		})
	}
}
