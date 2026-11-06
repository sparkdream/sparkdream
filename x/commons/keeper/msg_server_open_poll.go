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

// OpenPoll opens a non-binding, single-choice poll for a group's members.
// The creator must be a member and the group's term must not have expired.
// A secret-ballot poll snapshots the group's voters now (equal member
// weights, at least MinSecretBallotVoters registered ZK keys).
func (k msgServer) OpenPoll(goCtx context.Context, msg *types.MsgOpenPoll) (*types.MsgOpenPollResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	group, err := k.Groups.Get(ctx, msg.CouncilName)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound, "%s", msg.CouncilName)
	}
	now := ctx.BlockTime().Unix()
	if isTermExpired(group, now) {
		return nil, errorsmod.Wrapf(types.ErrGroupExpired, "group %s expired on %d", msg.CouncilName, group.CurrentTermExpiration)
	}
	isMember, err := k.HasMember(ctx, msg.CouncilName, msg.Creator)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "%s is not a member of %s", msg.Creator, msg.CouncilName)
	}
	if err := validatePollContent(msg.Question, msg.Options, msg.DurationSeconds); err != nil {
		return nil, err
	}
	if err := k.checkOpenPollCaps(ctx, msg.CouncilName, msg.Creator); err != nil {
		return nil, err
	}
	if err := k.recordPollOpen(ctx, msg.Creator); err != nil {
		return nil, err
	}

	pollID, err := k.PollSeq.Next(ctx)
	if err == nil && pollID == 0 {
		// IDs start at 1 (0 reads as "no poll"); InitGenesis primes the
		// sequence, this covers a store that skipped it.
		pollID, err = k.PollSeq.Next(ctx)
	}
	if err != nil {
		return nil, err
	}
	poll := types.Poll{
		Id:           pollID,
		CouncilName:  msg.CouncilName,
		Creator:      msg.Creator,
		Question:     msg.Question,
		Options:      msg.Options,
		SecretBallot: msg.SecretBallot,
		OpenedAt:     now,
		Deadline:     now + msg.DurationSeconds,
		Status:       types.PollStatus_POLL_STATUS_OPEN,
		Tallies:      make([]uint64, len(msg.Options)),
	}
	if msg.SecretBallot {
		tree, err := k.buildVoterTree(ctx, msg.CouncilName)
		if err != nil {
			return nil, err
		}
		if err := k.PollVoterTrees.Set(ctx, pollID, tree); err != nil {
			return nil, err
		}
		poll.VoterRoot = tree.Root
		poll.EligibleVoters = uint64(len(tree.Leaves))
	} else {
		members, err := k.GetCouncilMembers(ctx, msg.CouncilName)
		if err != nil {
			return nil, err
		}
		poll.EligibleVoters = uint64(len(members))
	}

	if err := k.Polls.Set(ctx, pollID, poll); err != nil {
		return nil, err
	}
	if err := k.PollsByCouncil.Set(ctx, collections.Join(msg.CouncilName, pollID)); err != nil {
		return nil, err
	}
	if err := k.OpenPollsByCouncil.Set(ctx, collections.Join(msg.CouncilName, pollID)); err != nil {
		return nil, err
	}
	if err := k.PollQueue.Set(ctx, collections.Join(poll.Deadline, pollID)); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(pollEvent("poll_opened", pollID,
		sdk.NewAttribute("council_name", msg.CouncilName),
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("deadline", strconv.FormatInt(poll.Deadline, 10)),
		sdk.NewAttribute("secret_ballot", strconv.FormatBool(poll.SecretBallot)),
	))
	return &types.MsgOpenPollResponse{PollId: pollID}, nil
}
