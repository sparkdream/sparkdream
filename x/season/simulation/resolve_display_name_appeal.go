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

// SimulateMsgResolveDisplayNameAppeal simulates a MsgResolveDisplayNameAppeal message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
func SimulateMsgResolveDisplayNameAppeal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgResolveDisplayNameAppeal{})

		// Find an active moderation with an appeal
		mod, _, err := findDisplayNameModeration(r, ctx, k)
		if err != nil || mod == nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no moderation records found"), nil, nil
		}

		if !mod.Active || mod.AppealChallengeId == "" {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active appeal to resolve"), nil, nil
		}

		// Randomly decide if appeal succeeds or fails
		appealSucceeded := r.Intn(2) == 1

		// Resolve via direct keeper call
		if err := k.ResolveDisplayNameAppealInternal(ctx, mod.Member, appealSucceeded); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to resolve appeal"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}
