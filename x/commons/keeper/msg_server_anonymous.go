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

	// 0. Authenticate: only the x/shield module account may submit anonymous proposals.
	shieldModuleAddr := k.authKeeper.GetModuleAddress("shield")
	if shieldModuleAddr == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "shield module address not found")
	}
	if msg.Proposer != shieldModuleAddr.String() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"only the shield module account can submit anonymous proposals (got %s, want %s)",
			msg.Proposer, shieldModuleAddr.String())
	}

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

	// Same rule as SubmitProposal: a signaling proposal (no executable
	// messages) is allowed but must carry metadata.
	if len(msg.Messages) == 0 && msg.Metadata == "" {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "signaling proposal requires non-empty metadata")
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

// AnonymousVoteProposal is disabled: every council and committee votes
// openly. An anonymous vote proves only that the voter is in the x/rep trust
// tree, not that they sit on the council, and a member could cast it on top
// of their open vote (the two cannot be linked), so counting it let any rep
// member move a council's tally. A secret ballot for the Commons Council
// would need a tree of that council's members and no open votes; until one
// exists the message is refused.
func (k msgServer) AnonymousVoteProposal(goCtx context.Context, msg *types.MsgAnonymousVoteProposal) (*types.MsgAnonymousVoteProposalResponse, error) {
	shieldModuleAddr := k.authKeeper.GetModuleAddress("shield")
	if shieldModuleAddr == nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrLogic, "shield module address not found")
	}
	if msg.Voter != shieldModuleAddr.String() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized,
			"only the shield module account can cast anonymous votes (got %s, want %s)",
			msg.Voter, shieldModuleAddr.String())
	}
	return nil, types.ErrAnonymousVotingDisabled
}
