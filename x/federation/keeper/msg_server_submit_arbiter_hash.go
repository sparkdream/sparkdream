package keeper

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) SubmitArbiterHash(ctx context.Context, msg *types.MsgSubmitArbiterHash) (*types.MsgSubmitArbiterHashResponse, error) {
	// This handler supports two paths:
	// 1. Identified: bridge operator signs directly (msg.Creator = operator address)
	// 2. Anonymous: dispatched by x/shield after ZK proof (msg.Creator = shield module address)

	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Verify content is in CHALLENGED or DISPUTED status
	content, err := k.Content.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", msg.ContentId)
	}
	if content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED &&
		content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED {
		return nil, errorsmod.Wrapf(types.ErrContentNotVerified, "content status is %s, expected CHALLENGED or DISPUTED", content.Status)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// The round must still be open: not escalated, no quorum yet, within
	// arbiter_resolution_deadline. A peer being removed takes its
	// disputes with it.
	record, err := k.VerificationRecords.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "no verification record for content %d", msg.ContentId)
	}
	if err := k.arbitrationOpen(ctx, record, blockTime); err != nil {
		return nil, err
	}
	peer, err := k.Peers.Get(ctx, content.PeerId)
	if err != nil || peer.Status == types.PeerStatus_PEER_STATUS_REMOVED {
		return nil, errorsmod.Wrapf(types.ErrArbitrationClosed, "peer %s is being removed", content.PeerId)
	}

	creatorBytes, _ := k.addressCodec.StringToBytes(msg.Creator)
	shieldModuleAddr := k.authKeeper.GetModuleAddress("shield")
	isShieldModule := shieldModuleAddr != nil && sdk.AccAddress(creatorBytes).Equals(shieldModuleAddr)

	// Determine the submission key. Identified callers key by their bech32
	// address so the "no double vote per operator" rule has a stable handle.
	// Anonymous callers key by a monotonic sequence: per-identity uniqueness
	// is enforced upstream by shield's per-content nullifier scope, so the
	// federation-side key only needs to keep ArbiterSubmissions entries from
	// overwriting each other (FEDERATION-S2-5).
	var submitterKey string
	if isShieldModule {
		seq, err := k.ArbiterAnonSubSeq.Next(ctx)
		if err != nil {
			return nil, errorsmod.Wrap(err, "failed to allocate anonymous arbiter sequence")
		}
		submitterKey = fmt.Sprintf("anon:%d", seq)
	} else {
		// Identified path — must be active bridge for same peer
		submitterKey = msg.Creator
		bridgeKey := collections.Join(msg.Creator, content.PeerId)
		binding, err := k.BridgeBindings.Get(ctx, bridgeKey)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Creator, content.PeerId)
		}
		// A suspended (underfunded) binding has no live stake behind its
		// vote, and an operator whose x/service record is not ACTIVE is on
		// its way out.
		if binding.Suspended {
			return nil, errorsmod.Wrap(types.ErrBridgeNotActive, "bridge is suspended")
		}
		if err := k.requireActiveOperator(ctx, msg.Creator, peer.Type); err != nil {
			return nil, err
		}
		// Cannot arbitrate own content (alias-aware: the same member under
		// a migrated key is still the submitter).
		if k.sameMember(ctx, msg.Creator, content.SubmittedBy) {
			return nil, errorsmod.Wrap(types.ErrSelfArbiter, "submitting operator cannot arbitrate their own content")
		}
		// Nor a dispute they are a party to: the challenger or the
		// verifier holding a binding for this peer would be voting in
		// their own case.
		//
		// The anonymous path cannot be checked the same way: the shield
		// proof shows only that the voter is an ESTABLISHED+ member, not
		// who. Its bound is the first-quorum rule (a quorum cannot be
		// replaced once reached) plus either party's right to escalate the
		// stashed verdict to the Operations Committee.
		if k.sameMember(ctx, msg.Creator, record.Challenger) || k.sameMember(ctx, msg.Creator, record.Verifier) {
			return nil, errorsmod.Wrap(types.ErrSelfArbiter, "a party to the dispute cannot arbitrate it")
		}
		// Check for duplicate submission by this operator
		arbiterKey := collections.Join(msg.ContentId, submitterKey)
		_, err = k.ArbiterSubmissions.Get(ctx, arbiterKey)
		if err == nil {
			return nil, errorsmod.Wrap(types.ErrBridgeAlreadyExists, "arbiter already submitted hash for this content")
		}
	}

	// Store submission
	submission := types.ArbiterHashSubmission{
		ContentId:   msg.ContentId,
		ContentHash: msg.ContentHash,
		SubmittedAt: blockTime,
		Operator:    msg.Creator, // shield module address for anonymous path
	}
	arbiterKey := collections.Join(msg.ContentId, submitterKey)
	if err := k.ArbiterSubmissions.Set(ctx, arbiterKey, submission); err != nil {
		return nil, err
	}

	// Increment hash count
	hashHex := hex.EncodeToString(msg.ContentHash)
	countKey := collections.Join(msg.ContentId, hashHex)
	currentCount, getErr := k.ArbiterHashCounts.Get(ctx, countKey)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	newCount := currentCount + 1
	if err := k.ArbiterHashCounts.Set(ctx, countKey, newCount); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeArbiterHashSubmitted,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute("content_hash", hashHex)),
	)

	// Check if quorum reached. The first quorum is final: arbitrationOpen
	// refuses every later vote, so this branch runs at most once per round.
	if newCount >= params.ArbiterQuorum {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeArbiterQuorumReached,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute("quorum_hash", hashHex),
				sdk.NewAttribute("matching_count", fmt.Sprintf("%d", newCount))),
		)

		// Stash the verifier-side auto-verdict on the record. The verdict
		// is APPLIED later by the EndBlocker (finalizeAutoResolutions)
		// once the escalation window closes without an escalation.
		// MsgEscalateChallenge clears the stash, deferring the final
		// verdict to the jury path.
		//
		// The verifier is right iff the arbiter quorum hash equals the
		// hash the verifier originally submitted (record.VerifierHash);
		// otherwise the auto-verdict is CHALLENGE_UPHELD (verifier was
		// overturned).
		escalationDeadline := blockTime + int64(params.ArbiterEscalationWindow.Seconds())
		record.ArbiterEscalationDeadline = escalationDeadline
		if bytesEqual(msg.ContentHash, record.VerifierHash) {
			record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT
		} else {
			record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG
		}

		// Schedule the auto-verdict. Phase 8 applies it only through the
		// entry keyed on record.ArbiterEscalationDeadline.
		if err := k.ArbiterEscalationQueue.Set(ctx, collections.Join(escalationDeadline, msg.ContentId)); err != nil {
			return nil, err
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeChallengeAutoResolved,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute("quorum_hash", hashHex)),
		)

		// Compare the arbiter-quorum hash to the originally-submitted
		// content hash. If they match, the original submission was
		// honest and no report is warranted. If they differ, the
		// originating bridge operator gets reported through x/service
		// (Phase 6 of the federation→service migration: federation files
		// the report rather than slashing; the controller resolves it,
		// and ReportTimeoutAction=ESCALATE keeps a silent controller from
		// parking it). A report x/service refuses under its rate limit is
		// queued and retried, never dropped.
		record.QuorumFalsifiedContent = !bytesEqual(msg.ContentHash, content.ContentHash)
		if record.QuorumFalsifiedContent {
			evidenceURI := fmt.Sprintf("content_id=%d quorum_hash=%s", content.Id, hashHex)
			k.Keeper.fileOrQueueChallengeReport(ctx, content, &record, evidenceURI)
		}

		if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
			return nil, errorsmod.Wrap(err, "failed to stash pending verifier verdict")
		}
	}

	return &types.MsgSubmitArbiterHashResponse{}, nil
}

// bytesEqual is a thin wrapper around bytes.Equal kept here to avoid a
// fresh bytes import on a file that's otherwise import-clean.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
