package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/collections"

	"sparkdream/x/federation/types"
)

// Identity links are indexed two ways by remote identity:
//
//   - IdentityClaimsByRemote (peer_id, remote_identity, local_address) holds
//     every link, verified or not. Several local addresses may claim the
//     same remote identity while none of them is verified, so a claim made
//     first (or made by someone who does not own the identity) never locks
//     the real owner out.
//   - IdentityLinksByRemote (peer_id, remote_identity) → local_address holds
//     only the VERIFIED link. It is the uniqueness gate (a verified identity
//     cannot be claimed again) and the ResolveRemoteIdentity answer.
//   - IdentityLinksByAttester (operator, peer_id, local_address) holds the
//     VERIFIED links a bridge operator attested (MsgAttestIdentityLink), so
//     they can be returned to UNVERIFIED if that operator is slashed.

// addIdentityLinkIndexes indexes a newly stored link.
func (k Keeper) addIdentityLinkIndexes(ctx context.Context, link types.IdentityLink) error {
	if err := k.IdentityClaimsByRemote.Set(ctx, collections.Join3(link.PeerId, link.RemoteIdentity, link.LocalAddress)); err != nil {
		return err
	}
	if link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED {
		if err := k.IdentityLinksByRemote.Set(ctx, collections.Join(link.PeerId, link.RemoteIdentity), link.LocalAddress); err != nil {
			return err
		}
		if link.AttestedBy != "" {
			return k.IdentityLinksByAttester.Set(ctx, collections.Join3(link.AttestedBy, link.PeerId, link.LocalAddress))
		}
	}
	return nil
}

// removeIdentityLinkIndexes drops link's claim entry, and its reverse-index
// entry when that entry names this link's local address (another member's
// verified link to the same remote identity is left alone).
func (k Keeper) removeIdentityLinkIndexes(ctx context.Context, link types.IdentityLink) {
	if err := k.IdentityClaimsByRemote.Remove(ctx, collections.Join3(link.PeerId, link.RemoteIdentity, link.LocalAddress)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removeIdentityLinkIndexes: IdentityClaimsByRemote.Remove failed", "err", err)
	}
	remoteKey := collections.Join(link.PeerId, link.RemoteIdentity)
	if owner, err := k.IdentityLinksByRemote.Get(ctx, remoteKey); err == nil && owner == link.LocalAddress {
		if err := k.IdentityLinksByRemote.Remove(ctx, remoteKey); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removeIdentityLinkIndexes: IdentityLinksByRemote.Remove failed", "err", err)
		}
	}
	if link.AttestedBy != "" {
		if err := k.IdentityLinksByAttester.Remove(ctx, collections.Join3(link.AttestedBy, link.PeerId, link.LocalAddress)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removeIdentityLinkIndexes: IdentityLinksByAttester.Remove failed", "err", err)
		}
	}
}

// setIdentityLinkCount stores a member's link count, removing it at zero:
// InitGenesis re-counts the imported links and only writes members that
// hold one, and an absent count reads as zero.
func (k Keeper) setIdentityLinkCount(ctx context.Context, addr string, n uint32) error {
	if n == 0 {
		return k.IdentityLinkCount.Remove(ctx, addr)
	}
	return k.IdentityLinkCount.Set(ctx, addr, n)
}
