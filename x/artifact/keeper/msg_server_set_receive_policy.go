package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) SetReceivePolicy(ctx context.Context, msg *types.MsgSetReceivePolicy) (*types.MsgSetReceivePolicyResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	if _, ok := types.ReceivePolicy_name[int32(msg.Policy)]; !ok {
		return nil, errorsmod.Wrapf(types.ErrInvalidParams, "unknown receive policy %d", msg.Policy)
	}
	if msg.Policy == types.ReceivePolicy_RECEIVE_POLICY_UNSPECIFIED {
		if err := k.ReceivePolicies.Remove(ctx, msg.Owner); err != nil {
			return nil, err
		}
	} else if err := k.ReceivePolicies.Set(ctx, msg.Owner, uint64(msg.Policy)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventReceivePolicySet,
		sdk.NewAttribute("address", msg.Owner), sdk.NewAttribute("policy", msg.Policy.String()))
	return &types.MsgSetReceivePolicyResponse{}, nil
}
