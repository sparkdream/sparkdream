package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func SimulateMsgStake(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgStake{}

		// Find an IN_PROGRESS contribution with a STAKING tranche
		contrib, contribID, trancheID, err := findContributionWithTrancheStatus(r, ctx, k, types.TrancheStatus_TRANCHE_STATUS_STAKING)
		if err != nil || contrib == nil {
			// Create an approved contribution (has STAKING tranche 0)
			simAccount, _ := simtypes.RandomAcc(r, accs)
			contribID, err = getOrCreateApprovedContribution(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create approved contribution"), nil, nil
			}
			c, err := k.Contribution.Get(ctx, contribID)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get contribution"), nil, nil
			}
			contrib = &c
			trancheID = 0
		}

		if int(trancheID) >= len(contrib.Tranches) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "tranche out of range"), nil, nil
		}
		tranche := contrib.Tranches[trancheID]

		// Pick a staker who is NOT the contributor (self-stake prevention)
		stakerAcc, found := pickDifferentAccount(r, accs, contrib.Contributor)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no available staker account"), nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get params"), nil, nil
		}

		// Calculate stake amount: between min_stake and remaining threshold
		remaining := tranche.StakeThreshold.Sub(tranche.DreamStaked)
		if remaining.LT(params.MinStakeAmount) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "remaining threshold below minimum"), nil, nil
		}

		stakeAmount := params.MinStakeAmount
		rangeVal := remaining.Sub(params.MinStakeAmount).Int64()
		if rangeVal > 0 {
			stakeAmount = math.NewInt(int64(r.Intn(int(rangeVal))) + params.MinStakeAmount.Int64())
		}

		// Create the stake directly
		stakeID, err := createRevealStakeForTranche(ctx, k, stakerAcc.Address.String(), contribID, trancheID, stakeAmount)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create stake: "+err.Error()), nil, nil
		}

		// Update tranche dream_staked in the contribution
		c, err := k.Contribution.Get(ctx, contribID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to re-read contribution"), nil, nil
		}
		c.Tranches[trancheID].DreamStaked = c.Tranches[trancheID].DreamStaked.Add(stakeAmount)

		// Check if tranche is now BACKED
		if c.Tranches[trancheID].DreamStaked.GTE(c.Tranches[trancheID].StakeThreshold) {
			c.Tranches[trancheID].Status = types.TrancheStatus_TRANCHE_STATUS_BACKED
			c.Tranches[trancheID].BackedAt = ctx.BlockHeight()
			c.Tranches[trancheID].RevealDeadline = ctx.BlockHeight() + params.RevealDeadlineEpochs
		}

		if err := k.Contribution.Set(ctx, contribID, c); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update contribution"), nil, nil
		}

		_ = stakeID
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
