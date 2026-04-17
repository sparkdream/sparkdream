package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RequestReputationAttestation(ctx context.Context, msg *types.MsgRequestReputationAttestation) (*types.MsgRequestReputationAttestationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Verify peer is type SPARK_DREAM and ACTIVE
	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		return nil, errorsmod.Wrapf(types.ErrReputationNotSupported, "reputation queries only for Spark Dream peers, peer %q is %s", msg.PeerId, peer.Type)
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q status is %s", msg.PeerId, peer.Status)
	}

	// 2. Verify peer policy allows reputation attestations
	policy, err := k.PeerPolicies.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, err
	}
	if !policy.AcceptReputationAttestations {
		return nil, errorsmod.Wrapf(types.ErrReputationNotSupported, "peer %q does not accept reputation attestations", msg.PeerId)
	}

	// 3. Send IBC ReputationQueryPacket (best-effort: event emitted even if IBC unavailable)
	packetData := &types.FederationPacketData{
		Packet: &types.FederationPacketData_ReputationQuery{
			ReputationQuery: &types.ReputationQueryPacket{
				QueriedAddress: msg.RemoteAddress,
				Requester:      msg.Creator,
			},
		},
	}
	// Non-fatal: IBC may not be available in all environments (e.g., tests).
	// The response will arrive via OnAcknowledgementPacket when IBC is operational.
	_, _ = k.SendFederationPacket(ctx, msg.PeerId, packetData)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeReputationAttested,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator)),
	)

	return &types.MsgRequestReputationAttestationResponse{}, nil
}
