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

func SimulateMsgApprove(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgApprove{}

		// Find a PROPOSED contribution to approve
		contrib, contribID, err := findContribution(r, ctx, k, types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED)
		if err != nil || contrib == nil {
			// No proposed contributions — create one first, then approve it
			simAccount, _ := simtypes.RandomAcc(r, accs)
			contribID, err = getOrCreateContribution(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create contribution"), nil, nil
			}
		}

		// Approve by transitioning to IN_PROGRESS
		_, err = approveContribution(ctx, k, contribID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to approve: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
