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

// SimulateMsgAppealDisplayNameModeration simulates a MsgAppealDisplayNameModeration
// using direct keeper calls. This bypasses the DREAM token requirement for simulation
// purposes. Full token integration testing should be done in integration tests.
func SimulateMsgAppealDisplayNameModeration(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Find a pending moderation case that hasn't been appealed yet
		moderation, moderatedAddr, err := findDisplayNameModeration(r, ctx, k)

		// If no moderation exists, create one so we can appeal it
		if err != nil || moderation == nil {
			simAccount, _ := simtypes.RandomAcc(r, accs)
			moderatedAddr = simAccount.Address.String()

			// Ensure profile exists
			if err := getOrCreateMemberProfile(r, ctx, k, moderatedAddr); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "failed to create profile"), nil, nil
			}

			newMod := types.DisplayNameModeration{
				Member:       moderatedAddr,
				RejectedName: "simulated-name",
				Reason:       "Simulated moderation for appeal",
				ModeratedAt:  ctx.BlockHeight(),
				Active:       true,
			}
			if err := k.DisplayNameModeration.Set(ctx, moderatedAddr, newMod); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "failed to create moderation"), nil, nil
			}
			moderation = &newMod
		}

		// Skip if already appealed
		if moderation.AppealChallengeId != "" || moderation.AppealedAt > 0 {
			// Reset appeal state so we can re-appeal in this simulation
			moderation.AppealedAt = 0
			moderation.AppealChallengeId = ""
		}

		// Mark the moderation as appealed directly (bypasses DREAM lock requirement)
		moderation.AppealedAt = ctx.BlockHeight()
		if err := k.DisplayNameModeration.Set(ctx, moderatedAddr, *moderation); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "failed to update moderation"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealDisplayNameModeration{}), "ok (direct keeper call)"), nil, nil
	}
}
