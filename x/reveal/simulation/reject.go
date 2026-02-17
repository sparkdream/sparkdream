package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func SimulateMsgReject(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgReject{}

		// Find a PROPOSED contribution to reject
		contrib, contribID, err := findContribution(r, ctx, k, types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED)
		if err != nil || contrib == nil {
			// Create one to reject
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

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get params"), nil, nil
		}

		currentEpoch := ctx.BlockHeight()

		// Remove old status index
		if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove status index"), nil, nil
		}

		// Transition to CANCELLED with cooldown
		contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
		contrib.ProposalEligibleAt = currentEpoch + params.ProposalCooldownEpochs
		contrib.BondRemaining = math.ZeroInt()

		// Cancel all tranches
		for i := range contrib.Tranches {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}

		if err := k.Contribution.Set(ctx, contribID, *contrib); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save contribution"), nil, nil
		}
		if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save status index"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
