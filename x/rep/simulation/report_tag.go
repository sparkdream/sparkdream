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

// SimulateMsgReportTag simulates a MsgReportTag by writing a tag report
// directly through the keeper, bypassing the DREAM bond check for speed.
func SimulateMsgReportTag(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "no tags in registry"), nil, nil
		}

		if _, err := k.TagReport.Get(ctx, tagName); err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "tag report already exists"), nil, nil
		}

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

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgReportTag{}), "ok (direct keeper call)"), nil, nil
	}
}

// firstExistingTag returns a registered tag name, or "" if the registry is empty.
func firstExistingTag(ctx sdk.Context, k keeper.Keeper) (string, error) {
	var name string
	err := k.Tag.Walk(ctx, nil, func(key string, _ types.Tag) (bool, error) {
		name = key
		return true, nil
	})
	return name, err
}
