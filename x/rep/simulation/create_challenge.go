package simulation

import (
	"fmt"
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

func SimulateMsgCreateChallenge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a challenger with DREAM
		minStake := math.NewInt(100)
		challenger, challengerAcc, err := getOrCreateMemberWithDream(r, ctx, k, accs, minStake)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateChallenge{}), "failed to get/create challenger with DREAM"), nil, nil
		}

		// Find or create an active project
		project, _, err := findProject(r, ctx, k, types.ProjectStatus_PROJECT_STATUS_ACTIVE)
		if err != nil || project == nil {
			_, err := getOrCreateProject(r, ctx, k, challenger)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateChallenge{}), "failed to create project"), nil, nil
			}
		}

		// Find or create a submitted initiative to challenge
		initID, err := getOrCreateInitiative(r, ctx, k, challenger, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateChallenge{}), "failed to get/create initiative"), nil, nil
		}

		// Calculate stake (10-30% of available balance, min 100)
		if challenger.DreamBalance == nil || challenger.DreamBalance.LT(minStake) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateChallenge{}), "insufficient balance"), nil, nil
		}

		availableBalance := *challenger.DreamBalance
		if challenger.StakedDream != nil {
			availableBalance = availableBalance.Sub(*challenger.StakedDream)
		}

		if availableBalance.LT(minStake) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateChallenge{}), "insufficient unstaked balance"), nil, nil
		}

		maxStake := availableBalance.QuoRaw(3)
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

		// Always create non-anonymous challenges in simulation
		// Anonymous challenges require cryptographic membership proofs that cannot be generated in sim
		msg := &types.MsgCreateChallenge{
			Challenger:      challenger.Address,
			InitiativeId:    initID,
			Reason:          "Simulation challenge",
			Evidence:        []string{fmt.Sprintf("evidence-%d", r.Intn(1000))},
			StakedDream:     &stakeAmount,
			IsAnonymous:     false,
			PayoutAddress:   challenger.Address,
			MembershipProof: nil,
			Nullifier:       nil,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      challengerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
