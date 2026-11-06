package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/federation/types"
)

// SetPeer is the only writer of Peers: it stores the record and keeps the
// derived channel indexes (PeerByChannel, ActivePeerChannels) in step with
// it, so a status change or a re-registration never leaves a stale entry.
func (k Keeper) SetPeer(ctx context.Context, peer types.Peer) error {
	old, err := k.Peers.Get(ctx, peer.Id)
	switch {
	case err == nil:
		if err := k.unindexPeer(ctx, old); err != nil {
			return err
		}
	case !errors.Is(err, collections.ErrNotFound):
		return err
	}
	if err := k.Peers.Set(ctx, peer.Id, peer); err != nil {
		return err
	}
	return k.indexPeer(ctx, peer)
}

// removePeer deletes a peer record with its channel index entries.
func (k Keeper) removePeer(ctx context.Context, peerID string) error {
	old, err := k.Peers.Get(ctx, peerID)
	if err != nil {
		return err
	}
	if err := k.unindexPeer(ctx, old); err != nil {
		return err
	}
	return k.Peers.Remove(ctx, peerID)
}

// indexPeer adds the channel index entries a peer record implies.
func (k Keeper) indexPeer(ctx context.Context, peer types.Peer) error {
	if peer.Status == types.PeerStatus_PEER_STATUS_REMOVED {
		return nil
	}
	if peer.IbcChannelId != "" {
		if err := k.PeerByChannel.Set(ctx, peer.IbcChannelId, peer.Id); err != nil {
			return err
		}
	}
	if peer.Status != types.PeerStatus_PEER_STATUS_ACTIVE || peer.Type != types.PeerType_PEER_TYPE_SPARK_DREAM {
		return nil
	}
	for _, ch := range []string{peer.IbcChannelId, peer.IbcTransferChannelId} {
		if ch == "" {
			continue
		}
		if err := k.ActivePeerChannels.Set(ctx, collections.Join(ch, peer.Id)); err != nil {
			return err
		}
	}
	return nil
}

// unindexPeer removes the channel index entries a peer record implied. The
// federation channel entry is removed only while it still names this peer.
func (k Keeper) unindexPeer(ctx context.Context, peer types.Peer) error {
	if peer.IbcChannelId != "" {
		owner, err := k.PeerByChannel.Get(ctx, peer.IbcChannelId)
		switch {
		case err == nil:
			if owner == peer.Id {
				if err := k.PeerByChannel.Remove(ctx, peer.IbcChannelId); err != nil {
					return err
				}
			}
		case !errors.Is(err, collections.ErrNotFound):
			return err
		}
	}
	for _, ch := range []string{peer.IbcChannelId, peer.IbcTransferChannelId} {
		if ch == "" {
			continue
		}
		if err := k.ActivePeerChannels.Remove(ctx, collections.Join(ch, peer.Id)); err != nil {
			return err
		}
	}
	return nil
}

// peerForChannel returns the id of the non-REMOVED peer bound to the
// federation channel channelID, "" when there is none.
func (k Keeper) peerForChannel(ctx context.Context, channelID string) (string, error) {
	if channelID == "" {
		return "", nil
	}
	id, err := k.PeerByChannel.Get(ctx, channelID)
	if errors.Is(err, collections.ErrNotFound) {
		return "", nil
	}
	return id, err
}

// rebuildPeerIndexes recreates the channel indexes from Peers at
// InitGenesis.
func (k Keeper) rebuildPeerIndexes(ctx context.Context, peers []types.Peer) error {
	for _, peer := range peers {
		if err := k.indexPeer(ctx, peer); err != nil {
			return fmt.Errorf("peer %s: %w", peer.Id, err)
		}
	}
	return nil
}
