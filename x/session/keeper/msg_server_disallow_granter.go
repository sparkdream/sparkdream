package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// DisallowGranter removes granter from the grantee's allowed granters
// (SES-102): the granter's next session key or sign-in to this grantee waits
// as PENDING again. Existing grants are untouched; the grantee declines
// those one by one. Never session-delegable.
func (k msgServer) DisallowGranter(ctx context.Context, msg *types.MsgDisallowGranter) (*types.MsgDisallowGranterResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Granter); err != nil {
		return nil, errorsmod.Wrap(err, "invalid granter address")
	}
	allowed, err := k.IsGranterAllowed(ctx, msg.Grantee, msg.Granter)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errorsmod.Wrapf(types.ErrGranterNotAllowed, "granter %s, grantee %s", msg.Granter, msg.Grantee)
	}
	if err := k.removeAllowedGranter(ctx, msg.Grantee, msg.Granter); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"granter_disallowed",
		sdk.NewAttribute("grantee", msg.Grantee),
		sdk.NewAttribute("granter", msg.Granter),
	))
	return &types.MsgDisallowGranterResponse{}, nil
}
