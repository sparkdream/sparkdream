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

// SimulateMsgSetNextSeasonInfo simulates a MsgSetNextSeasonInfo message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgSetNextSeasonInfo(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get the current season to determine the next season number
		season, err := k.Season.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetNextSeasonInfo{}), "no active season"), nil, nil
		}

		// Generate random season info
		themes := []string{"Discovery", "Adventure", "Innovation", "Unity", "Challenge", "Growth"}
		theme := themes[r.Intn(len(themes))]

		nextSeasonInfo := types.NextSeasonInfo{
			Name:  fmt.Sprintf("Season %d", season.Number+1),
			Theme: theme,
		}

		// Save the next season info using direct keeper call
		if err := k.NextSeasonInfo.Set(ctx, nextSeasonInfo); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetNextSeasonInfo{}), "failed to set next season info"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgSetNextSeasonInfo{}), "ok (direct keeper call)"), nil, nil
	}
}
