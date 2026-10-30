package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CreateInterim(ctx context.Context, msg *types.MsgCreateInterim) (*types.MsgCreateInterimResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// An interim is budget-backed paid work, and CreateInterimWork below makes
	// the creator its sole assignee. Creating one is therefore only a request
	// to be paid for work: the budget is paid when the Operations Committee
	// approves it (MsgApproveInterim, never by an assignee), not when the
	// assignee says it is done (REP-E-02). Active membership is the floor;
	// the per-member active cap (max_active_interims_per_member) and the
	// per-season interim reward cap bound it from there.
	if !k.Keeper.IsActiveMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrapf(types.ErrNotMember,
			"only active members may create interim work (creator: %s)", msg.Creator)
	}

	// ADJUDICATION and JURY_DUTY interims are raised by the module only
	// (REP-E-01). A member-created ADJUDICATION interim naming a challenged
	// initiative, with a deadline already behind it, force-rejected the
	// challenge sitting in jury review on its next EndBlock.
	if IsSystemOnlyInterimType(msg.InterimType) {
		return nil, errorsmod.Wrapf(types.ErrInterimTypeSystemOnly, "%s", msg.InterimType)
	}
	if _, known := types.InterimType_name[int32(msg.InterimType)]; !known {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "unknown interim type %d", int32(msg.InterimType))
	}
	if _, known := types.InterimComplexity_name[int32(msg.Complexity)]; !known {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "unknown interim complexity %d", int32(msg.Complexity))
	}
	if msg.Deadline <= sdk.UnwrapSDKContext(ctx).BlockHeight() {
		return nil, errorsmod.Wrapf(types.ErrInterimDeadlineInvalid,
			"deadline %d is not after the current height %d", msg.Deadline, sdk.UnwrapSDKContext(ctx).BlockHeight())
	}

	// Create interim work with single assignee (creator)
	_, err = k.Keeper.CreateInterimWork(
		ctx,
		msg.InterimType,
		[]string{msg.Creator},
		"", // Committee will be determined based on interim type
		msg.ReferenceId,
		msg.ReferenceType,
		msg.Complexity,
		msg.Deadline,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgCreateInterimResponse{}, nil
}
