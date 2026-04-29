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

func SimulateMsgAssignInitiative(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a member who will own the parent project. Using the
		// project creator as the msg.Creator satisfies the handler's
		// authorization gate ("only the assignee, project creator, or
		// operations committee may assign") regardless of which initiative
		// gets selected below.
		creator, creatorAcc, err := getOrCreateMember(r, ctx, k, accs)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "failed to get/create creator"), nil, nil
		}

		// Find or create an open initiative
		initID, err := getOrCreateInitiative(r, ctx, k, creator, types.InitiativeStatus_INITIATIVE_STATUS_OPEN)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "failed to get/create initiative"), nil, nil
		}

		// Get the initiative to check tier and tags
		initiative, err := k.Initiative.Get(ctx, initID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "failed to get initiative"), nil, nil
		}

		// Get the project to determine its creator (the authorized assigner).
		project, err := k.Project.Get(ctx, initiative.ProjectId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "failed to get project"), nil, nil
		}

		// If the initiative came from an existing project (not the one we
		// just created with `creator`), the signer must match the project
		// creator. Look up the matching simAccount.
		signerAcc := creatorAcc
		signerAddr := creator.Address
		if project.Creator != creator.Address {
			projectCreatorMember := &types.Member{Address: project.Creator}
			acc, found := getAccountFromMember(projectCreatorMember, accs)
			if !found {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "project creator not in sim accounts"), nil, nil
			}
			signerAcc = acc
			signerAddr = project.Creator
		}

		// Get or create an assignee with appropriate reputation for the initiative tier
		// Try a few times to ensure we don't get the project creator
		var assignee *types.Member
		for i := 0; i < 5; i++ {
			assignee, _, err = getOrCreateMemberWithReputation(r, ctx, k, accs, initiative.Tier, initiative.Tags)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "failed to get/create assignee with reputation"), nil, nil
			}
			// Make sure assignee is not the project creator
			if assignee.Address != project.Creator {
				break
			}
		}
		// If we still got the project creator after 5 tries, skip this operation
		if assignee.Address == project.Creator {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAssignInitiative{}), "assignee cannot be project creator"), nil, nil
		}

		msg := &types.MsgAssignInitiative{
			Creator:      signerAddr,
			InitiativeId: initID,
			Assignee:     assignee.Address,
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
