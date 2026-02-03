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

// SimulateMsgExtendSeason simulates a MsgExtendSeason message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgExtendSeason(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get the current season
		season, err := k.Season.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgExtendSeason{}), "no active season"), nil, nil
		}

		// Check if season is in a state that can be extended (must be active)
		if season.Status != types.SeasonStatus_SEASON_STATUS_ACTIVE {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgExtendSeason{}), "season not active"), nil, nil
		}

		// Check max extensions (typically 3)
		if season.ExtensionsCount >= 3 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgExtendSeason{}), "max extensions reached"), nil, nil
		}

		// Extend the season by 1-3 epochs (random)
		extensionEpochs := uint64(r.Intn(3) + 1)
		blocksPerEpoch := int64(1000) // Assume 1000 blocks per epoch for simulation

		// Update season fields
		season.EndBlock += int64(extensionEpochs) * blocksPerEpoch
		season.ExtensionsCount++
		season.TotalExtensionEpochs += extensionEpochs

		// Save the updated season
		if err := k.Season.Set(ctx, season); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgExtendSeason{}), "failed to extend season"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgExtendSeason{}), "ok (direct keeper call)"), nil, nil
	}
}
