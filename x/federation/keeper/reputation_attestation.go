package keeper

import (
	"context"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"sort"

	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"
)

// setRepAttestation stores att under (local_address, peer_id) with its
// expiry-queue entry and its (peer_id, local_address) index entry, replacing
// a previous attestation and its expiry entry.
func (k Keeper) setRepAttestation(ctx context.Context, att types.ReputationAttestation) error {
	key := collections.Join(att.LocalAddress, att.PeerId)
	if prev, err := k.RepAttestations.Get(ctx, key); err == nil {
		if err := k.AttestationExp.Remove(ctx, collections.Join3(prev.ExpiresAt, att.LocalAddress, att.PeerId)); err != nil {
			return err
		}
	}
	if err := k.RepAttestations.Set(ctx, key, att); err != nil {
		return err
	}
	if err := k.AttestationExp.Set(ctx, collections.Join3(att.ExpiresAt, att.LocalAddress, att.PeerId)); err != nil {
		return err
	}
	return k.RepAttestationsByPeer.Set(ctx, collections.Join(att.PeerId, att.LocalAddress))
}

// removeRepAttestation deletes the (localAddr, peerID) attestation with its
// expiry-queue and peer-index entries. Missing entries are skipped.
func (k Keeper) removeRepAttestation(ctx context.Context, localAddr, peerID string) {
	key := collections.Join(localAddr, peerID)
	if att, err := k.RepAttestations.Get(ctx, key); err == nil {
		if err := k.AttestationExp.Remove(ctx, collections.Join3(att.ExpiresAt, localAddr, peerID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removeRepAttestation: AttestationExp.Remove failed", "err", err)
		}
	}
	if err := k.RepAttestations.Remove(ctx, key); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removeRepAttestation: RepAttestations.Remove failed", "err", err)
	}
	if err := k.RepAttestationsByPeer.Remove(ctx, collections.Join(peerID, localAddr)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removeRepAttestation: RepAttestationsByPeer.Remove failed", "err", err)
	}
}

// sanitizeRemoteReputations bounds the per-tag reputations a peer returned
// before they are stored. A peer is not bound by this chain's responder
// cap, so the receiving side re-applies it: well-formed tag names only
// (x/common tag format, at most MaxInboundTagSize bytes), positive scores,
// one entry per tag (the first wins), sorted by tag, at most
// MaxReputationResponseTags entries.
func sanitizeRemoteReputations(in []types.TagReputation) []types.TagReputation {
	seen := make(map[string]struct{}, len(in))
	var out []types.TagReputation
	for _, tr := range in {
		if len(tr.Tag) > types.MaxInboundTagSize || !commontypes.ValidateTagFormat(tr.Tag) {
			continue
		}
		if tr.Score.IsNil() || !tr.Score.IsPositive() {
			continue
		}
		if _, dup := seen[tr.Tag]; dup {
			continue
		}
		seen[tr.Tag] = struct{}{}
		out = append(out, types.TagReputation{Tag: tr.Tag, Score: tr.Score})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	if len(out) > types.MaxReputationResponseTags {
		out = out[:types.MaxReputationResponseTags]
	}
	return out
}
