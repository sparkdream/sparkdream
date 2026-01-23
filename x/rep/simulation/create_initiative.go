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

func SimulateMsgCreateInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a creator
		creator, creatorAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateInitiative{}), "failed to get/create creator"), nil, nil
		}

		// Get or create an active project
		projectID, err := getOrCreateProject(r, ctx, k, creator)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateInitiative{}), "failed to get/create project"), nil, nil
		}

		// Generate initiative details
		tier := randomInitiativeTier(r)
		category := randomInitiativeCategory(r)

		// Budget based on tier
		var budget math.Int
		switch tier {
		case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
			budget = math.NewInt(int64(r.Intn(400) + 100)) // 100-500
		case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
			budget = math.NewInt(int64(r.Intn(900) + 600)) // 600-1500
		case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
			budget = math.NewInt(int64(r.Intn(2000) + 1500)) // 1500-3500
		case types.InitiativeTier_INITIATIVE_TIER_EPIC:
			budget = math.NewInt(int64(r.Intn(4000) + 3500)) // 3500-7500
		}

		msg := &types.MsgCreateInitiative{
			Creator:     creator.Address,
			ProjectId:   projectID,
			Title:       fmt.Sprintf("Initiative-%d", r.Intn(10000)),
			Description: "Simulation generated initiative",
			Tags:        randomTags(r),
			Tier:        uint64(tier),
			Category:    uint64(category),
			TemplateId:  "",
			Budget:      &budget,
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      creatorAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
