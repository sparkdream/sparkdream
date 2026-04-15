package keeper

import (
	"bytes"
	"context"
	"slices"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UpdatePeerPolicy(ctx context.Context, msg *types.MsgUpdatePeerPolicy) (*types.MsgUpdatePeerPolicyResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Authorization: Operations Committee
	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
		}
	}

	// Verify peer exists
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Validation 1: content types must be in known_content_types
	for _, ct := range msg.Policy.OutboundContentTypes {
		if !slices.Contains(params.KnownContentTypes, ct) {
			return nil, errorsmod.Wrapf(types.ErrUnknownContentType, "outbound content type %q not in known_content_types", ct)
		}
	}
	for _, ct := range msg.Policy.InboundContentTypes {
		if !slices.Contains(params.KnownContentTypes, ct) {
			return nil, errorsmod.Wrapf(types.ErrUnknownContentType, "inbound content type %q not in known_content_types", ct)
		}
	}

	// Validation 2: reject reveal content types
	for _, ct := range msg.Policy.OutboundContentTypes {
		if ct == "reveal_proposal" || ct == "reveal_tranche" {
			return nil, errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "reveal content types cannot be federated")
		}
	}
	for _, ct := range msg.Policy.InboundContentTypes {
		if ct == "reveal_proposal" || ct == "reveal_tranche" {
			return nil, errorsmod.Wrapf(types.ErrContentTypeNotAllowed, "reveal content types cannot be federated")
		}
	}

	// Validation 3: reputation fields only for Spark Dream peers
	if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		if msg.Policy.AllowReputationQueries || msg.Policy.AcceptReputationAttestations {
			return nil, errorsmod.Wrapf(types.ErrPeerTypeMismatch, "reputation bridging only supported for Spark Dream peers")
		}
	}

	// Set peer_id on the policy to ensure consistency
	msg.Policy.PeerId = msg.PeerId

	if err := k.PeerPolicies.Set(ctx, msg.PeerId, msg.Policy); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerPolicyUpdated,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyUpdatedBy, msg.Authority),
		),
	)

	return &types.MsgUpdatePeerPolicyResponse{}, nil
}
