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

// SimulateMsgReportTag simulates a MsgReportTag message using direct keeper calls.
// This bypasses the DREAM token requirement for simulation purposes.
// Full token integration testing should be done in integration tests.
func SimulateMsgReportTag(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a tag to report
		tagName, err := getOrCreateTag(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "failed to get/create tag"), nil, nil
		}

		// Check if a report already exists for this tag
		_, err = k.TagReport.Get(ctx, tagName)
		if err == nil {
			// Report already exists, add this reporter as a cosigner
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "tag report already exists"), nil, nil
		}

		// Use direct keeper calls to create tag report (bypasses DREAM token check)
		now := ctx.BlockTime().Unix()
		report := types.TagReport{
			TagName:       tagName,
			TotalBond:     fmt.Sprintf("%d", 10+r.Intn(90)),
			FirstReportAt: now,
			UnderReview:   false,
			Reporters:     []string{simAccount.Address.String()},
		}

		if err := k.TagReport.Set(ctx, tagName, report); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "failed to create tag report"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "ok (direct keeper call)"), nil, nil
	}
}
