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
	bridge, err := k.BridgeBindings.Get(ctx, bridgeKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Operator, msg.PeerId)
	}
	if bridge.Suspended {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotActive, "bridge is suspended")
	}
	// local_content_id is stored, so it is bounded like every other
	// identifier a bridge writes (FED-107).
	if len(msg.LocalContentId) == 0 || len(msg.LocalContentId) > types.MaxInboundIdentifierSize {
		return nil, errorsmod.Wrapf(types.ErrInvalidRequest, "local_content_id must be 1..%d bytes", types.MaxInboundIdentifierSize)
	}

	// 2. Verify peer is ACTIVE
	peer, err := k.GetPeerRequireActive(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}

	// 2b. Same operator gate as every other bridge message: an active
	//     member whose x/service record is ACTIVE with min_bond. An
	//     UNBONDING operator fires no hook, so its binding is never
	//     suspended and only this check stops it (FED-107).
	if err := k.requireActiveOperator(ctx, msg.Operator, peer.Type); err != nil {
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

	// 3b. Outbound rate limits (spec §10.3), as for FederateContent: each
	//     attestation is a state row, so they are capped per block and
	//     per peer window.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.checkAndRecordOutboundPerBlock(ctx, sdkCtx.BlockHeight(), params.MaxOutboundPerBlock); err != nil {
		return nil, errorsmod.Wrap(err, "global per-block outbound cap")
	}
	if err := k.checkAndRecordOutboundRate(ctx, msg.PeerId, blockTime,
		int64(params.RateLimitWindow.Seconds()), policy.OutboundRateLimitPerEpoch); err != nil {
		return nil, errorsmod.Wrapf(err, "peer %s", msg.PeerId)
	}

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
