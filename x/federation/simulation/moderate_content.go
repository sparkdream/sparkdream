package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgModerateContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create verified content to moderate (hide)
		content, contentID, err := getOrCreateVerifiedContent(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgModerateContent{}), "failed to get/create verified content"), nil, nil
		}

		// Toggle: if active/verified hide it, if hidden unhide it
		if content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE
		} else {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
		}

		if err := k.Content.Set(ctx, contentID, content); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgModerateContent{}), "failed to update content"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgModerateContent{}), "ok (direct keeper call)"), nil, nil
	}
}
