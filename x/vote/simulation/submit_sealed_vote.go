package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func SimulateMsgSealedVote(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSealedVote{}

		// Find or create an ACTIVE SEALED proposal
		simAccount, _ := simtypes.RandomAcc(r, accs)
		proposalID, err := getOrCreateSealedProposal(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get/create sealed proposal: "+err.Error()), nil, nil
		}

		// Generate a unique nullifier (regenerate on collision)
		nullifier := randomNullifier(r)
		nullKey := keeper.NullifierKey(proposalID, nullifier)
		for {
			used, _ := k.UsedNullifier.Has(ctx, nullKey)
			if !used {
				break
			}
			nullifier = randomNullifier(r)
			nullKey = keeper.NullifierKey(proposalID, nullifier)
		}

		// Create the sealed vote with commitment
		sealedVote := types.SealedVote{
			ProposalId:     proposalID,
			Nullifier:      nullifier,
			VoteCommitment: randomVoteCommitment(r),
			Proof:          randomProof(r),
			SubmittedAt:    ctx.BlockHeight(),
			Revealed:       false,
		}

		if err := k.SealedVote.Set(ctx, nullKey, sealedVote); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save sealed vote"), nil, nil
		}
		if err := k.UsedNullifier.Set(ctx, nullKey, types.UsedNullifier{
			Index:      nullKey,
			ProposalId: proposalID,
			Nullifier:  nullifier,
			UsedAt:     ctx.BlockHeight(),
		}); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to mark nullifier used"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
