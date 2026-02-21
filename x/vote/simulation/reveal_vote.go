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

func SimulateMsgRevealVote(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRevealVote{}

		// Find or create a TALLYING proposal with unrevealed sealed votes
		simAccount, _ := simtypes.RandomAcc(r, accs)
		proposalID, err := getOrCreateTallyingProposal(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get/create tallying proposal: "+err.Error()), nil, nil
		}

		// Try to find an unrevealed sealed vote on this proposal
		sv, err := findSealedVote(r, ctx, k, proposalID, false)
		if err != nil || sv == nil {
			// Create a sealed vote first, then reveal it
			nullifier := randomNullifier(r)
			sealedVote := types.SealedVote{
				ProposalId:     proposalID,
				Nullifier:      nullifier,
				VoteCommitment: randomVoteCommitment(r),
				Proof:          randomProof(r),
				SubmittedAt:    ctx.BlockHeight(),
				Revealed:       false,
			}
			svk := keeper.NullifierKey(proposalID, nullifier)
			if err := k.SealedVote.Set(ctx, svk, sealedVote); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create sealed vote for reveal"), nil, nil
			}
			sv = &sealedVote
		}

		proposal, err := k.VotingProposal.Get(ctx, proposalID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get proposal"), nil, nil
		}

		// Reveal the vote: mark as revealed and pick a random option
		voteOption := uint32(0)
		if len(proposal.Options) > 0 {
			voteOption = uint32(r.Intn(len(proposal.Options)))
		}

		sv.Revealed = true
		sv.RevealedOption = voteOption
		sv.RevealSalt = randomZKPublicKey(r)

		svk := keeper.NullifierKey(proposalID, sv.Nullifier)
		if err := k.SealedVote.Set(ctx, svk, *sv); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save revealed vote"), nil, nil
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
