package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgResolveTagReport simulates a MsgResolveTagReport by removing the
// tag report directly through the keeper, bypassing the operations committee
// authorization check for simulation speed.
func SimulateMsgResolveTagReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		tagName, err := firstExistingTag(ctx, k)
		if err != nil || tagName == "" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "no tags in registry"), nil, nil
		}

		if _, err := k.TagReport.Get(ctx, tagName); err != nil {
			now := ctx.BlockTime().Unix()
			report := types.TagReport{
				TagName:       tagName,
				TotalBond:     fmt.Sprintf("%d", 10+r.Intn(90)),
				FirstReportAt: now,
				UnderReview:   false,
				Reporters:     []string{simAccount.Address.String()},
			}
			if err := k.TagReport.Set(ctx, tagName, report); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "failed to seed tag report"), nil, nil
			}
		}

		if err := k.TagReport.Remove(ctx, tagName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "failed to remove report"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "ok (direct keeper call)"), nil, nil
	}
}
