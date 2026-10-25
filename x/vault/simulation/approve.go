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

func SimulateMsgApprove(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgApprove{})
		v, p, ok := randomPendingProposal(r, ctx, k)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no pending proposal"), nil, nil
		}
		var candidates []simtypes.Account
		for _, a := range signerAccounts(v, accs) {
			if !hasApproved(p, a.Address.String()) {
				candidates = append(candidates, a)
			}
		}
		if len(candidates) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "every simulated signer already approved"), nil, nil
		}
		signer := candidates[r.Intn(len(candidates))]
		msg := &types.MsgApprove{Signer: signer.Address.String(), ProposalId: p.ProposalId, MessagesHash: p.MessagesHash, Execute: r.Intn(2) == 0}
		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R: r, App: app, TxGen: txGen, Cdc: nil, Msg: msg, CoinsSpentInMsg: sdk.NewCoins(),
			Context: ctx, SimAccount: signer, AccountKeeper: ak, Bankkeeper: bk, ModuleName: types.ModuleName,
		})
	}
}
