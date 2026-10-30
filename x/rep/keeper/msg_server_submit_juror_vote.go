package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) SubmitJurorVote(ctx context.Context, msg *types.MsgSubmitJurorVote) (*types.MsgSubmitJurorVoteResponse, error) {
	jurorAddr, err := k.addressCodec.StringToBytes(msg.Juror)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid juror address")
	}

	// Validate confidence
	if msg.Confidence == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "confidence is required")
	}
	// Only a deciding verdict with a confidence in [0, 1] is a vote
	// (REP-E-23). The keeper re-checks, for callers that bypass this server.
	if err := types.ValidateJurorVote(msg.Verdict, *msg.Confidence); err != nil {
		return nil, err
	}

	// Submit the juror vote
	if err := k.Keeper.SubmitJurorVote(
		ctx,
		msg.JuryReviewId,
		jurorAddr,
		msg.CriteriaVotes,
		msg.Verdict,
		*msg.Confidence,
		msg.Reasoning,
	); err != nil {
		return nil, err
	}

	return &types.MsgSubmitJurorVoteResponse{}, nil
}
