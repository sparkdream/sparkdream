package keeper

import (
	"bytes"
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	peer.Status = types.PeerStatus_PEER_STATUS_ACTIVE
	if err := k.Peers.Set(ctx, msg.PeerId, peer); err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypePeerResumed,
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
		),
	)

	return &types.MsgResumePeerResponse{}, nil
}
