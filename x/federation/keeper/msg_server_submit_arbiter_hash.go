package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
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
		_, err := k.BridgeBindings.Get(ctx, bridgeKey)
		if err != nil {
			return nil, errorsmod.Wrapf(types.ErrBridgeNotFound, "operator %s not registered for peer %s", msg.Creator, content.PeerId)
		}
		// Cannot arbitrate own content
		if msg.Creator == content.SubmittedBy {
			return nil, errorsmod.Wrap(types.ErrSelfArbiter, "submitting operator cannot arbitrate their own content")
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
	currentCount, _ := k.ArbiterHashCounts.Get(ctx, countKey)
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

	// Check if quorum reached
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
		// overturned). Skipped silently if the verification record is
		// missing (defensive — the verify path always writes one).
		if record, rerr := k.VerificationRecords.Get(ctx, msg.ContentId); rerr == nil {
			if bytesEqual(msg.ContentHash, record.VerifierHash) {
				record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT
			} else {
				record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG
			}
			if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
				return nil, errorsmod.Wrap(err, "failed to stash pending verifier verdict")
			}
		}

		// Auto-resolve: schedule the resolution window (existing
		// behavior) and, if the arbiter quorum hash differs from the
		// originally-submitted content hash, file a system report
		// against the submitting bridge operator via x/service.
		//
		// Phase 6 of the federation→service migration: federation
		// files the report rather than directly slashing. The
		// controller resolves it through the standard tier-1 path
		// (with ReportTimeoutAction=ESCALATE on bridge service types,
		// a silent controller can't park the slash forever — the
		// EndBlocker auto-escalates to jury).
		escalationDeadline := blockTime + int64(params.ArbiterEscalationWindow.Seconds())
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
		// honest and no slash is warranted. If they differ, the
		// originating bridge operator gets reported.
		if !bytesEqual(msg.ContentHash, content.ContentHash) {
			evidenceURI := fmt.Sprintf("content_id=%d quorum_hash=%s", content.Id, hashHex)
			if err := k.Keeper.fileChallengeReport(ctx, content, evidenceURI); err != nil {
				// Log via event so a service-side failure doesn't
				// roll back the auto-resolve — federation can recover
				// orphans manually via MsgPruneOrphanBindings if a
				// service-side report write fails here.
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent(types.EventTypeFederationHookFailure,
						sdk.NewAttribute("hook", "fileChallengeReport"),
						sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
						sdk.NewAttribute("error", err.Error()),
					),
				)
			}
		}
	}

	return &types.MsgSubmitArbiterHashResponse{}, nil
}

// fileChallengeReport opens a system report against the bridge
// operator who submitted the now-rejected/escalated content. Phase 6
// of the federation→service migration: federation calls
// serviceKeeper.OpenSystemReport with a dedupe key derived from the
// content_id, so repeat calls during EndBlocker retries / re-orgs
// don't open duplicate reports. Used by both MsgSubmitArbiterHash
// (quorum-reached path) and MsgEscalateChallenge (party-initiated).
//
// No-op when serviceKeeper isn't wired (standalone-mode tests).
func (k Keeper) fileChallengeReport(ctx context.Context, content types.FederatedContent, evidenceURI string) error {
	sk := k.serviceKeeper()
	if sk == nil {
		return nil
	}

	peer, err := k.Peers.Get(ctx, content.PeerId)
	if err != nil {
		return errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", content.PeerId)
	}
	serviceType, err := serviceTypeForPeer(peer.Type)
	if err != nil {
		// SPARK_DREAM peer; no service-backed bridge to slash.
		return nil
	}

	operatorAddr, err := k.addressCodec.StringToBytes(content.SubmittedBy)
	if err != nil {
		return errorsmod.Wrap(err, "invalid submitted_by address")
	}
	callerAddr := k.authKeeper.GetModuleAddress(types.ModuleName)

	// dedupeKey = sha256(content_id || ":federation:challenge"). Per
	// migration plan: include the challenge ID so a re-orged re-fire
	// of the same content's challenge resolves to the same report.
	idBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(idBuf, content.Id)
	keyHash := sha256.Sum256(append(idBuf, []byte(":federation:challenge")...))

	// slashBps=0 → service falls back to ServiceTypeConfig.challenge_default_slash_bps.
	_, _, err = sk.OpenSystemReport(ctx, callerAddr, operatorAddr, serviceType, 0, evidenceURI, keyHash[:])
	return err
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
