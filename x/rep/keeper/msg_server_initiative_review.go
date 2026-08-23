package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// SubmitInitiativeReview files a bonded reviewer's verdict on submitted work.
func (k msgServer) SubmitInitiativeReview(goCtx context.Context, msg *types.MsgSubmitInitiativeReview) (*types.MsgSubmitInitiativeReviewResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := k.Keeper.SubmitInitiativeReview(ctx, msg.InitiativeId, msg.Reviewer,
		msg.Approved, msg.CriteriaVotes, msg.Comments); err != nil {
		return nil, err
	}
	return &types.MsgSubmitInitiativeReviewResponse{}, nil
}

// SetVerificationPolicy configures how a project's initiatives are reviewed.
//
// Settable while the project is ACTIVE rather than fixed at creation: the
// reviewer roster grows over time, so a project has to be able to turn review on
// once reviewers exist. Fixing it at creation would strand every project made
// before the roster existed on conviction-only permanently.
func (k msgServer) SetVerificationPolicy(goCtx context.Context, msg *types.MsgSetVerificationPolicy) (*types.MsgSetVerificationPolicyResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	project, err := k.Keeper.GetProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, err
	}
	// Authorization lives in the message server throughout this module, which
	// keeps the keeper callable as trusted internal API.
	if msg.Creator != project.Creator && !k.Keeper.isOpsCommitteeAddr(ctx, msg.Creator) {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"only the project creator or the Operations Committee may set the verification policy")
	}
	if project.Status != types.ProjectStatus_PROJECT_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrInvalidProjectStatus,
			"project %d must be ACTIVE, got %s", msg.ProjectId, project.Status)
	}

	policy := msg.Policy
	if err := k.Keeper.ValidateVerificationPolicy(ctx, &policy); err != nil {
		return nil, err
	}
	project.VerificationPolicy = &policy
	if err := k.Keeper.UpdateProject(ctx, project); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"verification_policy_set",
		sdk.NewAttribute("project_id", fmt.Sprintf("%d", msg.ProjectId)),
		sdk.NewAttribute("set_by", msg.Creator),
		sdk.NewAttribute("min_verifier_count", fmt.Sprintf("%d", policy.MinVerifierCount)),
	))
	return &types.MsgSetVerificationPolicyResponse{}, nil
}

// ResolveReviewEscalation settles a review round that reached its deadline
// without meeting the gate. Operations Committee only.
func (k msgServer) ResolveReviewEscalation(goCtx context.Context, msg *types.MsgResolveReviewEscalation) (*types.MsgResolveReviewEscalationResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if !k.Keeper.isOpsCommitteeAddr(ctx, msg.Creator) {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"only the Operations Committee may resolve a review escalation")
	}
	if msg.Resolution == types.ReviewEscalation_REVIEW_ESCALATION_NONE {
		return nil, errorsmod.Wrap(types.ErrInvalidRequest, "resolution must be APPROVED, REJECTED or PASSED")
	}
	if err := k.Keeper.ResolveReviewEscalation(ctx, msg.InitiativeId, msg.Resolution, msg.Creator, msg.Reason); err != nil {
		return nil, err
	}
	return &types.MsgResolveReviewEscalationResponse{}, nil
}
