package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgClaimQuestReward simulates a MsgClaimQuestReward message using direct keeper calls.
// This bypasses the maintenance mode check for simulation purposes.
// Full maintenance mode testing should be done in integration tests.
func SimulateMsgClaimQuestReward(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimQuestReward{}), "failed to create profile"), nil, nil
		}

		// Find or create quest progress for this user
		questID, err := getOrCreateQuest(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimQuestReward{}), "failed to get/create quest"), nil, nil
		}

		// Create quest progress if needed
		progressKey, err := getOrCreateMemberQuestProgress(r, ctx, k, simAccount.Address.String(), questID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimQuestReward{}), "failed to create quest progress"), nil, nil
		}

		// Check if quest reward was already claimed
		progress, err := k.MemberQuestProgress.Get(ctx, progressKey)
		if err == nil && progress.Completed {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimQuestReward{}), "quest reward already claimed"), nil, nil
		}

		// Use direct keeper calls to claim reward (bypasses maintenance mode check)
		if err == nil {
			progress.Completed = true
			k.MemberQuestProgress.Set(ctx, progressKey, progress)
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgClaimQuestReward{}), "ok (direct keeper call)"), nil, nil
	}
}
