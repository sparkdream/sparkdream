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

func SimulateMsgVote(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgVote{}

		// Find or create an ACTIVE PUBLIC proposal
		simAccount, _ := simtypes.RandomAcc(r, accs)
		proposalID, err := getOrCreateActiveProposal(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get/create proposal: "+err.Error()), nil, nil
		}

		proposal, err := k.VotingProposal.Get(ctx, proposalID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get proposal"), nil, nil
		}

		// Generate a unique nullifier for this vote (regenerate on collision)
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

		// Pick random vote option
		voteOption := uint32(r.Intn(len(proposal.Options)))

		// Create the vote directly
		vote := types.AnonymousVote{
			ProposalId:  proposalID,
			Nullifier:   nullifier,
			VoteOption:  voteOption,
			Proof:       randomProof(r),
			SubmittedAt: ctx.BlockHeight(),
		}

		if err := k.AnonymousVote.Set(ctx, nullKey, vote); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save vote"), nil, nil
		}
		if err := k.UsedNullifier.Set(ctx, nullKey, types.UsedNullifier{
			Index:      nullKey,
			ProposalId: proposalID,
			Nullifier:  nullifier,
			UsedAt:     ctx.BlockHeight(),
		}); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to mark nullifier used"), nil, nil
		}

		// Update tally
		if int(voteOption) < len(proposal.Tally) {
			proposal.Tally[voteOption].VoteCount++
			if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update tally"), nil, nil
			}
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
