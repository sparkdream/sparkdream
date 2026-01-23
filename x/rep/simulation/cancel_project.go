package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgCancelProject(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a canceller
		canceller, cancellerAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelProject{}), "failed to get/create canceller"), nil, nil
		}

		// Find an active project to cancel OR create one
		var projectID uint64
		project, pID, err := findProject(r, ctx, k, types.ProjectStatus_PROJECT_STATUS_ACTIVE)
		if err == nil && project != nil {
			projectID = pID
		} else {
			// Create a new active project to cancel
			projectID, err = getOrCreateProject(r, ctx, k, canceller)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelProject{}), "failed to create project"), nil, nil
			}
			// Ensure it's active (not completed/cancelled)
			projObj, _ := k.Project.Get(ctx, projectID)
			projObj.Status = types.ProjectStatus_PROJECT_STATUS_ACTIVE
			_ = k.Project.Set(ctx, projectID, projObj)
		}

		msg := &types.MsgCancelProject{
			Creator:   canceller.Address,
			ProjectId: projectID,
			Reason:    "Simulation cancellation",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      cancellerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
