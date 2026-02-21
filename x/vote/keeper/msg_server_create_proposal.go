package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	"cosmossdk.io/math"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateProposal(ctx context.Context, msg *types.MsgCreateProposal) (*types.MsgCreateProposalResponse, error) {
	proposerAddr, err := k.addressCodec.StringToBytes(msg.Proposer)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid proposer address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	isModule := k.isModuleAccount(ctx, proposerAddr)

	// Check proposer is member or module account.
	if !isModule && !k.repKeeper.IsMember(ctx, proposerAddr) {
		return nil, types.ErrNotAMember
	}

	// Public proposals cannot use PRIVATE visibility.
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_PRIVATE {
		return nil, errorsmod.Wrap(types.ErrInvalidVisibility, "public proposers cannot create PRIVATE proposals; use CreateAnonymousProposal")
	}

	// Check sealed visibility is allowed.
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_SEALED && !params.AllowSealedProposals {
		return nil, types.ErrSealedNotAllowed
	}

	// Validate deposit (skip for module accounts).
	if !isModule {
		if msg.Deposit.IsAllLT(params.MinProposalDeposit) {
			return nil, types.ErrInsufficientDeposit
		}
	}

	// Validate vote options.
	if err := k.validateProposalOptions(params, msg.Options); err != nil {
		return nil, err
	}

	// Resolve quorum/threshold/vetoThreshold: use submitted values or defaults.
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

	// Validate ranges.
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
	if msg.Visibility == types.VisibilityLevel_VISIBILITY_SEALED {
		revealEpoch = uint64(votingEnd / blocksPerEpoch)
		revealEnd = votingEnd + params.SealedRevealPeriodEpochs*blocksPerEpoch
	}

	proposal := types.VotingProposal{
		Id:              proposalID,
		Title:           msg.Title,
		Description:     msg.Description,
		Proposer:        msg.Proposer,
		MerkleRoot:      merkleRoot,
		SnapshotBlock:   sdkCtx.BlockHeight(),
		EligibleVoters:  voterCount,
		Options:         msg.Options,
		VotingStart:     votingStart,
		VotingEnd:       votingEnd,
		Quorum:          quorum,
		Threshold:       threshold,
		VetoThreshold:   vetoThreshold,
		Tally:           initTally(msg.Options),
		Status:          types.ProposalStatus_PROPOSAL_STATUS_ACTIVE,
		Outcome:         types.ProposalOutcome_PROPOSAL_OUTCOME_UNSPECIFIED,
		ProposalType:    msg.ProposalType,
		ReferenceId:     msg.ReferenceId,
		CreatedAt:       sdkCtx.BlockHeight(),
		Visibility:      msg.Visibility,
		Deposit:         msg.Deposit,
		Messages:        msg.Messages,
		RevealEpoch:     revealEpoch,
		RevealEnd:       revealEnd,
	}

	if err := k.VotingProposal.Set(ctx, proposalID, proposal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store proposal")
	}

	// Store tree snapshot.
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
		sdk.NewAttribute(types.AttributeProposer, msg.Proposer),
		sdk.NewAttribute(types.AttributeVisibility, msg.Visibility.String()),
		sdk.NewAttribute(types.AttributeVoterCount, fmt.Sprintf("%d", voterCount)),
	))

	return &types.MsgCreateProposalResponse{ProposalId: proposalID}, nil
}
