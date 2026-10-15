package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// contentHashAnchored reports whether hashHex is the dedup key of a content
// record that still exists. An index entry whose record is gone is treated
// as absent: binaries before store v2 deleted content without its
// ContentByHash entry, and such an orphan must not reject a legitimate
// re-anchoring of the same content forever. Callers overwrite the entry
// when they store the new record.
func (k Keeper) contentHashAnchored(ctx context.Context, hashHex string) bool {
	contentID, err := k.ContentByHash.Get(ctx, hashHex)
	if err != nil {
		return false
	}
	has, err := k.Content.Has(ctx, contentID)
	return err == nil && has
}

// removeContentRecord deletes a content record together with every index
// that points at it: peer, type, creator, hash and expiration. The hash
// entry is removed only when it still names this record, so deleting one
// record can never free the dedup key of another.
func (k Keeper) removeContentRecord(ctx context.Context, content types.FederatedContent) {
	contentID := content.Id
	_ = k.ContentByPeer.Remove(ctx, collections.Join(content.PeerId, contentID))
	_ = k.ContentByType.Remove(ctx, collections.Join(content.ContentType, contentID))
	// The IBC receive path indexes the creator even when it is empty, so
	// the removal is unconditional; removing an absent key is a no-op.
	_ = k.ContentByCreator.Remove(ctx, collections.Join(content.CreatorIdentity, contentID))
	if len(content.ContentHash) > 0 {
		hashHex := hex.EncodeToString(content.ContentHash)
		if indexed, err := k.ContentByHash.Get(ctx, hashHex); err == nil && indexed == contentID {
			_ = k.ContentByHash.Remove(ctx, hashHex)
		}
	}
	_ = k.ContentExpiration.Remove(ctx, collections.Join(content.ExpiresAt, contentID))
	_ = k.Content.Remove(ctx, contentID)
}

// cancelContentVerification settles the verification state of a content
// record that peer removal is about to delete (spec §9 Phase 9 step 1).
//
//   - An active dispute (content CHALLENGED or DISPUTED) is cancelled: the
//     escrowed challenge fee goes back to the challenger in full, an open
//     jury escalation refunds its fee to the escalator, and
//     challenge_cancelled_peer_removed is emitted. Nobody lost the dispute;
//     the subject of it simply ceased to exist.
//   - Any verifier commitment still reserved against the record (a pending
//     dispute, or a verification whose challenge window is still open) is
//     released back to the verifier's available bond.
//   - The VerificationRecord, arbiter submissions and the challenge-window
//     entry are deleted. The remaining queue entries (verification window,
//     arbiter resolution/escalation) are keyed on deadlines the record does
//     not store; their EndBlocker walks treat a missing record or content
//     as a no-op and drop the entry, and content IDs are never reused.
func (k Keeper) cancelContentVerification(ctx context.Context, sdkCtx sdk.Context, content types.FederatedContent) {
	contentID := content.Id
	activeDispute := content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED ||
		content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED

	if esc, err := k.EscalatedChallenges.Get(ctx, contentID); err == nil {
		// overturned=true is the refund branch of disposeEscalationFee.
		k.disposeEscalationFee(ctx, esc, true)
		k.removeEscalatedChallenge(ctx, esc)
	}

	record, err := k.VerificationRecords.Get(ctx, contentID)
	if err != nil {
		if activeDispute {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeCancelledPeerRemoved,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
				sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId),
				sdk.NewAttribute(types.AttributeKeyChallengerRefunded, "0")))
		}
		return
	}

	refunded := "0"
	// Only a CHALLENGED record holds an undisbursed fee: every resolution
	// path moves the content off CHALLENGED as it disburses, and DISPUTED
	// content was never challenged (the dispute is verifier vs operator).
	if content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED &&
		!record.EscrowedChallengeFee.IsNil() && record.EscrowedChallengeFee.IsPositive() {
		k.refundChallengeFee(ctx, record)
		refunded = record.EscrowedChallengeFee.String()
	}

	k.releaseVerifierCommitment(ctx, &record)

	if activeDispute {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeCancelledPeerRemoved,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
			sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId),
			sdk.NewAttribute(types.AttributeKeyChallengerRefunded, refunded)))
	}

	k.cleanupArbiterData(ctx, contentID)
	if record.ChallengeWindowEnds > 0 {
		_ = k.ChallengeWindow.Remove(ctx, collections.Join(record.ChallengeWindowEnds, contentID))
	}
	_ = k.VerificationRecords.Remove(ctx, contentID)
}
