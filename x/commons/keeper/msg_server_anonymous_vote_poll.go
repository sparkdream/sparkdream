package keeper

import (
	"context"
	"strconv"

	"sparkdream/x/commons/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// AnonymousVotePoll counts one secret-ballot poll vote. It runs only inside
// x/shield's MsgShieldedExec, whose proof shows the voter is in the poll's
// voter snapshot (ShieldMerkleRoot) and whose nullifier, scoped to the poll,
// lets each member vote once.
func (k msgServer) AnonymousVotePoll(goCtx context.Context, msg *types.MsgAnonymousVotePoll) (*types.MsgAnonymousVotePollResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	shieldModuleAddr := k.authKeeper.GetModuleAddress("shield")
	if shieldModuleAddr == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "shield module address not found")
	}
	if msg.Voter != shieldModuleAddr.String() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"only the shield module account can cast anonymous poll votes (got %s, want %s)",
			msg.Voter, shieldModuleAddr.String())
	}

	poll, err := k.openPollForVoting(ctx, msg.PollId, msg.Option)
	if err != nil {
		return nil, err
	}
	if !poll.SecretBallot {
		return nil, errorsmod.Wrapf(types.ErrBallotMode, "poll %d is voted openly", msg.PollId)
	}
	poll.Tallies[msg.Option]++
	if err := k.Polls.Set(ctx, msg.PollId, poll); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(pollEvent("anonymous_poll_vote", msg.PollId,
		sdk.NewAttribute("option", strconv.FormatUint(msg.Option, 10)),
	))
	return &types.MsgAnonymousVotePollResponse{}, nil
}
