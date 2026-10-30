package keeper

import (
	"context"
	"errors"

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

	// 2b. Drop its unverified-expiry entry. Left behind, it would fire at
	//     the old expiry and prune a link re-created for the same peer.
	if link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED {
		if err := k.UnverifiedLinkExp.Remove(ctx, collections.Join3(k.linkUnverifiedExpiry(ctx, link), msg.Creator, msg.PeerId)); err != nil {
			return nil, err
		}
	}

	// 3. Remove its claim and (if verified) reverse-index entries
	k.removeIdentityLinkIndexes(ctx, link)

	// 4. Decrement IdentityLinkCount
	currentCount, getErr := k.IdentityLinkCount.Get(ctx, msg.Creator)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	if currentCount > 0 {
		if err := k.setIdentityLinkCount(ctx, msg.Creator, currentCount-1); err != nil {
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
