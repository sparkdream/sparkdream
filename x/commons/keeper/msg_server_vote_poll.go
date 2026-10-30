package keeper

import (
	"context"
	"strconv"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// VotePoll casts a member's open vote on a poll: one vote per member
// (alias-aware, so a migrated key does not vote again), final. The voter
// must be a member of the group when voting.
func (k msgServer) VotePoll(goCtx context.Context, msg *types.MsgVotePoll) (*types.MsgVotePollResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := k.addressCodec.StringToBytes(msg.Voter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid voter address")
	}

	poll, err := k.openPollForVoting(ctx, msg.PollId, msg.Option)
	if err != nil {
		return nil, err
	}
	if poll.SecretBallot {
		return nil, errorsmod.Wrapf(types.ErrBallotMode, "poll %d is a secret ballot; vote with MsgAnonymousVotePoll through x/shield", msg.PollId)
	}
	isMember, err := k.HasMember(ctx, poll.CouncilName, msg.Voter)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "%s is not a member of %s", msg.Voter, poll.CouncilName)
	}

	voted := false
	if err := k.PollVotes.Walk(ctx, collections.NewPrefixedPairRange[uint64, string](msg.PollId),
		func(_ collections.Pair[uint64, string], v types.PollVote) (bool, error) {
			voted = k.canActAs(ctx, msg.Voter, v.Voter)
			return voted, nil
		}); err != nil {
		return nil, err
	}
	if voted {
		return nil, errorsmod.Wrapf(types.ErrAlreadyVoted, "poll %d", msg.PollId)
	}

	if err := k.PollVotes.Set(ctx, collections.Join(msg.PollId, msg.Voter), types.PollVote{
		PollId:  msg.PollId,
		Voter:   msg.Voter,
		Option:  msg.Option,
		VotedAt: ctx.BlockTime().Unix(),
	}); err != nil {
		return nil, err
	}
	poll.Tallies[msg.Option]++
	if err := k.Polls.Set(ctx, msg.PollId, poll); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(pollEvent("poll_vote", msg.PollId,
		sdk.NewAttribute("voter", msg.Voter),
		sdk.NewAttribute("option", strconv.FormatUint(msg.Option, 10)),
	))
	return &types.MsgVotePollResponse{}, nil
}
