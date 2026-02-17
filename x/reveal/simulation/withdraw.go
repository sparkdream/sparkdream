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

func SimulateMsgWithdraw(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgWithdraw{}

		// Pick a random account and try to find a withdrawable stake they own
		simAccount, _ := simtypes.RandomAcc(r, accs)
		stakerAddr := simAccount.Address.String()

		stake, stakeID, err := findWithdrawableStake(r, ctx, k, stakerAddr)
		if err != nil || stake == nil {
			// No withdrawable stake found for this account.
			// Try to create a staking scenario: create approved contribution, stake, then withdraw.
			contributorAcc, found := pickDifferentAccount(r, accs, stakerAddr)
			if !found {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no different account for contributor"), nil, nil
			}

			contribID, err := getOrCreateApprovedContribution(r, ctx, k, contributorAcc.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create approved contribution"), nil, nil
			}

			// Create a stake on tranche 0
			contrib, err := k.Contribution.Get(ctx, contribID)
			if err != nil || len(contrib.Tranches) == 0 {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "contribution has no tranches"), nil, nil
			}

			params, err := k.Params.Get(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get params"), nil, nil
			}

			stakeAmount := params.MinStakeAmount
			newStakeID, err := createRevealStakeForTranche(ctx, k, stakerAddr, contribID, 0, stakeAmount)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create stake"), nil, nil
			}

			// Update tranche dream_staked
			contrib.Tranches[0].DreamStaked = contrib.Tranches[0].DreamStaked.Add(stakeAmount)
			if err := k.Contribution.Set(ctx, contribID, contrib); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update contribution"), nil, nil
			}

			stakeID = newStakeID
			s, err := k.RevealStake.Get(ctx, stakeID)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get created stake"), nil, nil
			}
			stake = &s
		}

		// Withdraw: remove the stake and update tranche dream_staked
		contrib, err := k.Contribution.Get(ctx, stake.ContributionId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get contribution for withdrawal"), nil, nil
		}

		if int(stake.TrancheId) < len(contrib.Tranches) {
			tranche := &contrib.Tranches[stake.TrancheId]
			tranche.DreamStaked = tranche.DreamStaked.Sub(stake.Amount)

			// If tranche was BACKED and now below threshold, revert to STAKING
			if tranche.Status == types.TrancheStatus_TRANCHE_STATUS_BACKED &&
				tranche.DreamStaked.LT(tranche.StakeThreshold) {
				tranche.Status = types.TrancheStatus_TRANCHE_STATUS_STAKING
				tranche.BackedAt = 0
				tranche.RevealDeadline = 0
			}

			if err := k.Contribution.Set(ctx, stake.ContributionId, contrib); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update contribution"), nil, nil
			}
		}

		// Remove stake and indexes
		if err := k.RevealStake.Remove(ctx, stakeID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove stake"), nil, nil
		}
		trancheKey := keeper.TrancheKey(stake.ContributionId, stake.TrancheId)
		_ = k.StakesByTranche.Remove(ctx, collections.Join(trancheKey, stakeID))
		_ = k.StakesByStaker.Remove(ctx, collections.Join(stake.Staker, stakeID))

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
