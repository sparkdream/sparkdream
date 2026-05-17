package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgFinalizeControllerTransfer resolves a random open
// ControllerTransferCase with a REJECT verdict and clears the
// "one open case per (op, service_type)" index entry.
func SimulateMsgFinalizeControllerTransfer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		caseRow, caseID, found := findControllerTransferCase(r, ctx, k)
		msg := &types.MsgFinalizeControllerTransfer{}
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no open controller-transfer case"), nil, nil
		}
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg.JuryAuthority = simAccount.Address.String()
		msg.JuryCaseId = caseID
		msg.Verdict = types.TransferVerdict_TRANSFER_VERDICT_REJECT

		opBytes := mustAccBytes(caseRow.OperatorAddress)
		_ = k.ControllerTransferCases.Remove(ctx, caseID)
		_ = k.OpenControllerTransferByOperator.Remove(ctx, collections.Join(opBytes, caseRow.ServiceType))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
