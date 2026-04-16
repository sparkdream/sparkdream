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

	// Determine if this is a permissionless project (zero budget + zero SPARK)
	permissionless := msg.RequestedBudget.IsZero() && msg.RequestedSpark.IsZero()

	if permissionless {
		// Permissionless path: validate trust level and burn creation fee
		params, err := k.Params.Get(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to get params")
		}

		// Check minimum trust level
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
