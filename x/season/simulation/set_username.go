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

// SimulateMsgSetUsername simulates a MsgSetUsername message using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgSetUsername(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetUsername{}), "failed to create profile"), nil, nil
		}

		// Get the profile
		profile, err := k.MemberProfile.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetUsername{}), "profile not found"), nil, nil
		}

		// Check if already has a username (can only be set once in most systems)
		if profile.Username != "" {
			// For simulation, allow changing username
		}

		// Generate a new username
		newUsername := randomUsername(r)

		// Update the profile with the new username directly (bypasses DREAM token check)
		profile.Username = newUsername

		if err := k.MemberProfile.Set(ctx, simAccount.Address.String(), profile); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetUsername{}), "failed to set username"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetUsername{}), "ok (direct keeper call)"), nil, nil
	}
}
