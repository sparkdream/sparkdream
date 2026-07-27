package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgStake(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a member with DREAM to stake
		minStake := math.NewInt(50)
		staker, stakerAcc, err := getOrCreateMemberWithDream(r, ctx, k, accs, minStake)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "failed to get/create member with DREAM"), nil, nil
		}

		// Get or create an initiative to stake on
		targetType := types.StakeTargetType_STAKE_TARGET_INITIATIVE
		targetID, err := getOrCreateInitiative(r, ctx, k, staker, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
		if err != nil {
			// Fallback to OPEN status
			targetID, err = getOrCreateInitiative(r, ctx, k, staker, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "failed to get/create initiative"), nil, nil
			}
		}

		// Precheck CreateStake's per-target limits against live state. A
		// simulation runs many ops against a small account set, so the same
		// member repeatedly drawing the same random initiative is normal — and
		// both of these would fail delivery, which fails the whole simulation.
		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "failed to load params"), nil, nil
		}
		existingStakes, err := k.GetStakesByTarget(ctx, targetType, targetID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "failed to read existing stakes"), nil, nil
		}
		stakerTotal := math.ZeroInt()
		tranches := 0
		for _, s := range existingStakes {
			if s.Staker != staker.Address {
				continue
			}
			stakerTotal = stakerTotal.Add(s.Amount)
			tranches++
		}
		if tranches >= types.MaxStakeTranchesPerTarget {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "tranche cap reached on this target"), nil, nil
		}

		// Calculate stake amount (10-50% of available DREAM)
		// Calculate available (unstaked) balance
		if staker.DreamBalance == nil || staker.DreamBalance.LT(minStake) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "insufficient balance"), nil, nil
		}

		availableBalance := *staker.DreamBalance
		if staker.StakedDream != nil {
			availableBalance = availableBalance.Sub(*staker.StakedDream)
		}

		if availableBalance.LT(minStake) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "insufficient unstaked balance"), nil, nil
		}

		maxStake := availableBalance.QuoRaw(2) // Max 50%
		if maxStake.LT(minStake) {
			maxStake = minStake
		}
		if maxStake.GT(availableBalance) {
			maxStake = availableBalance
		}

		var stakeAmount math.Int
		rangeVal := maxStake.Sub(minStake).Int64()
		if rangeVal > 0 {
			stakeAmount = math.NewInt(int64(r.Intn(int(rangeVal))) + minStake.Int64())
		} else {
			stakeAmount = minStake
		}

		// Second half of the per-target precheck: the anti-whale cap is on the
		// staker's cumulative total on this target, not on a single stake.
		if stakerTotal.Add(stakeAmount).GT(params.MaxInitiativeStakePerMember) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgStake{}), "per-member stake cap reached on this target"), nil, nil
		}

		msg := &types.MsgStake{
			Staker:     staker.Address,
			TargetType: targetType,
			TargetId:   targetID,
			Amount:     &stakeAmount,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      stakerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
