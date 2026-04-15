package keeper

import (
	"context"
	"slices"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AttestOutbound(ctx context.Context, msg *types.MsgAttestOutbound) (*types.MsgAttestOutboundResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Operator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}

	// 1. Verify operator is a registered, ACTIVE bridge for this peer
	bridgeKey := collections.Join(msg.Operator, msg.PeerId)
	bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Status != types.BridgeStatus_BRIDGE_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge status is %s", bridge.Status)
	}

	// 2. Verify peer is ACTIVE
	if _, err := k.GetPeerRequireActive(ctx, msg.PeerId); err != nil {
		return nil, err
	}

	// 3. Verify content_type is in outbound_content_types
	policy, err := k.PeerPolicies.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(policy.OutboundContentTypes, msg.ContentType) {
		return nil, errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "content type %q not in outbound types for peer %s", msg.ContentType, msg.PeerId)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 4. Store OutboundAttestation
	attestID, err := k.OutboundAttestSeq.Next(ctx)
	if err != nil {
		return nil, err
	}
	attestation := types.OutboundAttestation{
		Id:             attestID,
		PeerId:         msg.PeerId,
		ContentType:    msg.ContentType,
		LocalContentId: msg.LocalContentId,
		SubmittedBy:    msg.Operator,
		PublishedAt:    blockTime,
	}
	if err := k.OutboundAttestations.Set(ctx, attestID, attestation); err != nil {
		return nil, err
	}

	// 5. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeOutboundAttested,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyContentType, msg.ContentType),
			sdk.NewAttribute(types.AttributeKeyLocalContentID, msg.LocalContentId)),
	)

	return &types.MsgAttestOutboundResponse{}, nil
}
