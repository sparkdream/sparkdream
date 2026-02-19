package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func SimulateMsgUpdateCollaboratorRole(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgUpdateCollaboratorRole{
			Creator: simAccount.Address.String(),
		}

		collab, collabKey, err := findAnyCollaborator(r, ctx, k)
		if err != nil || collab == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no collaborator found"), nil, nil
		}

		// Toggle role: EDITOR -> ADMIN, ADMIN -> EDITOR
		if collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR {
			collab.Role = types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN
		} else {
			collab.Role = types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR
		}

		if err := k.Collaborator.Set(ctx, collabKey, *collab); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update collaborator role: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
