package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func SimulateMsgAbandonInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a member
		member, memberAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbandonInitiative{}), "failed to get/create member"), nil, nil
		}

		// Try to find an initiative assigned to this member
		_, initID, err := findInitiativeByAssignee(r, ctx, k, member.Address, types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED)
		if err != nil {
			// Create a project and initiative directly assigned to this member
			projectID, err := getOrCreateProject(r, ctx, k, member)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbandonInitiative{}), "failed to create project"), nil, nil
			}

			// Create initiative directly
			initID, err = k.InitiativeSeq.Next(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbandonInitiative{}), "failed to get init ID"), nil, nil
			}

			tier := randomInitiativeTier(r)
			budget := calculateBudgetByTier(r, tier)

			newInit := types.Initiative{
				Id:          initID,
				ProjectId:   projectID,
				Title:       randomName(r, "Initiative"),
				Description: "Simulation generated initiative",
				Tags:        randomTags(r),
				Tier:        tier,
				Category:    randomInitiativeCategory(r),
				Status:      types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
				Assignee:    member.Address, // Explicitly assign to this member
				Budget:      &budget,
			}

			if err := k.Initiative.Set(ctx, initID, newInit); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAbandonInitiative{}), "failed to create initiative"), nil, nil
			}

			// Mirror CreateInitiative's budget allocation on the project so AbandonInitiative's
			// ReturnBudget succeeds (non-permissionless only).
			if project, perr := k.GetProject(ctx, projectID); perr == nil && !project.Permissionless {
				project.AllocatedBudget = PtrInt(keeper.DerefInt(project.AllocatedBudget).Add(budget))
				_ = k.Project.Set(ctx, projectID, project)
			}
		}

		msg := &types.MsgAbandonInitiative{
			Creator:      member.Address,
			InitiativeId: initID,
			Reason:       "Simulation abandonment",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      memberAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
