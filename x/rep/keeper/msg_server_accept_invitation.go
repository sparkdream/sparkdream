package keeper

import (
	"context"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AcceptInvitation(ctx context.Context, msg *types.MsgAcceptInvitation) (*types.MsgAcceptInvitationResponse, error) {
	inviteeAddr, err := k.addressCodec.StringToBytes(msg.Invitee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid invitee address")
	}

	// Open content: joining is agreeing that everything the member publishes
	// is dedicated to the public domain (docs/content-license.md).
	if msg.AcceptedContentLicense != commontypes.ChainContentLicense {
		return nil, errorsmod.Wrapf(types.ErrContentLicenseNotAccepted, "got %q", msg.AcceptedContentLicense)
	}

	// Accept invitation
	if err := k.Keeper.AcceptInvitation(ctx, msg.InvitationId, inviteeAddr); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"content_license_accepted",
		sdk.NewAttribute("member", msg.Invitee),
		sdk.NewAttribute("license", msg.AcceptedContentLicense),
	))

	return &types.MsgAcceptInvitationResponse{}, nil
}
