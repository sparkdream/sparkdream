package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgAppealGovAction simulates a MsgAppealGovAction message using direct keeper calls.
// This bypasses the membership requirement for simulation purposes.
// Full x/rep integration testing should be done in integration tests.
func SimulateMsgAppealGovAction(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Random action type: 1=warning, 2=demotion, 3=zeroing, 4=tag_removal, 5=forum_pause, 6=thread_lock, 7=thread_move
		actionType := uint64(r.Intn(7) + 1)

		// Generate a unique action target
		actionTargetPrefixes := []string{"category", "tag", "member_report", "thread"}
		actionTarget := fmt.Sprintf("%s_%d_%d", actionTargetPrefixes[r.Intn(len(actionTargetPrefixes))], ctx.BlockHeight(), r.Intn(1000000))

		// Use direct keeper calls to create the appeal (bypasses membership check)
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

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealGovAction{}), "ok (direct keeper call)"), nil, nil
	}
}
