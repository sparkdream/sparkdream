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

func SimulateMsgProposeProject(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a proposer
		proposer, proposerAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgProposeProject{}), "failed to get/create proposer"), nil, nil
		}

		// Generate project details
		category := randomProjectCategory(r)
		council := randomCouncil(r)
		budgetDream := math.NewInt(int64(r.Intn(90000) + 10000)) // 10k-100k DREAM
		budgetSpark := math.NewInt(int64(r.Intn(9000) + 1000))   // 1k-10k SPARK

		msg := &types.MsgProposeProject{
			Creator:         proposer.Address,
			Name:            fmt.Sprintf("Project-%d", r.Intn(10000)),
			Description:     "Simulation generated project",
			Tags:            randomTags(r),
			Category:        category,
			Council:         council,
			RequestedBudget: &budgetDream,
			RequestedSpark:  &budgetSpark,
			Deliverables:    []string{"Deliverable 1", "Deliverable 2"},
			Milestones:      []string{"Milestone 1", "Milestone 2"},
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      proposerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
