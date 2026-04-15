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

func SimulateMsgChallengeVerification(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need verified content to challenge
		content, contentID, err := getOrCreateVerifiedContent(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgChallengeVerification{}), "failed to get/create verified content"), nil, nil
		}

		// Mark as challenged
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgChallengeVerification{}), "failed to update content"), nil, nil
		}

		// Update verification record outcome if it exists
		rec, err := k.VerificationRecords.Get(ctx, contentID)
		if err == nil {
			rec.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CHALLENGED
			_ = k.VerificationRecords.Set(ctx, contentID, rec)
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgChallengeVerification{}), "ok (direct keeper call)"), nil, nil
	}
}
