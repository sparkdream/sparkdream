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

func SimulateMsgCancelProposal(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelProposal{}

		// Find an ACTIVE proposal to cancel
		proposal, err := findProposal(r, ctx, k, types.ProposalStatus_PROPOSAL_STATUS_ACTIVE)
		if err != nil || proposal == nil {
			// Create one to cancel
			simAccount, _ := simtypes.RandomAcc(r, accs)
			proposalID, createErr := getOrCreateActiveProposal(r, ctx, k, simAccount.Address.String())
			if createErr != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create proposal for cancel"), nil, nil
			}
			p, getErr := k.VotingProposal.Get(ctx, proposalID)
			if getErr != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get created proposal"), nil, nil
			}
			proposal = &p
		}

		// Cancel the proposal
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_CANCELLED
		proposal.FinalizedAt = ctx.BlockHeight()
		proposal.Outcome = types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED

		if err := k.VotingProposal.Set(ctx, proposal.Id, *proposal); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to cancel proposal"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
