package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ProposeProject(ctx context.Context, msg *types.MsgProposeProject) (*types.MsgProposeProjectResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Validate creator is a member
	member, err := k.GetMember(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "creator must be a member")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

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
		// Permissionless path: validate trust level and burn creation fee
		if uint32(member.TrustLevel) < params.PermissionlessMinTrustLevel {
			return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
				"permissionless project requires trust level >= %d, got %d",
				params.PermissionlessMinTrustLevel, member.TrustLevel)
		}

		// Burn creation fee (if fee > 0)
		if params.ProjectCreationFee.IsPositive() {
			if err := k.BurnDREAM(ctx, creatorAddr, params.ProjectCreationFee); err != nil {
				return nil, errorsmod.Wrapf(types.ErrInsufficientCreationFee,
					"need %s micro-DREAM for project creation fee: %v",
					params.ProjectCreationFee.String(), err)
			}
		}
	}

	// Create project
	_, err = k.CreateProject(
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

	return &types.MsgProposeProjectResponse{}, nil
}
