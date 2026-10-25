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

func SimulateMsgCancelProposal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCancelProposal{})
		_, p, ok := randomPendingProposal(r, ctx, k)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pending proposal"), nil, nil
		}
		proposer, found := findAccount(accs, p.Proposer)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "proposer is not a simulated account"), nil, nil
		}
		msg := &types.MsgCancelProposal{Signer: proposer.Address.String(), ProposalId: p.ProposalId}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg, CoinsSpentInMsg: sdk.NewCoins(),
			Context: ctx, SimAccount: proposer, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
		})
	}
}
