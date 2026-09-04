package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CreateInterim(ctx context.Context, msg *types.MsgCreateInterim) (*types.MsgCreateInterimResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// An interim is budget-backed paid work, and CreateInterimWork below makes
	// the creator its sole assignee — so creating one is commissioning DREAM
	// for yourself. This had no gate at all: any address, member or not, could
	// mint a complexity-derived budget by pairing it with MsgCompleteInterim.
	// Membership is the floor; the per-member active cap
	// (max_active_interims_per_member) and the per-season interim reward cap
	// bound it from there.
	if _, err := k.Keeper.GetMember(ctx, creatorAddr); err != nil {
		return nil, errorsmod.Wrapf(types.ErrNotMember,
			"only members may create interim work (creator: %s)", msg.Creator)
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
