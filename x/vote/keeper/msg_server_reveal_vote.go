package keeper

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RevealVote(ctx context.Context, msg *types.MsgRevealVote) (*types.MsgRevealVoteResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	proposal, err := k.VotingProposal.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, types.ErrProposalNotFound
	}

	if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_TALLYING {
		return nil, types.ErrProposalNotTallying
	}

	// Load sealed vote by nullifier.
	voteKey := nullifierKey(msg.ProposalId, msg.Nullifier)
	sealedVote, err := k.Keeper.SealedVote.Get(ctx, voteKey)
	if err != nil {
		return nil, types.ErrVoteNotFound
	}

	if sealedVote.Revealed {
		return nil, types.ErrAlreadyRevealed
	}

	// Verify hash(vote_option, salt) == commitment.
	expectedCommitment := computeCommitmentHash(msg.VoteOption, msg.RevealSalt)
	if !bytes.Equal(expectedCommitment, sealedVote.VoteCommitment) {
		return nil, types.ErrRevealMismatch
	}

	// Validate vote option in range.
	if msg.VoteOption >= uint32(len(proposal.Options)) {
		return nil, types.ErrVoteOptionOutOfRange
	}

	// Mark revealed, store option and salt.
	sealedVote.Revealed = true
	sealedVote.RevealedOption = msg.VoteOption
	sealedVote.RevealSalt = msg.RevealSalt

	if err := k.Keeper.SealedVote.Set(ctx, voteKey, sealedVote); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update sealed vote")
	}

	// Update tally.
	if err := k.updateTally(ctx, &proposal, msg.VoteOption); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tally")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventSealedVoteRevealed,
		sdk.NewAttribute(types.AttributeProposalID, fmt.Sprintf("%d", msg.ProposalId)),
		sdk.NewAttribute(types.AttributeNullifier, hex.EncodeToString(msg.Nullifier)),
		sdk.NewAttribute(types.AttributeVoteOption, fmt.Sprintf("%d", msg.VoteOption)),
	))

	return &types.MsgRevealVoteResponse{}, nil
}
