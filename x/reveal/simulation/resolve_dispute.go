package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

func SimulateMsgResolveDispute(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgResolveDispute{}

		// Find or create a contribution with a DISPUTED tranche
		simAccount, _ := simtypes.RandomAcc(r, accs)
		stakerAcc, found := pickDifferentAccount(r, accs, simAccount.Address.String())
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no different account for staker"), nil, nil
		}

		contribID, trancheID, err := getOrCreateDisputedContribution(r, ctx, k, simAccount.Address.String(), stakerAcc.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get/create disputed contribution: "+err.Error()), nil, nil
		}

		contrib, err := k.Contribution.Get(ctx, contribID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get contribution"), nil, nil
		}

		if int(trancheID) >= len(contrib.Tranches) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "tranche out of range"), nil, nil
		}

		if contrib.Tranches[trancheID].Status != types.TrancheStatus_TRANCHE_STATUS_DISPUTED {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "tranche not disputed"), nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get params"), nil, nil
		}

		// Pick a random verdict
		verdict := randomDisputeVerdict(r)
		tranche := &contrib.Tranches[trancheID]

		switch verdict {
		case types.DisputeVerdict_DISPUTE_VERDICT_ACCEPT:
			// Tranche verified (simplified — skip payout in sim since repKeeper not wired)
			tranche.Status = types.TrancheStatus_TRANCHE_STATUS_VERIFIED
			tranche.VerifiedAt = ctx.BlockHeight()

			// Check if all tranches verified — complete contribution
			allVerified := true
			nextTranche := -1
			for i := range contrib.Tranches {
				if contrib.Tranches[i].Status != types.TrancheStatus_TRANCHE_STATUS_VERIFIED {
					allVerified = false
					if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED && nextTranche == -1 {
						nextTranche = i
					}
				}
			}

			if allVerified {
				if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove status index"), nil, nil
				}
				contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED
				contrib.BondRemaining = math.ZeroInt()
				contrib.HoldbackAmount = math.ZeroInt()
				if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save status index"), nil, nil
				}
			} else if nextTranche >= 0 {
				// Unlock next tranche for staking
				contrib.Tranches[nextTranche].Status = types.TrancheStatus_TRANCHE_STATUS_STAKING
				contrib.Tranches[nextTranche].StakeDeadline = ctx.BlockHeight() + params.StakeDeadlineEpochs
				contrib.CurrentTranche = uint32(nextTranche)
			}

		case types.DisputeVerdict_DISPUTE_VERDICT_IMPROVE:
			// Return to BACKED for re-reveal
			tranche.Status = types.TrancheStatus_TRANCHE_STATUS_BACKED
			tranche.CodeUri = ""
			tranche.DocsUri = ""
			tranche.CommitHash = ""
			tranche.RevealedAt = 0
			tranche.VerificationDeadline = 0
			tranche.RevealDeadline = ctx.BlockHeight() + params.RevealDeadlineEpochs

		case types.DisputeVerdict_DISPUTE_VERDICT_REJECT:
			// Hard fail — slash bond, cancel remaining
			slashAmount := contrib.BondRemaining.Quo(math.NewInt(2))
			contrib.BondRemaining = contrib.BondRemaining.Sub(slashAmount)
			contrib.HoldbackAmount = math.ZeroInt()

			tranche.Status = types.TrancheStatus_TRANCHE_STATUS_FAILED

			// Cancel remaining locked tranches
			for i := range contrib.Tranches {
				if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED {
					contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
				}
			}

			if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove status index"), nil, nil
			}
			contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
			if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contribID)); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save status index"), nil, nil
			}
		}

		if err := k.Contribution.Set(ctx, contribID, contrib); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save contribution"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
