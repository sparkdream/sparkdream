package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) SealedVote(ctx context.Context, msg *types.MsgSealedVote) (*types.MsgSealedVoteResponse, error) {
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

	// SEALED or PRIVATE mode only.
	if proposal.Visibility != types.VisibilityLevel_VISIBILITY_SEALED &&
		proposal.Visibility != types.VisibilityLevel_VISIBILITY_PRIVATE {
		return nil, errorsmod.Wrap(types.ErrInvalidVisibility, "MsgSealedVote is only for SEALED/PRIVATE visibility proposals")
	}

	// Validate commitment non-empty.
	if len(msg.VoteCommitment) == 0 {
		return nil, types.ErrInvalidCommitment
	}

	// Check nullifier not used.
	if k.isNullifierUsed(ctx, msg.ProposalId, msg.Nullifier) {
		return nil, types.ErrNullifierUsed
	}

	// Verify ZK proof (stub).
	if err := verifyVoteProof(ctx, params.VoteVerifyingKey, proposal.MerkleRoot, msg.Nullifier, 0, msg.Proof); err != nil {
		return nil, types.ErrInvalidProof
	}

	// Check encrypted reveal size.
	if len(msg.EncryptedReveal) > int(params.MaxEncryptedRevealBytes) {
		return nil, types.ErrEncryptedRevealTooLarge
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Record nullifier.
	if err := k.recordNullifier(ctx, msg.ProposalId, msg.Nullifier); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record nullifier")
	}

	// Store sealed vote.
	voteKey := nullifierKey(msg.ProposalId, msg.Nullifier)
	sealedVote := types.SealedVote{
		Index:           voteKey,
		ProposalId:      msg.ProposalId,
		Nullifier:       msg.Nullifier,
		VoteCommitment:  msg.VoteCommitment,
		Proof:           msg.Proof,
		SubmittedAt:     sdkCtx.BlockHeight(),
		EncryptedReveal: msg.EncryptedReveal,
		Revealed:        false,
	}
	if err := k.Keeper.SealedVote.Set(ctx, voteKey, sealedVote); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store sealed vote")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventSealedVoteCast,
		sdk.NewAttribute(types.AttributeProposalID, fmt.Sprintf("%d", msg.ProposalId)),
		sdk.NewAttribute(types.AttributeNullifier, hex.EncodeToString(msg.Nullifier)),
	))

	return &types.MsgSealedVoteResponse{}, nil
}
