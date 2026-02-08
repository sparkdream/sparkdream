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

// SimulateMsgCreateTitle simulates a MsgCreateTitle message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgCreateTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Generate a unique title ID
		titleId := fmt.Sprintf("sim_title_%d_%d", ctx.BlockHeight(), r.Intn(10000))

		// Check if title already exists
		_, err := k.Title.Get(ctx, titleId)
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTitle{}), "title already exists"), nil, nil
		}

		// Create a new title using direct keeper call
		title := types.Title{
			TitleId:              titleId,
			Name:                 fmt.Sprintf("Title %s", titleId),
			Description:          "A simulation generated title",
			Rarity:               types.Rarity(r.Intn(6) + 1), // COMMON to MYTHIC
			RequirementType:      types.RequirementType(r.Intn(8) + 1),
			RequirementThreshold: uint64(1 + r.Intn(10)),
			RequirementSeason:    uint64(r.Intn(5)),
			Seasonal:             r.Intn(2) == 1,
		}

		// Save the title directly via keeper
		if err := k.Title.Set(ctx, titleId, title); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTitle{}), "failed to create title"), nil, nil
		}

		// Return success - using NoOpMsg with "ok" comment to indicate direct keeper call succeeded
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateTitle{}), "ok (direct keeper call)"), nil, nil
	}
}
