package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func SimulateMsgCancel(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancel{}

		// Find a PROPOSED contribution to cancel (simplest case: contributor cancels before any backing)
		contrib, contribID, err := findContribution(r, ctx, k, types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED)
		if err != nil || contrib == nil {
			// Try IN_PROGRESS without any BACKED tranches
			contrib, contribID, err = findContribution(r, ctx, k, types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS)
			if err != nil || contrib == nil {
				// Create one to cancel
				simAccount, _ := simtypes.RandomAcc(r, accs)
				contribID, err = getOrCreateContribution(r, ctx, k, simAccount.Address.String())
				if err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create contribution"), nil, nil
				}
				c, err := k.Contribution.Get(ctx, contribID)
				if err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get contribution"), nil, nil
				}
				contrib = &c
			}
		}

		// Check that no tranche has reached BACKED (contributor cancel rule)
		if keeper.HasAnyTrancheReachedStatus(contrib, types.TrancheStatus_TRANCHE_STATUS_BACKED) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "cannot cancel - tranche already backed"), nil, nil
		}

		// Remove old status index
		if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove status index"), nil, nil
		}

		// Cancel all tranches
		for i := range contrib.Tranches {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
		contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED

		if err := k.Contribution.Set(ctx, contribID, *contrib); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save contribution"), nil, nil
		}
		if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save status index"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
