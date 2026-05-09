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

	// 3. Send IBC ReputationQueryPacket. The reputation_attested event is still
	// emitted on success so callers see their request acknowledged on-chain;
	// the actual attestation is stored later in OnAcknowledgementPacket once
	// the remote chain responds. If SendFederationPacket fails (IBC not wired,
	// channel closed, marshal error, etc.) we surface that via a dedicated
	// event + log line — silently swallowing with `_, _ =` was the exact
	// regression `test_crosschain_reputation.sh` TEST 1 catches.
	packetData := &types.FederationPacketData{
		Packet: &types.FederationPacketData_ReputationQuery{
			ReputationQuery: &types.ReputationQueryPacket{
				QueriedAddress: msg.RemoteAddress,
				Requester:      msg.Creator,
			},
		},
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if _, sendErr := k.SendFederationPacket(ctx, msg.PeerId, packetData); sendErr != nil {
		sdkCtx.Logger().With("module", "x/federation").Error(
			"RequestReputationAttestation: SendFederationPacket failed; no IBC packet was committed",
			"peer_id", msg.PeerId,
			"requester", msg.Creator,
			"queried_address", msg.RemoteAddress,
			"error", sendErr,
		)
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeFederationPacketSendFailed,
				sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
				sdk.NewAttribute(types.AttributeKeyPacketKind, "reputation_query"),
				sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
				sdk.NewAttribute(types.AttributeKeyError, sendErr.Error())),
		)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeReputationAttested,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator)),
	)

	return &types.MsgRequestReputationAttestationResponse{}, nil
}
