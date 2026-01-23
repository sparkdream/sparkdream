package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgSubmitInterimWork(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a member
		member, memberAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitInterimWork{}), "failed to get/create member"), nil, nil
		}

		// Find or create an in-progress interim assigned to this member
		interim, interimID, err := findInterimByAssignee(r, ctx, k, member.Address, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS)
		if err != nil || interim == nil {
			// Create a new interim and set to IN_PROGRESS
			interimID, err = createInterim(ctx, k, r, member)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSubmitInterimWork{}), "failed to create interim"), nil, nil
			}
			interimObj, _ := k.Interim.Get(ctx, interimID)
			interimObj.Status = types.InterimStatus_INTERIM_STATUS_IN_PROGRESS
			_ = k.Interim.Set(ctx, interimID, interimObj)
		}

		msg := &types.MsgSubmitInterimWork{
			Creator:        member.Address,
			InterimId:      interimID,
			DeliverableUri: fmt.Sprintf("https://docs.example.com/interim/%d", r.Intn(1000)),
			Comments:       "Simulation interim work submission",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      memberAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
