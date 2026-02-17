package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func SimulateMsgReveal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgReveal{}

		// Find or create a contribution with a BACKED tranche (ready for reveal)
		simAccount, _ := simtypes.RandomAcc(r, accs)
		stakerAcc, found := pickDifferentAccount(r, accs, simAccount.Address.String())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no different account for staker"), nil, nil
		}

		contribID, trancheID, err := getOrCreateBackedContribution(r, ctx, k, simAccount.Address.String(), stakerAcc.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get/create backed contribution: "+err.Error()), nil, nil
		}

		// Reveal the code (update tranche status directly)
		contrib, err := k.Contribution.Get(ctx, contribID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get contribution"), nil, nil
		}

		if int(trancheID) >= len(contrib.Tranches) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "tranche out of range"), nil, nil
		}

		tranche := &contrib.Tranches[trancheID]
		if tranche.Status != types.TrancheStatus_TRANCHE_STATUS_BACKED {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "tranche not in BACKED status"), nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get params"), nil, nil
		}

		tranche.CodeUri = randomURI(r, "code")
		tranche.DocsUri = randomURI(r, "docs")
		tranche.CommitHash = randomCommitHash(r)
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_REVEALED
		tranche.RevealedAt = ctx.BlockHeight()
		tranche.VerificationDeadline = ctx.BlockHeight() + params.VerificationPeriodEpochs

		if err := k.Contribution.Set(ctx, contribID, contrib); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save contribution"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
