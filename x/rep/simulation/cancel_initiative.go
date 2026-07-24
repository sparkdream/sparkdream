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

func SimulateMsgCancelInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a member who will own the parent project. The signer is
		// resolved to the project creator below, satisfying the handler's
		// authorization gate ("only the project creator or operations committee
		// may cancel an initiative") regardless of which initiative is selected.
		creator, _, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "failed to get/create creator"), nil, nil
		}

		// Find or create an OPEN initiative (getOrCreateInitiative leaves the
		// assignee empty for OPEN status).
		initID, err := getOrCreateInitiative(r, ctx, k, creator, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "failed to get/create initiative"), nil, nil
		}

		initiative, err := k.Initiative.Get(ctx, initID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "failed to get initiative"), nil, nil
		}

		// Precheck the keeper's preconditions so the delivered tx cannot fail:
		// cancellation requires OPEN status and no assignee.
		if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN || initiative.Assignee != "" {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "initiative not open/unassigned"), nil, nil
		}

		// Resolve the signer to the project creator (the authorized canceller).
		project, err := k.Project.Get(ctx, initiative.ProjectId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "failed to get project"), nil, nil
		}
		signerMember := &types.Member{Address: project.Creator}
		signerAcc, found := getAccountFromMember(signerMember, accs)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "project creator not in sim accounts"), nil, nil
		}

		// ReturnBudget errors if the project's allocated budget is below the
		// amount being returned; skip rather than deliver a failing tx if the
		// found initiative's allocation was never mirrored (non-permissionless).
		if !project.Permissionless {
			allocated := keeper.DerefInt(project.AllocatedBudget)
			if allocated.LT(keeper.DerefInt(initiative.Budget)) {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCancelInitiative{}), "project budget allocation below initiative budget"), nil, nil
			}
		}

		msg := &types.MsgCancelInitiative{
			Creator:      project.Creator,
			InitiativeId: initID,
			Reason:       "Simulation cancellation",
		}

		return simulation.GenAndDeliverTxWithRandFees(simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      signerAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		})
	}
}
