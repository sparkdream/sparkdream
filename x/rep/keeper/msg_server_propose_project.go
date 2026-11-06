package keeper

import (
	"context"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ProposeProject(ctx context.Context, msg *types.MsgProposeProject) (*types.MsgProposeProjectResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Validate creator is an active member. A zeroed or inactive record
	// proposes nothing: a budget-backed proposal opens paid committee work
	// (REP-E-201).
	member, err := k.GetMember(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "creator must be a member")
	}
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrMemberNotActive, "creator %s is %s", msg.Creator, member.Status)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	requestedBudget, err := requireAmount(msg.RequestedBudget, "requested_budget")
	if err != nil {
		return nil, err
	}
	requestedSpark, err := requireAmount(msg.RequestedSpark, "requested_spark")
	if err != nil {
		return nil, err
	}
	msg.RequestedBudget, msg.RequestedSpark = &requestedBudget, &requestedSpark

	// Proposal-time hard cap: reject obviously-absurd requested amounts before
	// any state is written. The cap is set well above any legitimate project
	// (default 1M DREAM / 100K SPARK) and exists purely to prevent state
	// pollution from spammy proposals. Distinct from LargeProjectBudgetThreshold,
	// which is an approval-time routing rule, not a cap.
	if msg.RequestedBudget.GT(params.MaxProjectRequestedBudget) {
		return nil, errorsmod.Wrapf(types.ErrRequestedBudgetExceedsCap,
			"requested %s exceeds cap %s",
			msg.RequestedBudget.String(), params.MaxProjectRequestedBudget.String())
	}
	if msg.RequestedSpark.GT(params.MaxProjectRequestedSpark) {
		return nil, errorsmod.Wrapf(types.ErrRequestedSparkExceedsCap,
			"requested %s exceeds cap %s",
			msg.RequestedSpark.String(), params.MaxProjectRequestedSpark.String())
	}

	// Determine if this is a permissionless project (zero budget + zero SPARK)
	permissionless := msg.RequestedBudget.IsZero() && msg.RequestedSpark.IsZero()

	if permissionless {
		// Permissionless path: validate trust level
		if commontypes.Saturate[uint32](member.TrustLevel) < params.PermissionlessMinTrustLevel {
			return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
				"permissionless project requires trust level >= %d, got %d",
				params.PermissionlessMinTrustLevel, member.TrustLevel)
		}
	} else {
		// Budget-backed path: every proposal opens a paid review interim for
		// the picked council's Operations Committee, so one member may hold
		// only max_proposed_projects_per_creator of them in PROPOSED at once,
		// counted across their recent aliases (REP-E-201).
		proposed, err := k.CountProposedProjectsForCreator(ctx, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to count proposed projects")
		}
		if proposed >= params.MaxProposedProjectsPerCreator {
			return nil, errorsmod.Wrapf(types.ErrTooManyProposedProjects,
				"%s already holds %d PROPOSED projects (max %d)", msg.Creator, proposed, params.MaxProposedProjectsPerCreator)
		}
	}

	// Burn the creation fee on both paths (if fee > 0). A budget-backed
	// proposal used to be free, which priced the committee's review work at
	// the gas of one transaction.
	if params.ProjectCreationFee.IsPositive() {
		if err := k.BurnDREAM(ctx, creatorAddr, params.ProjectCreationFee); err != nil {
			return nil, errorsmod.Wrapf(types.ErrInsufficientCreationFee,
				"need %s micro-DREAM for project creation fee: %v",
				params.ProjectCreationFee.String(), err)
		}
	}

	// Create project
	projectID, err := k.CreateProject(
		ctx,
		creatorAddr,
		msg.Name,
		msg.Description,
		msg.Tags,
		msg.Category,
		msg.Council,
		*msg.RequestedBudget,
		*msg.RequestedSpark,
		permissionless,
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create project")
	}

	// A budget-backed proposal is reviewed by the picked council's Operations
	// Committee, and the review is paid through a PROJECT_APPROVAL (or, at or
	// above budget_review_threshold, BUDGET_REVIEW) interim settled by
	// MsgApproveProjectBudget. Best-effort: the proposal is the member's
	// action and never fails on the committee's pay.
	if !permissionless {
		k.openProjectApprovalInterim(ctx, projectID, params)
	}

	return &types.MsgProposeProjectResponse{}, nil
}
