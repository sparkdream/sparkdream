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

func SimulateMsgApproveInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create an approver
		approver, approverAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgApproveInitiative{}), "failed to get/create approver"), nil, nil
		}

		// Find or create a submitted initiative
		initID, err := getOrCreateInitiative(r, ctx, k, approver, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgApproveInitiative{}), "failed to get/create initiative"), nil, nil
		}

		// Always approve in the simulation. Rejection path calls
		// ReturnBudget, which requires the project's AllocatedBudget to
		// still cover this initiative's budget — but other operations may
		// have since reallocated or spent it, leading to spurious
		// "only 0 allocated" failures.
		msg := &types.MsgApproveInitiative{
			Creator:      approver.Address,
			InitiativeId: initID,
			Approved:     true,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      approverAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
