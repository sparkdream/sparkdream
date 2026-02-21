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

func (k msgServer) Vote(ctx context.Context, msg *types.MsgVote) (*types.MsgVoteResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	proposal, err := k.VotingProposal.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, types.ErrProposalNotFound
	}

	if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_ACTIVE {
		return nil, types.ErrProposalNotActive
	}

	// PUBLIC mode only.
	if proposal.Visibility != types.VisibilityLevel_VISIBILITY_PUBLIC {
		return nil, errorsmod.Wrap(types.ErrInvalidVisibility, "MsgVote is only for PUBLIC visibility proposals; use MsgSealedVote for SEALED/PRIVATE")
	}

	// Validate vote option in range.
	if msg.VoteOption >= uint32(len(proposal.Options)) {
		return nil, types.ErrVoteOptionOutOfRange
	}

	// Check nullifier not used.
	if k.isNullifierUsed(ctx, msg.ProposalId, msg.Nullifier) {
		return nil, types.ErrNullifierUsed
	}

	// Verify merkle root matches snapshot.
	snapshot, err := k.VoterTreeSnapshot.Get(ctx, msg.ProposalId)
	if err == nil && !bytes.Equal(snapshot.MerkleRoot, proposal.MerkleRoot) {
		return nil, types.ErrMerkleRootMismatch
	}

	// Verify ZK proof (stub).
	if err := verifyVoteProof(ctx, params.VoteVerifyingKey, proposal.MerkleRoot, msg.Nullifier, msg.VoteOption, msg.Proof); err != nil {
		return nil, types.ErrInvalidProof
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Record nullifier.
	if err := k.recordNullifier(ctx, msg.ProposalId, msg.Nullifier); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record nullifier")
	}

	// Store anonymous vote.
	voteKey := nullifierKey(msg.ProposalId, msg.Nullifier)
	vote := types.AnonymousVote{
		Index:       voteKey,
		ProposalId:  msg.ProposalId,
		Nullifier:   msg.Nullifier,
		VoteOption:  msg.VoteOption,
		Proof:       msg.Proof,
		SubmittedAt: sdkCtx.BlockHeight(),
	}
	if err := k.AnonymousVote.Set(ctx, voteKey, vote); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store anonymous vote")
	}

	// Update tally.
	if err := k.updateTally(ctx, &proposal, msg.VoteOption); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update tally")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventVoteCast,
		sdk.NewAttribute(types.AttributeProposalID, fmt.Sprintf("%d", msg.ProposalId)),
		sdk.NewAttribute(types.AttributeNullifier, hex.EncodeToString(msg.Nullifier)),
		sdk.NewAttribute(types.AttributeVoteOption, fmt.Sprintf("%d", msg.VoteOption)),
	))

	return &types.MsgVoteResponse{}, nil
}
