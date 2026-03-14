package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// SubmitAnonymousProposal creates a proposal through the x/shield module.
// The proposer must be the shield module account address. Unlike regular proposals,
// no council membership check is performed — the ZK proof verified by x/shield
// proves the submitter is a registered voter.
func (k msgServer) SubmitAnonymousProposal(goCtx context.Context, msg *types.MsgSubmitAnonymousProposal) (*types.MsgSubmitAnonymousProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Validate policy address exists
	councilName, err := k.PolicyToName.Get(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "unknown policy address %s", msg.PolicyAddress)
	}

	// 2. Get group for term expiration check
	extGroup, err := k.Groups.Get(ctx, councilName)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get group")
	}

	// 3. Check term expiration (anonymous proposals cannot bypass this)
	if ctx.BlockTime().Unix() > extGroup.CurrentTermExpiration {
		return nil, errorsmod.Wrapf(
			sdkerrors.ErrUnauthorized,
			"TERM EXPIRED: Group %s expired on %d. Anonymous proposals not accepted.",
			councilName, extGroup.CurrentTermExpiration,
		)
	}

	// 4. No membership check — shield ZK proof proves voter registration.
	// We still enforce the allowed messages list (same permission model).
	perms, err := k.PolicyPermissions.Get(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "no permissions found for %s", msg.PolicyAddress)
	}

	if len(msg.Messages) == 0 {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "empty proposal")
	}

	for _, anyMsg := range msg.Messages {
		var sdkMsg sdk.Msg
		if err := k.cdc.UnpackAny(anyMsg, &sdkMsg); err != nil {
			return nil, err
		}
		typeURL := sdk.MsgTypeURL(sdkMsg)

		isAllowed := false
		for _, allowedURL := range perms.AllowedMessages {
			if typeURL == allowedURL {
				isAllowed = true
				break
			}
		}
		if !isAllowed {
			return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "msg %s not allowed for policy %s", typeURL, msg.PolicyAddress)
		}
	}

	// 5. Get decision policy for timing
	decPolicy, err := k.DecisionPolicies.Get(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, errorsmod.Wrap(err, "decision policy not found")
	}

	// 6. Get current policy version
	policyVersion, err := k.GetPolicyVersion(ctx, msg.PolicyAddress)
	if err != nil {
		return nil, err
	}

	// 7. Create proposal (proposer = shield module account)
	proposalID, err := k.ProposalSeq.Next(ctx)
	if err != nil {
		return nil, err
	}

	now := ctx.BlockTime().Unix()
	proposal := types.Proposal{
		Id:             proposalID,
		CouncilName:    councilName,
		PolicyAddress:  msg.PolicyAddress,
		Proposer:       msg.Proposer, // shield module account address
		Messages:       msg.Messages,
		Status:         types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED,
		SubmitTime:     now,
		VotingDeadline: now + decPolicy.VotingPeriod,
		PolicyVersion:  policyVersion,
		Metadata:       fmt.Sprintf("[anonymous] %s", msg.Metadata),
		ExecutionTime:  now + decPolicy.VotingPeriod + decPolicy.MinExecutionPeriod,
	}

	if err := k.Proposals.Set(ctx, proposalID, proposal); err != nil {
		return nil, err
	}

	if err := k.ProposalsByCouncil.Set(ctx, collections.Join(councilName, proposalID)); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"submit_anonymous_proposal",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", proposalID)),
			sdk.NewAttribute("council_name", councilName),
		),
	)

	return &types.MsgSubmitAnonymousProposalResponse{ProposalId: proposalID}, nil
}

// AnonymousVoteProposal casts an anonymous vote on a proposal through x/shield.
// Each anonymous vote has uniform weight=1. The nullifier (handled by x/shield,
// scoped to proposal_id) prevents double-voting.
func (k msgServer) AnonymousVoteProposal(goCtx context.Context, msg *types.MsgAnonymousVoteProposal) (*types.MsgAnonymousVoteProposalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Get proposal
	proposal, err := k.Proposals.Get(ctx, msg.ProposalId)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "proposal %d not found", msg.ProposalId)
	}

	// 2. Check proposal is open for voting
	if proposal.Status != types.ProposalStatus_PROPOSAL_STATUS_SUBMITTED {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "proposal %d is not open for voting (status: %s)", msg.ProposalId, proposal.Status)
	}

	// 3. Check voting deadline
	if ctx.BlockTime().Unix() > proposal.VotingDeadline {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "voting period has ended for proposal %d", msg.ProposalId)
	}

	// 4. Validate vote option
	if msg.Option == types.VoteOption_VOTE_OPTION_UNSPECIFIED {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "vote option must be specified")
	}

	// 5. Increment anonymous vote tally.
	// Double-vote prevention is handled by x/shield's nullifier (scoped to proposal_id).
	tally, err := k.AnonVoteTallies.Get(ctx, msg.ProposalId)
	if err != nil {
		tally = types.AnonVoteTally{}
	}

	switch msg.Option {
	case types.VoteOption_VOTE_OPTION_YES:
		tally.YesCount++
	case types.VoteOption_VOTE_OPTION_NO:
		tally.NoCount++
	case types.VoteOption_VOTE_OPTION_ABSTAIN:
		tally.AbstainCount++
	case types.VoteOption_VOTE_OPTION_NO_WITH_VETO:
		tally.NoWithVetoCount++
	}

	if err := k.AnonVoteTallies.Set(ctx, msg.ProposalId, tally); err != nil {
		return nil, err
	}

	// 6. Check if threshold is met (early acceptance)
	// checkThreshold already includes anonymous votes from AnonVoteTallies.
	accepted, err := k.checkThreshold(ctx, proposal)
	if err != nil {
		return nil, err
	}
	if accepted {
		proposal.Status = types.ProposalStatus_PROPOSAL_STATUS_ACCEPTED
		decPolicy, policyErr := k.DecisionPolicies.Get(ctx, proposal.PolicyAddress)
		if policyErr == nil {
			proposal.ExecutionTime = ctx.BlockTime().Unix() + decPolicy.MinExecutionPeriod
		}
		if err := k.Proposals.Set(ctx, msg.ProposalId, proposal); err != nil {
			return nil, err
		}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"anonymous_vote_proposal",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", msg.ProposalId)),
			sdk.NewAttribute("option", msg.Option.String()),
		),
	)

	return &types.MsgAnonymousVoteProposalResponse{}, nil
}
