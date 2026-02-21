package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	"cosmossdk.io/math"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateAnonymousProposal(ctx context.Context, msg *types.MsgCreateAnonymousProposal) (*types.MsgCreateAnonymousProposalResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Submitter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid submitter address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Epoch validation: claimed_epoch must be current epoch +/- 1.
	currentEpoch := k.seasonKeeper.GetCurrentEpoch(ctx)
	if currentEpoch < 0 {
		currentEpoch = 0
	}
	claimedEpoch := int64(msg.ClaimedEpoch)
	if claimedEpoch < currentEpoch-1 || claimedEpoch > currentEpoch+1 {
		return nil, types.ErrEpochMismatch
	}

	// Proposal nullifier check.
	if k.isProposalNullifierUsed(ctx, msg.ClaimedEpoch, msg.Nullifier) {
		return nil, types.ErrProposalLimitReached
	}

	// ZK proof verification (stub).
	if err := verifyProposalProof(ctx, params.ProposalVerifyingKey, nil, msg.Nullifier, msg.Proof); err != nil {
		return nil, types.ErrInvalidProof
	}

	// Check visibility restrictions.
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_PRIVATE && !params.AllowPrivateProposals {
		return nil, types.ErrPrivateNotAllowed
	}
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_SEALED && !params.AllowSealedProposals {
		return nil, types.ErrSealedNotAllowed
	}

	// Validate vote options.
	if err := k.validateProposalOptions(params, msg.Options); err != nil {
		return nil, err
	}

	// Resolve quorum/threshold/vetoThreshold.
	quorum := params.DefaultQuorum
	if !msg.Quorum.IsNil() && msg.Quorum.IsPositive() {
		quorum = msg.Quorum
	}
	threshold := params.DefaultThreshold
	if !msg.Threshold.IsNil() && msg.Threshold.IsPositive() {
		threshold = msg.Threshold
	}
	vetoThreshold := params.DefaultVetoThreshold
	if !msg.VetoThreshold.IsNil() && msg.VetoThreshold.IsPositive() {
		vetoThreshold = msg.VetoThreshold
	}

	one := math.LegacyOneDec()
	if quorum.GT(one) || threshold.GT(one) || vetoThreshold.GT(one) {
		return nil, types.ErrInvalidThreshold
	}

	// Resolve voting period.
	votingPeriodEpochs := params.DefaultVotingPeriodEpochs
	if msg.VotingPeriodEpochs > 0 {
		votingPeriodEpochs = msg.VotingPeriodEpochs
	}
	if votingPeriodEpochs < params.MinVotingPeriodEpochs || votingPeriodEpochs > params.MaxVotingPeriodEpochs {
		return nil, types.ErrVotingPeriodOutOfRange
	}

	// Build tree snapshot.
	merkleRoot, voterCount, err := k.buildTreeSnapshot(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to build voter tree")
	}
	if voterCount == 0 {
		return nil, types.ErrNoEligibleVoters
	}

	// For PRIVATE visibility, check voter count limit.
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_PRIVATE {
		if voterCount > uint64(params.MaxPrivateEligibleVoters) {
			return nil, types.ErrTooManyEligibleVoters
		}
	}

	// Allocate proposal ID.
	proposalID, err := k.VotingProposalSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to allocate proposal ID")
	}

	// Compute timing.
	blocksPerEpoch := k.getBlocksPerEpoch(ctx)
	votingStart := sdkCtx.BlockHeight()
	votingEnd := votingStart + votingPeriodEpochs*blocksPerEpoch
	revealEpoch := uint64(0)
	revealEnd := votingEnd
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_SEALED || msg.Visibility == types.VisibilityLevel_VISIBILITY_PRIVATE {
		revealEpoch = uint64(votingEnd / blocksPerEpoch)
		revealEnd = votingEnd + params.SealedRevealPeriodEpochs*blocksPerEpoch
	}

	// Record proposal nullifier.
	if err := k.recordProposalNullifier(ctx, msg.ClaimedEpoch, msg.Nullifier); err != nil {
		return nil, errorsmod.Wrap(err, "failed to record proposal nullifier")
	}

	proposal := types.VotingProposal{
		Id:                proposalID,
		Title:             msg.Title,
		Description:       msg.Description,
		EncryptedContent:  msg.EncryptedContent,
		ContentNonce:      msg.ContentNonce,
		ProposerNullifier: msg.Nullifier,
		MerkleRoot:        merkleRoot,
		SnapshotBlock:     sdkCtx.BlockHeight(),
		EligibleVoters:    voterCount,
		Options:           msg.Options,
		VotingStart:       votingStart,
		VotingEnd:         votingEnd,
		Quorum:            quorum,
		Threshold:         threshold,
		VetoThreshold:     vetoThreshold,
		Tally:             initTally(msg.Options),
		Status:            types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
		Outcome:           types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED,
		ProposalType:      msg.ProposalType,
		ReferenceId:       msg.ReferenceId,
		CreatedAt:         sdkCtx.BlockHeight(),
		Visibility:        msg.Visibility,
		KeyShares:         msg.KeyShares,
		Messages:          msg.Messages,
		RevealEpoch:       revealEpoch,
		RevealEnd:         revealEnd,
	}

	if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store proposal")
	}

	snapshot := types.VoterTreeSnapshot{
		ProposalId:    proposalID,
		MerkleRoot:    merkleRoot,
		SnapshotBlock: sdkCtx.BlockHeight(),
		VoterCount:    voterCount,
	}
	if err := k.VoterTreeSnapshot.Set(ctx, proposalID, snapshot); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store voter tree snapshot")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventProposalCreated,
		sdk.NewAttribute(types.AttributeProposalID, fmt.Sprintf("%d", proposalID)),
		sdk.NewAttribute(types.AttributeVisibility, msg.Visibility.String()),
		sdk.NewAttribute(types.AttributeVoterCount, fmt.Sprintf("%d", voterCount)),
	))

	return &types.MsgCreateAnonymousProposalResponse{ProposalId: proposalID}, nil
}
