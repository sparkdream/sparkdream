package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CompleteInterim(ctx context.Context, msg *types.MsgCompleteInterim) (*types.MsgCompleteInterimResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Get the interim to check authorization
	interim, err := k.Keeper.GetInterim(ctx, msg.InterimId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "interim not found")
	}

	// Authorization check based on interim type
	if interim.Type == types.InterimType_INTERIM_TYPE_ADJUDICATION {
		// An ADJUDICATION verdict upholds or rejects a challenge (slashing
		// one side), so it needs an executed proposal: the Commons Operations
		// Committee policy, the Commons Council policy or x/gov, never a
		// single committee member.
		if !k.Keeper.isCouncilOrCommitteePolicy(ctx, msg.Creator, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotGovAuthority,
				fmt.Sprintf("only the Commons Operations Committee policy (a committee proposal), the Commons Council policy or x/gov can complete ADJUDICATION interims (creator: %s)", msg.Creator))
		}
		// The verdict is structured, not parsed from the notes: a free-text
		// keyword that failed to match used to strand the challenged initiative
		// forever (see CompleteInterimDirectly).
		if interim.ReferenceId != 0 && msg.Decision == types.AdjudicationDecision_ADJUDICATION_DECISION_UNSPECIFIED {
			return nil, errorsmod.Wrap(types.ErrInvalidRequest,
				"completing an ADJUDICATION interim requires a decision: ADJUDICATION_DECISION_UPHOLD or ADJUDICATION_DECISION_REJECT")
		}
	} else {
		// Every other interim is paid only through MsgApproveInterim, by the
		// Operations Committee and never by an assignee (REP-E-02, REP-E-05).
		// Completing one here let the payee certify their own payment: a
		// member could create an EXPERT interim and collect 1,500 DREAM in
		// the next transaction, or farm reputation up the trust ladder, and a
		// seated juror could take the jury-duty budget without voting.
		return nil, errorsmod.Wrapf(types.ErrInterimNeedsApproval,
			"interim %d is %s; submit the work with MsgSubmitInterimWork and the Operations Committee approves it with MsgApproveInterim",
			interim.Id, interim.Type)
	}

	// Complete the interim directly using the keeper method
	if err := k.Keeper.CompleteInterimDirectly(ctx, msg.InterimId, msg.Decision, msg.CompletionNotes); err != nil {
		return nil, errorsmod.Wrap(err, "failed to complete interim")
	}

	return &types.MsgCompleteInterimResponse{}, nil
}
