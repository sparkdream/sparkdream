package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgStartQuest simulates a MsgStartQuest message using direct keeper calls.
// This bypasses the maintenance mode check for simulation purposes.
// Full maintenance mode testing should be done in integration tests.
func SimulateMsgStartQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Ensure member has a profile
		if err := getOrCreateMemberProfile(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStartQuest{}), "failed to create profile"), nil, nil
		}

		// Find or create an active quest
		questID, err := getOrCreateQuest(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStartQuest{}), "failed to get/create quest"), nil, nil
		}

		// Check if user has already started this quest
		progressKey := fmt.Sprintf("%s:%s", simAccount.Address.String(), questID)
		_, err = k.MemberQuestProgress.Get(ctx, progressKey)
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStartQuest{}), "quest already started"), nil, nil
		}

		// Use direct keeper calls to start quest (bypasses maintenance mode check)
		progress := types.MemberQuestProgress{
			MemberQuest:     progressKey,
			Completed:       false,
			LastAttemptBlock: ctx.BlockHeight(),
		}

		if err := k.MemberQuestProgress.Set(ctx, progressKey, progress); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStartQuest{}), "failed to create progress"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStartQuest{}), "ok (direct keeper call)"), nil, nil
	}
}
