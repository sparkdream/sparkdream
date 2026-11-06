package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibckeeper "github.com/cosmos/ibc-go/v10/modules/core/keeper"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"
)

func (k msgServer) ResumePeer(ctx context.Context, msg *types.MsgResumePeer) (*types.MsgResumePeerResponse, error) {
	authorityBytes, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Activation is the trust decision in a peer's lifecycle: a PENDING peer
	// is inert -- FederateContent, SubmitFederatedContent and
	// RequestReputationAttestation all require ACTIVE -- so this is the point
	// at which content and reputation start crossing. Unlike registration,
	// suspension and removal, it therefore takes a committee VOTE and not one
	// member's signature: IsCouncilOrCommitteePolicy excludes individual
	// membership. Stopping stays 1-of-N on purpose (see SuspendPeer).
	if !bytes.Equal(k.authority, authorityBytes) {
		if k.late.commonsKeeper == nil || !k.late.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, msg.Authority, "commons", "operations") {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized,
				"peer activation must be executed by the Operations Committee policy "+
					"(a passed committee proposal), the Commons Council policy, or governance. "+
					"A signature from an individual committee member is not enough -- "+
					"submit a proposal and vote it through")
		}
	}

	peer, err := k.Peers.Get(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", msg.PeerId)
	}
	// Allow resuming SUSPENDED peers and activating PENDING peers (council-gated activation)
	if peer.Status != types.PeerStatus_PEER_STATUS_SUSPENDED && peer.Status != types.PeerStatus_PEER_STATUS_PENDING {
		return nil, errorsmod.Wrapf(types.ErrPeerNotActive, "peer %q is not suspended or pending (status: %s)", msg.PeerId, peer.Status)
	}

	// FED-106: a Spark Dream peer's federation channel must exist on this
	// chain's federation port, be OPEN, and lead to the chain the peer was
	// registered for. Channel ids are sequential, so one registered ahead
	// of its handshake could be taken by whoever opens the next federation
	// channel; the light client's chain id behind the channel must equal
	// the peer's expected_chain_id, and a channel whose chain cannot be
	// resolved is refused: activation is the trust point. The transfer
	// channel, when the peer names one, is held to the same check
	// (FED-201): it is what the relay refund trusts and what the voucher
	// metadata below is keyed on. The event names the counterparty chain
	// for the record.
	counterpartyChain, err := k.checkPeerChannel(ctx, peer)
	if err != nil {
		return nil, err
	}

	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	if err := k.SetPeer(ctx, peer); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Pre-register IBC voucher metadata for a Spark Dream peer that supplied
	// a peer_identity and a transfer channel (spec §9.2). This runs at
	// activation, not registration (FED-201): registration is 1-of-N and
	// the transfer channel is only verified here, so a display symbol must
	// not be attached to whatever chain sits behind an unverified channel.
	// Idempotent (skips existing metadata), so a SUSPENDED -> ACTIVE resume
	// is a no-op. Errors do not fail activation: metadata is informational
	// and the peer record is the source of truth.
	if peer.Type == types.PeerType_PEER_TYPE_SPARK_DREAM &&
		peer.IbcTransferChannelId != "" &&
		peer.PeerIdentity != nil &&
		peer.PeerIdentity.BondDenom != "" {
		if err := k.preRegisterIBCVoucherMetadata(ctx, peer); err != nil {
			sdkCtx.EventManager().EmitEvent(
				sdk.NewEvent(
					"federation_peer_metadata_skipped",
					sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
					sdk.NewAttribute("reason", err.Error()),
				),
			)
		}
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerResumed,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyCounterpartyChainID, counterpartyChain),
		),
	)

	return &types.MsgResumePeerResponse{}, nil
}

// checkPeerChannel verifies that a Spark Dream peer's federation channel is
// OPEN on the federation port and that the Tendermint light client behind
// it (channel -> connection -> client) names the peer's expected_chain_id;
// it returns that chain id. The peer's ICS-20 transfer channel, when set,
// must pass the same check on the transfer port (FED-201). A channel whose
// client chain cannot be resolved (no connection hop, missing connection or
// client, or a non-Tendermint client) is refused with
// ErrPeerChainIdMismatch: an unverifiable channel is not trusted. Peers of
// other types, and Spark Dream peers without a federation channel, pass
// with "".
func (k Keeper) checkPeerChannel(ctx context.Context, peer types.Peer) (string, error) {
	if peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM || peer.IbcChannelId == "" {
		return "", nil
	}
	var ibcK *ibckeeper.Keeper
	if k.late != nil && k.late.ibcKeeperFn != nil {
		ibcK = k.late.ibcKeeperFn()
	}
	if ibcK == nil {
		return "", errorsmod.Wrapf(types.ErrPeerChainIdMismatch, "IBC keeper is not wired; channel %s cannot be verified", peer.IbcChannelId)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	chainID, err := channelChainID(sdkCtx, ibcK, types.PortID, peer.IbcChannelId, peer)
	if err != nil {
		return "", err
	}
	if peer.IbcTransferChannelId != "" {
		if _, err := channelChainID(sdkCtx, ibcK, ibctransfertypes.PortID, peer.IbcTransferChannelId, peer); err != nil {
			return "", err
		}
	}
	return chainID, nil
}

// channelChainID resolves channel -> connection -> Tendermint client on
// port and returns the client's chain id, refusing a channel that is not
// OPEN or whose chain is not peer.expected_chain_id.
func channelChainID(sdkCtx sdk.Context, ibcK *ibckeeper.Keeper, port, channelID string, peer types.Peer) (string, error) {
	ch, found := ibcK.ChannelKeeper.GetChannel(sdkCtx, port, channelID)
	if !found {
		return "", errorsmod.Wrapf(types.ErrInvalidRequest, "channel %s does not exist on port %s", channelID, port)
	}
	if ch.State != channeltypes.OPEN {
		return "", errorsmod.Wrapf(types.ErrInvalidRequest, "channel %s on port %s is %s, not OPEN", channelID, port, ch.State)
	}
	if len(ch.ConnectionHops) == 0 {
		return "", errorsmod.Wrapf(types.ErrPeerChainIdMismatch, "channel %s on port %s has no connection hops", channelID, port)
	}
	conn, found := ibcK.ConnectionKeeper.GetConnection(sdkCtx, ch.ConnectionHops[0])
	if !found {
		return "", errorsmod.Wrapf(types.ErrPeerChainIdMismatch, "connection %s of channel %s does not exist", ch.ConnectionHops[0], channelID)
	}
	cs, found := ibcK.ClientKeeper.GetClientState(sdkCtx, conn.ClientId)
	if !found {
		return "", errorsmod.Wrapf(types.ErrPeerChainIdMismatch, "client %s of channel %s does not exist", conn.ClientId, channelID)
	}
	tm, ok := cs.(*ibctm.ClientState)
	if !ok {
		return "", errorsmod.Wrapf(types.ErrPeerChainIdMismatch, "client %s of channel %s is %s, not a Tendermint client", conn.ClientId, channelID, cs.ClientType())
	}
	if tm.ChainId != peer.ExpectedChainId {
		return "", errorsmod.Wrapf(types.ErrPeerChainIdMismatch, "channel %s on port %s leads to chain %q, peer %q expects %q", channelID, port, tm.ChainId, peer.Id, peer.ExpectedChainId)
	}
	return tm.ChainId, nil
}
