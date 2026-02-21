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

func SimulateMsgCreateAnonymousProposal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCreateAnonymousProposal{}

		// Pick a random account as anonymous proposer
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Ensure proposer is registered
		if err := getOrCreateVoterRegistration(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to register voter"), nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get params"), nil, nil
		}

		proposalID, err := k.VotingProposalSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get next proposal ID"), nil, nil
		}

		// Create proposal with mock nullifier (skip ZK proof verification)
		mockNullifier := randomNullifier(r)

		options := randomVoteOptions()
		tally := make([]*types.VoteTally, len(options))
		for i, opt := range options {
			tally[i] = &types.VoteTally{OptionId: opt.Id, VoteCount: 0}
		}

		proposal := types.VotingProposal{
			Id:                proposalID,
			Title:             randomProposalTitle(r),
			Description:       randomProposalDescription(r),
			ProposerNullifier: mockNullifier,
			MerkleRoot:        randomNullifier(r),
			SnapshotBlock:     ctx.BlockHeight(),
			EligibleVoters:    100,
			Options:           options,
			VotingStart:       ctx.BlockHeight(),
			VotingEnd:         ctx.BlockHeight() + params.DefaultVotingPeriodEpochs,
			Quorum:            params.DefaultQuorum,
			Threshold:         params.DefaultThreshold,
			Tally:             tally,
			Status:            types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
			Outcome:           types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED,
			ProposalType:      types.ProposalType_PROPOSAL_TYPE_GENERAL,
			CreatedAt:         ctx.BlockHeight(),
			Visibility:        types.VisibilityLevel_VISIBILITY_PUBLIC,
		}

		if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save proposal"), nil, nil
		}

		snapshot := types.VoterTreeSnapshot{
			ProposalId:    proposalID,
			MerkleRoot:    proposal.MerkleRoot,
			SnapshotBlock: ctx.BlockHeight(),
			VoterCount:    100,
		}
		if err := k.VoterTreeSnapshot.Set(ctx, proposalID, snapshot); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to save snapshot"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
