package keeper

import (
	"context"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// CancelPoll lets the creator withdraw an open poll before anyone votes.
func (k msgServer) CancelPoll(goCtx context.Context, msg *types.MsgCancelPoll) (*types.MsgCancelPollResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	poll, err := k.Polls.Get(ctx, msg.PollId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPollNotFound, "poll %d", msg.PollId)
	}
	if !k.canActAs(ctx, msg.Creator, poll.Creator) {
		return nil, errorsmod.Wrap(sdkerrors.ErrUnauthorized, "only the poll's creator can cancel it")
	}
	if poll.Status != types.PollStatus_POLL_STATUS_OPEN {
		return nil, errorsmod.Wrapf(types.ErrPollNotOpen, "poll %d", msg.PollId)
	}
	if pollVoteTotal(poll) > 0 {
		return nil, errorsmod.Wrapf(types.ErrPollHasVotes, "poll %d", msg.PollId)
	}
	if err := k.endPoll(ctx, &poll, types.PollStatus_POLL_STATUS_CANCELLED); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(pollEvent("poll_cancelled", msg.PollId))
	return &types.MsgCancelPollResponse{}, nil
}
