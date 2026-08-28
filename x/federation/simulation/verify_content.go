package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

func SimulateMsgVerifyContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		addr := simAccount.Address.String()

		// Need pending content
		content, contentID, err := getOrCreatePendingContent(r, ctx, k, addr)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVerifyContent{}), "failed to get/create pending content"), nil, nil
		}

		// Create verification record — committed bond reservation against the
		// rep BondedRole is skipped in sim (cross-module state seeding not
		// supported); the corresponding msg handler does reserve bond at
		// runtime.
		commitAmount := math.NewInt(int64(r.Intn(50) + 10))
		record := types.VerificationRecord{
			ContentId:            contentID,
			Verifier:             addr,
			VerifierHash:         content.ContentHash,
			VerifiedAt:           ctx.BlockTime().Unix(),
			ChallengeWindowEnds:  ctx.BlockTime().Unix() + int64(types.DefaultParams().ChallengeWindow.Seconds()),
			CommittedAmount:      commitAmount,
			VerifierBondSnapshot: math.ZeroInt(),
			EscrowedChallengeFee: math.ZeroInt(),
			Outcome:              types.VerificationOutcome_VERIFICATION_OUTCOME_PENDING,
		}

		if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVerifyContent{}), "failed to set verification record"), nil, nil
		}

		// Update content status to VERIFIED
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVerifyContent{}), "failed to update content"), nil, nil
		}

		// Report the verification to x/rep, which owns the per-kind counters.
		// Best-effort: the simulation drives the keeper directly and a rep
		// keeper may not be wired.
		_ = k.ReportSimulatedVerification(ctx, addr)

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgVerifyContent{}), "ok (direct keeper call)"), nil, nil
	}
}
