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

// SimulateMsgReportDisplayName simulates a MsgReportDisplayName using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgReportDisplayName(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if len(accs) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "need at least 2 accounts"), nil, nil
		}

		// Pick two distinct accounts: reporter and target
		perm := r.Perm(len(accs))
		_ = accs[perm[0]] // reporter (unused in direct keeper call mode)
		targetAccount := accs[perm[1]]

		// Ensure target has a profile with display name
		if err := getOrCreateMemberProfile(r, ctx, k, targetAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "failed to create target profile"), nil, nil
		}

		// Re-read profile and ensure display name is set (may have been cleared by prior moderation)
		profile, err := k.MemberProfile.Get(ctx, targetAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "failed to get target profile"), nil, nil
		}
		if profile.DisplayName == "" {
			profile.DisplayName = randomDisplayName(r)
			if err := k.MemberProfile.Set(ctx, targetAccount.Address.String(), profile); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "failed to restore display name"), nil, nil
			}
		}

		// Create moderation record directly (bypasses DREAM lock requirement)
		moderation := types.DisplayNameModeration{
			Member:       targetAccount.Address.String(),
			RejectedName: profile.DisplayName,
			Reason:       "Simulated report",
			ModeratedAt:  ctx.BlockHeight(),
			Active:       true,
		}
		if err := k.DisplayNameModeration.Set(ctx, targetAccount.Address.String(), moderation); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "failed to save moderation"), nil, nil
		}

		// Clear the target's display name (moderated)
		profile.DisplayName = ""
		if err := k.MemberProfile.Set(ctx, targetAccount.Address.String(), profile); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "failed to update profile"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportDisplayName{}), "ok (direct keeper call)"), nil, nil
	}
}
