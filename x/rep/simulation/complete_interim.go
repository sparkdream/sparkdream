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

func SimulateMsgCompleteInterim(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCompleteInterim{}), "failed to get/create member"), nil, nil
		}

		// Find or create an in-progress interim
		interim, interimID, err := findInterim(r, ctx, k, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS)
		if err != nil || interim == nil {
			// Create a new interim and set to IN_PROGRESS
			interimID, err = createInterim(ctx, k, r, member)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCompleteInterim{}), "failed to create interim"), nil, nil
			}
			interimObj, _ := k.Interim.Get(ctx, interimID)
			interimObj.Status = types.InterimStatus_INTERIM_STATUS_IN_PROGRESS
			_ = k.Interim.Set(ctx, interimID, interimObj)
		}

		msg := &types.MsgCompleteInterim{
			Creator:   member.Address,
			InterimId: interimID,
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
