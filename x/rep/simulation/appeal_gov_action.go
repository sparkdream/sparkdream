package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgAppealGovAction(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		actionType := uint64(r.Intn(7) + 1)

		actionTargetPrefixes := []string{"category", "tag", "member_report", "thread"}
		actionTarget := fmt.Sprintf("%s_%d_%d", actionTargetPrefixes[r.Intn(len(actionTargetPrefixes))], ctx.BlockHeight(), r.Intn(1000000))

		appealID, err := k.GovActionAppealSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealGovAction{}), "failed to get appeal ID"), nil, nil
		}

		appeal := types.GovActionAppeal{
			Id:           appealID,
			Appellant:    simAccount.Address.String(),
			ActionType:   types.GovActionType(actionType),
			ActionTarget: actionTarget,
			AppealReason: "The governance action was applied without proper consideration",
			Status:       types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
			CreatedAt:    ctx.BlockTime().Unix(),
		}

		if err := k.GovActionAppeal.Set(ctx, appealID, appeal); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealGovAction{}), "failed to create appeal"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealGovAction{}), "ok (direct keeper call)"), nil, nil
	}
}
