package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnlinkIdentity(ctx context.Context, msg *types.MsgUnlinkIdentity) (*types.MsgUnlinkIdentityResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Verify link exists
	linkKey := collections.Join(msg.Creator, msg.PeerId)
	link, err := k.IdentityLinks.Get(ctx, linkKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for %s on peer %s", msg.Creator, msg.PeerId)
	}

	// 2. Remove from IdentityLinks
	if err := k.IdentityLinks.Remove(ctx, linkKey); err != nil {
		return nil, err
	}

	// 3. Remove from IdentityLinksByRemote
	if err := k.IdentityLinksByRemote.Remove(ctx, collections.Join(msg.PeerId, link.RemoteIdentity)); err != nil {
		return nil, err
	}

	// 4. Decrement IdentityLinkCount
	currentCount, _ := k.IdentityLinkCount.Get(ctx, msg.Creator)
	if currentCount > 0 {
		if err := k.IdentityLinkCount.Set(ctx, msg.Creator, currentCount-1); err != nil {
			return nil, err
		}
	}

	// 5. Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeIdentityUnlinked,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId)),
	)

	return &types.MsgUnlinkIdentityResponse{}, nil
}
