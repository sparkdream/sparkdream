package keeper

import (
	"bytes"
	"context"
	"encoding/hex"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AttestIdentityLink verifies an UNVERIFIED identity link to a bridge peer
// (ActivityPub, AT Protocol, NOSTR, Lens) on a bridge operator's word.
//
// Protocol (spec §3.4.1): MsgLinkIdentity stores a challenge on the link;
// the member publishes types.IdentityLinkProofString from the remote
// identity; a bridge operator bound to the peer fetches that evidence,
// checks its author and content off-chain, and signs this message with the
// challenge it found and where it found it. The operator's x/service SPARK
// bond backs the claim: the link records attested_by and the evidence, so a
// false attestation is reported with x/service MsgReportOperator, and a
// report that ends with the operator SLASHED returns every link it attested
// to UNVERIFIED (AttesterRevocations, EndBlocker Phase 3b).
func (k msgServer) AttestIdentityLink(ctx context.Context, msg *types.MsgAttestIdentityLink) (*types.MsgAttestIdentityLinkResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Operator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.LocalAddress); err != nil {
		return nil, errorsmod.Wrap(err, "invalid local address")
	}
	if msg.EvidenceUri == "" || len(msg.EvidenceUri) > types.MaxAttestationEvidenceSize {
		return nil, errorsmod.Wrapf(types.ErrInvalidAttestationEvidence, "evidence_uri must be 1-%d bytes", types.MaxAttestationEvidenceSize)
	}

	// 1. The signer holds a non-suspended binding for this peer.
	binding, err := k.BridgeBindings.Get(ctx, collections.Join(msg.Operator, msg.PeerId))
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Operator, msg.PeerId)
	}
	if binding.Suspended {
		return nil, errorsmod.Wrap(types.ErrBridgeNotActive, "bridge is suspended")
	}

	// 2. The peer is ACTIVE and is a bridge peer. Spark Dream peers verify
	//    links over IBC (MsgConfirmIdentityLink), never by attestation.
	peer, err := k.GetPeerRequireActive(ctx, msg.PeerId)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "peer %q", msg.PeerId)
	}
	if _, err := serviceTypeForPeer(peer.Type); err != nil {
		return nil, errorsmod.Wrapf(types.ErrPeerTypeMismatch, "peer %q is %s; identity links to it verify over IBC", msg.PeerId, peer.Type)
	}

	// 3. The operator's x/service record is ACTIVE (an UNBONDING operator
	//    fires no hook, so the binding alone would not catch it).
	if err := k.requireActiveOperator(ctx, msg.Operator, peer.Type); err != nil {
		return nil, err
	}

	// 4. No self-attestation: an operator vouching for its own (or its own
	//    member's, alias-aware) link has nobody independent behind it.
	if k.sameMember(ctx, msg.Operator, msg.LocalAddress) {
		return nil, errorsmod.Wrapf(types.ErrSelfAttestation, "operator %s", msg.Operator)
	}

	// 5. The claim exists, names this remote identity, and is still an
	//    unexpired UNVERIFIED claim.
	linkKey := collections.Join(msg.LocalAddress, msg.PeerId)
	link, err := k.IdentityLinks.Get(ctx, linkKey)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "no link for %s on peer %s", msg.LocalAddress, msg.PeerId)
	}
	if link.RemoteIdentity != msg.RemoteIdentity {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound,
			"%s is linked to %q on peer %s, not %q", msg.LocalAddress, link.RemoteIdentity, msg.PeerId, msg.RemoteIdentity)
	}
	switch link.Status {
	case types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED:
	case types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED:
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkAlreadyVerified, "link for %s on peer %s", msg.LocalAddress, msg.PeerId)
	default:
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkNotFound, "link is %s", link.Status)
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()
	expiry := k.linkUnverifiedExpiry(ctx, link)
	if now >= expiry {
		return nil, errorsmod.Wrapf(types.ErrIdentityLinkExpired, "claim expired at %d", expiry)
	}

	// 6. The attestation is bound to this claim's challenge: the proof the
	//    operator saw must be the one for this claim, not for an earlier
	//    claim of the same pair that was unlinked.
	challenge, err := hex.DecodeString(msg.Challenge)
	if err != nil || len(link.Challenge) == 0 || !bytes.Equal(challenge, link.Challenge) {
		return nil, errorsmod.Wrap(types.ErrAttestationChallengeMismatch, "challenge must be the hex of the link's challenge")
	}

	// 7. On ActivityPub the remote identity and the evidence must live on
	//    the peer's own hosts, the same provenance rule as inbound content:
	//    a bridge bound to one instance cannot vouch for another's accounts.
	if peer.Type == types.PeerType_PEER_TYPE_ACTIVITYPUB {
		policy, err := k.PeerPolicies.Get(ctx, msg.PeerId)
		if err != nil {
			return nil, err
		}
		if err := types.ContentURIHostAllowed(msg.PeerId, policy.ContentHosts, msg.EvidenceUri); err != nil {
			return nil, errorsmod.Wrap(types.ErrInvalidAttestationEvidence, err.Error())
		}
		if err := types.CreatorIdentityHostAllowed(msg.PeerId, policy.ContentHosts, msg.RemoteIdentity); err != nil {
			return nil, errorsmod.Wrap(types.ErrCreatorHostMismatch, err.Error())
		}
	}

	// 8. One verified owner per remote identity on a peer.
	remoteKey := collections.Join(msg.PeerId, msg.RemoteIdentity)
	if owner, oerr := k.IdentityLinksByRemote.Get(ctx, remoteKey); oerr == nil && owner != msg.LocalAddress {
		return nil, errorsmod.Wrapf(types.ErrRemoteIdentityAlreadyClaimed,
			"remote identity %s on peer %s is already verified to another address", msg.RemoteIdentity, msg.PeerId)
	}

	// 9. Verify: drop the claim's expiry entry, record the attester and the
	//    evidence, and enter the verified and per-attester indexes.
	if err := k.UnverifiedLinkExp.Remove(ctx, collections.Join3(expiry, msg.LocalAddress, msg.PeerId)); err != nil {
		return nil, err
	}
	link.Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED
	link.VerifiedAt = now
	link.AttestedBy = msg.Operator
	link.AttestationEvidence = msg.EvidenceUri
	if err := k.IdentityLinks.Set(ctx, linkKey, link); err != nil {
		return nil, err
	}
	if err := k.addIdentityLinkIndexes(ctx, link); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(types.EventTypeIdentityVerified,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.LocalAddress),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyRemoteIdentity, msg.RemoteIdentity),
		),
		sdk.NewEvent(types.EventTypeIdentityLinkAttested,
			sdk.NewAttribute(types.AttributeKeyOperator, msg.Operator),
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.LocalAddress),
			sdk.NewAttribute(types.AttributeKeyPeerID, msg.PeerId),
			sdk.NewAttribute(types.AttributeKeyRemoteIdentity, msg.RemoteIdentity),
			sdk.NewAttribute(types.AttributeKeyEvidenceURI, msg.EvidenceUri),
		),
	})

	return &types.MsgAttestIdentityLinkResponse{}, nil
}
