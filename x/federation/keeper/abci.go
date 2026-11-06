package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"
)

// EndBlocker runs at the end of each block. 12 phases per spec §9.
// BeginBlocker funds the bridge-operator reward pool. Funding is best-effort:
// FundOperatorRewardPool swallows its own failures, and any error surfacing
// here is logged rather than returned, because a funding problem must never
// halt the chain.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	if err := k.FundOperatorRewardPool(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().With("module", "x/federation").
			Error("BeginBlocker: operator reward pool funding failed", "error", err)
	}
	return nil
}

func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}

	logger := sdkCtx.Logger().With("module", "x/federation")

	maxPrune := params.MaxPrunePerBlock
	var pruned uint64
	var phaseErr error

	// Phase 1: Prune Expired Federated Content
	pruned, phaseErr = k.pruneExpiredContent(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 1 (prune expired content) failed", "error", phaseErr)
	}

	// Phase 2: Prune Expired Reputation Attestations
	pruned, phaseErr = k.pruneExpiredAttestations(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 2 (prune expired attestations) failed", "error", phaseErr)
	}

	// Phase 3: Prune Expired Unverified Identity Links
	pruned, phaseErr = k.pruneExpiredUnverifiedLinks(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 3 (prune expired unverified links) failed", "error", phaseErr)
	}

	// Phase 3b: Withdraw identity attestations of slashed bridge operators
	pruned, phaseErr = k.revokeDissolvedAttestations(ctx, sdkCtx, now, maxPrune, pruned, params)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 3b (revoke dissolved operator attestations) failed", "error", phaseErr)
	}

	// Phase 4: Prune Expired Identity Challenges
	pruned, phaseErr = k.pruneExpiredIdentityChallenges(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 4 (prune expired identity challenges) failed", "error", phaseErr)
	}

	// Phase 5: Expire Unverified Content
	pruned, phaseErr = k.expireUnverifiedContent(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 5 (expire unverified content) failed", "error", phaseErr)
	}

	// Phase 6: Release Verifier Bond Commitments
	pruned, phaseErr = k.releaseVerifierBondCommitments(ctx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 6 (release verifier bond commitments) failed", "error", phaseErr)
	}

	// Phase 7: Expire Arbiter Resolution Windows
	pruned, phaseErr = k.expireArbiterResolutions(ctx, sdkCtx, now, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 7 (expire arbiter resolutions) failed", "error", phaseErr)
	}

	// Phase 8: Finalize Auto-Resolutions
	pruned, phaseErr = k.finalizeAutoResolutions(ctx, now, maxPrune, pruned, params)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 8 (finalize auto-resolutions) failed", "error", phaseErr)
	}

	// Phase 8a: Retry EndBlocker settlements that failed (FED-29).
	pruned, phaseErr = k.retryFailedSettlements(ctx, now, maxPrune, pruned, params)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 8a (retry failed settlements) failed", "error", phaseErr)
	}

	// Phase 8b: Retry operator reports x/service refused under its
	// per-caller system-report rate limit.
	pruned, phaseErr = k.retryPendingChallengeReports(ctx, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 8b (retry pending challenge reports) failed", "error", phaseErr)
	}

	// Phase 9: Process Peer Removal Queue
	_, phaseErr = k.processPeerRemovalQueue(ctx, sdkCtx, maxPrune, pruned)
	if phaseErr != nil {
		logger.Error("EndBlocker phase 9 (process peer removal queue) failed", "error", phaseErr)
	}

	// Phase 10 (verifier epoch rewards) is gone: verifier pay -- both the
	// SPARK pool and the DREAM stipend -- is distributed by x/rep's
	// EndBlocker, which owns the RoleActivity record the payout is scored
	// from. Federation reports verifications and verdicts and pays nothing.
	//
	// Bridge OPERATOR pay does live here, because federation owns the data it
	// is scored from (BridgeBinding submission counters) and operators are
	// x/service Operators rather than x/rep bonded roles. Distribution runs
	// before the overflow burn so the pool drains to the people who earned it
	// first and the burn only ever targets residual.
	if err := k.DistributeOperatorRewards(ctx); err != nil {
		logger.Error("EndBlocker: operator reward distribution failed", "error", err)
	}
	// The overflow burn runs once per reward epoch, after the epoch's
	// distribution, as documented: running it every block would burn a
	// donation above the cap almost entirely within a few blocks.
	if k.IsOperatorRewardEpoch(ctx) {
		if err := k.BurnOperatorRewardPoolOverflow(ctx); err != nil {
			logger.Error("EndBlocker: operator reward pool overflow burn failed", "error", err)
		}
	}

	// Phase 11: Bridge Operator Monitoring
	if err := k.monitorBridgeOperators(ctx, sdkCtx, now, params); err != nil {
		logger.Error("EndBlocker phase 11 (monitor bridge operators) failed", "error", err)
	}

	// Phase 12: Clean Stale Rate Limit Counters
	if err := k.cleanStaleRateLimitCounters(ctx, now, params); err != nil {
		logger.Error("EndBlocker phase 12 (clean stale rate limit counters) failed", "error", err)
	}

	return nil
}

// --- Phase 1 ---

func (k Keeper) pruneExpiredContent(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.ContentExpiration.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		if content, err := k.Content.Get(ctx, contentID); err == nil {
			k.removeContentRecord(ctx, content)
		}
		if err := k.ContentExpiration.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredContent: ContentExpiration.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 2 ---

func (k Keeper) pruneExpiredAttestations(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(now+1, "", ""))

	err := k.AttestationExp.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		// Only the attestation this entry was written for: a refresh
		// replaced the old expiry entry, so a match is the live one.
		if att, err := k.RepAttestations.Get(ctx, collections.Join(key.K2(), key.K3())); err == nil && att.ExpiresAt == key.K1() {
			k.removeRepAttestation(ctx, key.K2(), key.K3())
		}
		if err := k.AttestationExp.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("pruneExpiredAttestations: AttestationExp.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 3 ---

func (k Keeper) pruneExpiredUnverifiedLinks(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Triple[int64, string, string]]).
		EndExclusive(collections.Join3(now+1, "", ""))

	err := k.UnverifiedLinkExp.Walk(ctx, rng, func(key collections.Triple[int64, string, string]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		localAddr := key.K2()
		peerID := key.K3()

		link, err := k.IdentityLinks.Get(ctx, collections.Join(localAddr, peerID))
		// Only the entry the live link was written with may prune it: a
		// stale entry left by an earlier link of the same (local, peer)
		// that was unlinked and re-created would otherwise delete the new
		// link early, often mid-confirmation.
		if err == nil && link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED &&
			k.linkUnverifiedExpiry(ctx, link) == key.K1() {
			if err := k.IdentityLinks.Remove(ctx, collections.Join(localAddr, peerID)); err != nil {
				sdkCtx.Logger().Error("pruneExpiredUnverifiedLinks: IdentityLinks.Remove failed", "err", err)
			}
			k.removeIdentityLinkIndexes(ctx, link)
			cnt, getErr := k.IdentityLinkCount.Get(ctx, localAddr)
			if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
				sdk.UnwrapSDKContext(sdkCtx).Logger().Error("pruneExpiredUnverifiedLinks: IdentityLinkCount.Get failed", "err", getErr)
			}
			if cnt > 0 {
				if err := k.setIdentityLinkCount(ctx, localAddr, cnt-1); err != nil {
					sdkCtx.Logger().Error("pruneExpiredUnverifiedLinks: IdentityLinkCount.Set failed", "err", err)
				}
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityLinkExpired,
				sdk.NewAttribute(types.AttributeKeyLocalAddress, localAddr),
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID)))
		}
		if err := k.UnverifiedLinkExp.Remove(ctx, key); err != nil {
			sdkCtx.Logger().Error("pruneExpiredUnverifiedLinks: UnverifiedLinkExp.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	})
	return pruned, err
}

// linkUnverifiedExpiry is the UnverifiedLinkExp key a link was written
// with. Links written before unverified_expires_at existed fall back to
// linked_at + the current TTL.
func (k Keeper) linkUnverifiedExpiry(ctx context.Context, link types.IdentityLink) int64 {
	if link.UnverifiedExpiresAt != 0 {
		return link.UnverifiedExpiresAt
	}
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("linkUnverifiedExpiry: Params.Get failed", "err", getErr)
	}
	return link.LinkedAt + int64(params.UnverifiedLinkTtl.Seconds())
}

// --- Phase 3b ---

// revokeDissolvedAttestations works off AttesterRevocations: for each
// queued (operator, peer) it returns the identity links the operator
// attested on that peer to UNVERIFIED, one budget unit per link, and drops
// the queue entry once the operator has no attested links left there. A
// link that went back to UNVERIFIED gets a fresh unverified_link_ttl and
// keeps its challenge, so the member can have another bridge operator
// attest the same published proof.
func (k Keeper) revokeDissolvedAttestations(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64, params types.Params) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	var queue []collections.Pair[string, string]
	if err := k.AttesterRevocations.Walk(ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
		queue = append(queue, key)
		return uint64(len(queue)) >= maxPrune-pruned, nil
	}); err != nil {
		return pruned, err
	}
	for _, entry := range queue {
		if pruned >= maxPrune {
			break
		}
		operator, peerID := entry.K1(), entry.K2()
		var locals []string
		done := true
		rng := collections.NewSuperPrefixedTripleRange[string, string, string](operator, peerID)
		if err := k.IdentityLinksByAttester.Walk(ctx, rng, func(key collections.Triple[string, string, string]) (bool, error) {
			if pruned+uint64(len(locals)) >= maxPrune {
				done = false
				return true, nil
			}
			locals = append(locals, key.K3())
			return false, nil
		}); err != nil {
			return pruned, err
		}
		for _, local := range locals {
			if err := k.IdentityLinksByAttester.Remove(ctx, collections.Join3(operator, peerID, local)); err != nil {
				sdkCtx.Logger().Error("revokeDissolvedAttestations: IdentityLinksByAttester.Remove failed", "err", err)
			}
			link, err := k.IdentityLinks.Get(ctx, collections.Join(local, peerID))
			if err == nil && link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED && link.AttestedBy == operator {
				if err := k.unverifyAttestedLink(ctx, sdkCtx, link, now, params); err != nil {
					return pruned, err
				}
			}
			pruned++
		}
		if done {
			if err := k.AttesterRevocations.Remove(ctx, entry); err != nil {
				sdkCtx.Logger().Error("revokeDissolvedAttestations: AttesterRevocations.Remove failed", "err", err)
			}
		}
	}
	return pruned, nil
}

// unverifyAttestedLink returns a bridge-attested VERIFIED link to
// UNVERIFIED: it leaves the verified reverse index, its attestation is
// cleared, and it re-enters the unverified expiry queue with a fresh TTL.
func (k Keeper) unverifyAttestedLink(ctx context.Context, sdkCtx sdk.Context, link types.IdentityLink, now int64, params types.Params) error {
	k.removeIdentityLinkIndexes(ctx, link)
	operator, evidence := link.AttestedBy, link.AttestationEvidence
	link.Status = types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED
	link.VerifiedAt = 0
	link.AttestedBy = ""
	link.AttestationEvidence = ""
	link.UnverifiedExpiresAt = now + int64(params.UnverifiedLinkTtl.Seconds())
	if err := k.IdentityLinks.Set(ctx, collections.Join(link.LocalAddress, link.PeerId), link); err != nil {
		return err
	}
	if err := k.addIdentityLinkIndexes(ctx, link); err != nil {
		return err
	}
	if err := k.UnverifiedLinkExp.Set(ctx, collections.Join3(link.UnverifiedExpiresAt, link.LocalAddress, link.PeerId)); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityAttestationRevoked,
		sdk.NewAttribute(types.AttributeKeyOperator, operator),
		sdk.NewAttribute(types.AttributeKeyLocalAddress, link.LocalAddress),
		sdk.NewAttribute(types.AttributeKeyPeerID, link.PeerId),
		sdk.NewAttribute(types.AttributeKeyRemoteIdentity, link.RemoteIdentity),
		sdk.NewAttribute(types.AttributeKeyEvidenceURI, evidence),
	))
	return nil
}

// --- Phase 4 ---

// pruneExpiredIdentityChallenges lives in identity_challenges.go.

// --- Phase 5 ---

func (k Keeper) expireUnverifiedContent(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.VerificationWindow.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		content, err := k.Content.Get(ctx, contentID)
		if err == nil && content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
			if err := k.Content.Set(ctx, contentID, content); err != nil {
				sdkCtx.Logger().Error("expireUnverifiedContent: Content.Set failed", "err", err)
			}

			bridgeKey := collections.Join(content.SubmittedBy, content.PeerId)
			binding, berr := k.BridgeBindings.Get(ctx, bridgeKey)
			if berr == nil {
				binding.ContentUnverified++
				binding.EpochUnverified++
				if err := k.BridgeBindings.Set(ctx, bridgeKey, binding); err != nil {
					sdkCtx.Logger().Error("expireUnverifiedContent: BridgeBindings.Set failed", "err", err)
				}
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeContentVerificationExpired,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
				sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId)))
		}
		if err := k.VerificationWindow.Remove(ctx, key); err != nil {
			sdkCtx.Logger().Error("expireUnverifiedContent: VerificationWindow.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 6 ---

func (k Keeper) releaseVerifierBondCommitments(ctx context.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	params, perr := k.Params.Get(ctx)
	if perr != nil {
		return pruned, perr
	}
	// held collects windows re-armed below; written after the walk so the
	// queue is not modified ahead of its own iterator
	var held []collections.Pair[int64, uint64]
	err := k.ChallengeWindow.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		record, err := k.VerificationRecords.Get(ctx, contentID)
		pending := err == nil && record.Outcome == types.VerificationOutcome_VERIFICATION_OUTCOME_PENDING
		status, live := k.contentStatus(ctx, contentID)
		switch {
		case pending && live && status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN:
			// FED-104: HIDDEN content (awaiting require_review, or hidden
			// by a member) cannot be challenged, so its window must not
			// lapse and free the verifier's bond meanwhile. Hold the
			// commitment and look again one window later; a restore
			// restarts the window (ModerateContent), and a prune lets the
			// next pass release it.
			record.ChallengeWindowEnds = now + int64(params.ChallengeWindow.Seconds())
			if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
				// FED-204: keep the queue entry so the hold is retried
				// next block instead of stranding a PENDING record with
				// no window entry.
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: VerificationRecords.Set failed", "err", err)
				pruned++
				return false, nil
			}
			held = append(held, collections.Join(record.ChallengeWindowEnds, contentID))
		case pending && live && status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED:
			// FED-202: content rejected by the committee while its
			// verification was still pending (a reject that reached the
			// record before ModerateContent settled it, or a record from
			// before that settlement existed). The verification does not
			// stand: release the commitment with no accuracy credit.
			record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_OVERTURNED
			k.releaseVerifierCommitment(ctx, &record)
			if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: VerificationRecords.Set failed", "err", err)
			}
			sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(types.EventTypeVerificationRejected,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
				sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier)))
		case pending:
			record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CONFIRMED
			// Release the verifier's committed bond back to available
			// (sets CommitmentReleased on the record) and bump the
			// per-module unchallenged counter.
			k.releaseVerifierCommitment(ctx, &record)
			activity, getErr := k.VerifierActivity.Get(ctx, record.Verifier)
			if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: VerifierActivity.Get failed", "err", getErr)
			}
			if activity.Address == "" {
				activity.Address = record.Verifier
			}
			activity.UnchallengedVerifications++
			if err := k.VerifierActivity.Set(ctx, record.Verifier, activity); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: VerifierActivity.Set failed", "err", err)
			}
			if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: VerificationRecords.Set failed", "err", err)
			}
		}
		if err := k.ChallengeWindow.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: ChallengeWindow.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	})
	for _, key := range held {
		if err := k.ChallengeWindow.Set(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("releaseVerifierBondCommitments: ChallengeWindow.Set failed", "err", err)
		}
	}
	return pruned, err
}

// contentStatus returns the content record's status and whether the
// record still exists (a pruned record reports false).
func (k Keeper) contentStatus(ctx context.Context, contentID uint64) (types.FederatedContentStatus, bool) {
	content, err := k.Content.Get(ctx, contentID)
	if err != nil {
		return 0, false
	}
	return content.Status, true
}

// --- Phase 7 ---

func (k Keeper) expireArbiterResolutions(ctx context.Context, sdkCtx sdk.Context, now int64, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	err := k.ArbiterResolutionQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		record, rerr := k.VerificationRecords.Get(ctx, contentID)
		// Only the entry written for the record's current round may act on
		// it. A stale entry (its round settled early and a new round began
		// since) is dropped untouched: a new round cannot start while its
		// predecessor's arbiter data remains, so whatever arbiter data
		// exists now belongs to the current round. A missing record means
		// peer removal already cleaned up.
		current := rerr == nil &&
			(record.ArbiterResolutionDeadline == 0 || record.ArbiterResolutionDeadline == key.K1())
		if !current {
			if err := k.ArbiterResolutionQueue.Remove(ctx, key); err != nil {
				sdkCtx.Logger().Error("expireArbiterResolutions: ArbiterResolutionQueue.Remove failed", "err", err)
			}
			pruned++
			return false, nil
		}

		// A stashed PendingVerifierVerdict means quorum landed and the
		// escalation queue (Phase 8) applies it — leave the record alone.
		// An EscalatedChallenge means the verdict was escalated and the
		// jury path owns resolution. Content no longer CHALLENGED /
		// DISPUTED was already settled (or this entry is being revisited
		// to finish its cleanup). Anything else is a genuine no-quorum
		// exit: this walk settles the content and releases the verifier's
		// commitment. Before this, both leaked forever — the content sat
		// DISPUTED/CHALLENGED until content_ttl pruned it, and the
		// commitment until a slash happened to free it.
		//
		// The arbiter_resolution_expired event is emitted only on that
		// no-quorum branch. Emitting it for every walked entry (as this
		// did originally) labelled early-resolved records "expired" when
		// the window had simply been consumed by a verdict.
		//
		// settleNoQuorum re-checks those guards; it runs in its own cache
		// context and a failure is retried from SettlementRetryQueue with
		// nothing committed (FED-29).
		k.runSettlement(ctx, contentID, types.SettlementKind_SETTLEMENT_KIND_NO_QUORUM, now,
			func(ctx context.Context) error { return k.settleNoQuorum(ctx, contentID, now) })

		// Delete the round's arbiter data against the prune budget. If the
		// budget runs out the entry stays and the next block resumes the
		// cleanup; the settlement above does not repeat (the content is no
		// longer in dispute, or a failed settlement is owed a retry and
		// the retry queue, not this entry, re-runs it).
		deleted, done := k.cleanupArbiterDataBudget(ctx, contentID, maxPrune-pruned)
		pruned += deleted
		if !done {
			return true, nil
		}
		if err := k.ArbiterResolutionQueue.Remove(ctx, key); err != nil {
			sdkCtx.Logger().Error("expireArbiterResolutions: ArbiterResolutionQueue.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	})
	return pruned, err
}

// --- Phase 8 ---

// finalizeAutoResolutions drains two queues sharing the prune budget:
// (a) the arbiter escalation queue, which fires the Phase 1 auto-
// verdict when the escalation window expires without an escalation,
// and (b) the jury deadline queue, which stamps TIMEOUT on
// EscalatedChallenges that the Operations Committee never resolved.
func (k Keeper) finalizeAutoResolutions(ctx context.Context, now int64, maxPrune, pruned uint64, params types.Params) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))

	if err := k.ArbiterEscalationQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		// Apply the stashed PendingVerifierVerdict. applyAutoVerdict acts
		// only for the entry keyed on the record's current escalation
		// deadline, only when no jury escalation is open and only while
		// the content is still in dispute; it zeroes the escrow and
		// clears the stash, so a revisit (below) never settles twice.
		//
		// The settlement runs in its own cache context; a failure commits
		// nothing and is retried from SettlementRetryQueue (FED-29).
		k.runSettlement(ctx, contentID, types.SettlementKind_SETTLEMENT_KIND_AUTO_VERDICT, now,
			func(ctx context.Context) error { return k.applyAutoVerdict(ctx, contentID, key.K1(), now, params) })
		if record, rerr := k.VerificationRecords.Get(ctx, contentID); rerr == nil && record.ArbiterEscalationDeadline == key.K1() {
			deleted, done := k.cleanupArbiterDataBudget(ctx, contentID, maxPrune-pruned)
			pruned += deleted
			if !done {
				return true, nil
			}
		}
		if err := k.ArbiterEscalationQueue.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("finalizeAutoResolutions: ArbiterEscalationQueue.Remove failed", "err", err)
		}
		pruned++
		return false, nil
	}); err != nil {
		return pruned, err
	}

	if pruned >= maxPrune {
		return pruned, nil
	}
	juryRng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))
	var stale []collections.Pair[int64, uint64]
	err := k.EscalatedChallengeDeadline.Walk(ctx, juryRng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		contentID := key.K2()
		// applyJuryVerdict removes both the EscalatedChallenge entry
		// and its deadline-queue entry. An entry whose EscalatedChallenge
		// is gone (or carries another deadline) is stale and dropped.
		if esc, eerr := k.EscalatedChallenges.Get(ctx, contentID); eerr != nil || esc.JuryDeadline != key.K1() {
			stale = append(stale, key)
		} else if !k.runSettlement(ctx, contentID, types.SettlementKind_SETTLEMENT_KIND_JURY_TIMEOUT, now,
			func(ctx context.Context) error {
				return k.applyJuryVerdict(ctx, contentID, types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT, now, params)
			}) {
			// Nothing committed, so the escalation and its deadline entry
			// are still there. The retry queue owns the TIMEOUT now; drop
			// the deadline entry so this walk does not re-run it every
			// block. The escalation stays open for MsgResolveEscalatedChallenge.
			stale = append(stale, key)
		}
		pruned++
		return false, nil
	})
	for _, key := range stale {
		if err := k.EscalatedChallengeDeadline.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("finalizeAutoResolutions: EscalatedChallengeDeadline.Remove failed", "err", err)
		}
	}
	return pruned, err
}

// --- Phase 9 ---

// processPeerRemovalQueue drains the data of removed peers in the order spec
// §9 Phase 9 lays down: content (cancelling active disputes), identity links
// and pending identity challenges, reputation attestations, outbound
// attestations, bridge bindings, the peer policy, and finally the peer
// record itself. Every phase shares the EndBlocker prune budget; a phase
// that runs out leaves its flag false and resumes next block.
func (k Keeper) processPeerRemovalQueue(ctx context.Context, sdkCtx sdk.Context, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	err := k.PeerRemovalQueue.Walk(ctx, nil, func(peerID string, state types.PeerRemovalState) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if !state.ContentDone {
			rng := collections.NewPrefixedPairRange[string, uint64](peerID)
			if err := k.ContentByPeer.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
				if pruned >= maxPrune {
					return true, nil
				}
				contentID := key.K2()
				// Arbiter data first, against the budget; a content with
				// more than the remaining budget resumes next block.
				deleted, cleaned := k.cleanupArbiterDataBudget(ctx, contentID, maxPrune-pruned)
				pruned += deleted
				if !cleaned {
					return true, nil
				}
				content, err := k.Content.Get(ctx, contentID)
				if err == nil {
					k.cancelContentVerification(ctx, sdkCtx, content)
					k.removeContentRecord(ctx, content)
				}
				if err := k.ContentByPeer.Remove(ctx, key); err != nil {
					sdkCtx.Logger().Error("processPeerRemovalQueue: ContentByPeer.Remove failed", "err", err)
				}
				pruned++
				state.LastPrunedContentId = contentID
				return false, nil
			}); err != nil {
				sdk.UnwrapSDKContext(sdkCtx).Logger().Error("processPeerRemovalQueue: ContentByPeer.Walk failed", "err", err)
			}
			if pruned < maxPrune {
				state.ContentDone = true
			}
		}
		if state.ContentDone && !state.LinksDone && pruned < maxPrune {
			// last_pruned_content_id is spent once content is done; the
			// outbound phase reuses it as its id cursor, so it starts
			// from zero here (this also resets a queue entry written by
			// a binary that left the content id in it).
			state.LastPrunedContentId = 0
			if k.removePeerIdentityLinks(ctx, sdkCtx, peerID, maxPrune, &pruned) {
				state.LinksDone = true
			}
		}
		if state.LinksDone && !state.AttestationsDone && pruned < maxPrune {
			if k.removePeerRepAttestations(ctx, peerID, maxPrune, &pruned) {
				state.AttestationsDone = true
			}
		}
		if state.AttestationsDone && !state.OutboundDone && pruned < maxPrune {
			if k.removePeerOutboundAttestations(ctx, peerID, maxPrune, &pruned, &state.LastPrunedContentId) {
				state.OutboundDone = true
			}
		}
		if state.OutboundDone && !state.BridgesDone && pruned < maxPrune {
			// Bindings still present here belong to operators that never
			// unbonded before a SUSPENDED peer was removed (the
			// stranded-peer escape hatch, MsgRemovePeer step 2a), or to
			// operators that reached RETIRED/SLASHED meanwhile. Bond
			// accounting stays with x/service; federation only drops the
			// binding, so an operator keeps its bond and unbonds normally.
			//
			// Every index goes with the binding, BindingsByOperator
			// included (leaving it would trip
			// BindingsByOperatorIndexInvariant), and each binding is
			// charged to the prune budget like every other phase.
			serviceType := ""
			if peer, perr := k.Peers.Get(ctx, peerID); perr == nil {
				serviceType, _ = serviceTypeForPeer(peer.Type)
			}
			var bridgeKeys []collections.Pair[string, string]
			rng := collections.NewPrefixedPairRange[string, string](peerID)
			bridgesDone := true
			if err := k.BridgesByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
				if pruned+uint64(len(bridgeKeys)) >= maxPrune {
					bridgesDone = false
					return true, nil
				}
				bridgeKeys = append(bridgeKeys, key)
				return false, nil
			}); err != nil {
				sdk.UnwrapSDKContext(sdkCtx).Logger().Error("processPeerRemovalQueue: BridgesByPeer.Walk failed", "err", err)
			}
			for _, key := range bridgeKeys {
				opAddr := key.K2()
				if err := k.BridgeBindings.Remove(ctx, collections.Join(opAddr, peerID)); err != nil {
					sdkCtx.Logger().Error("processPeerRemovalQueue: BridgeBindings.Remove failed", "err", err)
				}
				if err := k.BridgesByPeer.Remove(ctx, key); err != nil {
					sdkCtx.Logger().Error("processPeerRemovalQueue: BridgesByPeer.Remove failed", "err", err)
				}
				if serviceType != "" {
					if err := k.BindingsByOperator.Remove(ctx, collections.Join3(serviceType, opAddr, peerID)); err != nil {
						sdkCtx.Logger().Error("processPeerRemovalQueue: BindingsByOperator.Remove failed", "err", err)
					}
				}
				pruned++
			}
			state.BridgesDone = bridgesDone
		}
		if state.BridgesDone && !state.PolicyDone {
			if err := k.PeerPolicies.Remove(ctx, peerID); err != nil {
				sdkCtx.Logger().Error("processPeerRemovalQueue: PeerPolicies.Remove failed", "err", err)
			}
			state.PolicyDone = true
		}
		if state.ContentDone && state.LinksDone && state.AttestationsDone &&
			state.OutboundDone && state.BridgesDone && state.PolicyDone {
			if err := k.removePeer(ctx, peerID); err != nil {
				sdkCtx.Logger().Error("processPeerRemovalQueue: removePeer failed", "err", err)
			}
			if err := k.PeerRemovalQueue.Remove(ctx, peerID); err != nil {
				sdkCtx.Logger().Error("processPeerRemovalQueue: PeerRemovalQueue.Remove failed", "err", err)
			}
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypePeerCleanupComplete,
				sdk.NewAttribute(types.AttributeKeyPeerID, peerID)))
		} else {
			if err := k.PeerRemovalQueue.Set(ctx, peerID, state); err != nil {
				sdkCtx.Logger().Error("processPeerRemovalQueue: PeerRemovalQueue.Set failed", "err", err)
			}
		}
		return false, nil
	})
	return pruned, err
}

// removePeerIdentityLinks deletes every identity link to peerID and every
// pending identity challenge from it. Links are found through the
// (peer_id, remote_identity, local_address) claim index, so the walk only
// visits this peer's links, verified or not. Each link is deleted outright
// — the per-user link count is decremented, so the user can link again
// (including to the same peer id once it is re-registered) — and a
// verified link emits identity_link_revoked as its audit trail. Reports
// whether both sets are fully drained.
func (k Keeper) removePeerIdentityLinks(ctx context.Context, sdkCtx sdk.Context, peerID string, maxPrune uint64, pruned *uint64) bool {
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("removePeerIdentityLinks: Params.Get failed", "err", getErr)
	}
	done := true
	var claims []collections.Triple[string, string, string]
	rng := collections.NewPrefixedTripleRange[string, string, string](peerID)
	if err := k.IdentityClaimsByRemote.Walk(ctx, rng, func(key collections.Triple[string, string, string]) (bool, error) {
		if *pruned+uint64(len(claims)) >= maxPrune {
			done = false
			return true, nil
		}
		claims = append(claims, key)
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("removePeerIdentityLinks: IdentityClaimsByRemote.Walk failed", "err", err)
	}
	for _, key := range claims {
		k.removeIdentityLink(ctx, sdkCtx, key.K3(), peerID, params)
		// A claim entry without a live link (or for another remote
		// identity) is dropped too.
		if err := k.IdentityClaimsByRemote.Remove(ctx, key); err != nil {
			sdkCtx.Logger().Error("removePeerIdentityLinks: IdentityClaimsByRemote.Remove failed", "err", err)
		}
		*pruned++
	}
	if !done {
		return false
	}

	// Pending identity challenges from this peer, through the per-peer
	// index rather than a scan of every challenge.
	return k.removePeerPendingChallenges(ctx, peerID, maxPrune, pruned)
}

// removeIdentityLink deletes the (localAddr, peerID) link record, its claim
// and reverse-index entries, its unverified-expiry entry and its share of
// the per-user link count.
func (k Keeper) removeIdentityLink(ctx context.Context, sdkCtx sdk.Context, localAddr, peerID string, params types.Params) {
	linkKey := collections.Join(localAddr, peerID)
	link, err := k.IdentityLinks.Get(ctx, linkKey)
	if err != nil {
		return
	}
	if err := k.IdentityLinks.Remove(ctx, linkKey); err != nil {
		sdkCtx.Logger().Error("removeIdentityLink: IdentityLinks.Remove failed", "err", err)
	}
	k.removeIdentityLinkIndexes(ctx, link)
	if link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_UNVERIFIED {
		if err := k.UnverifiedLinkExp.Remove(ctx, collections.Join3(k.linkUnverifiedExpiry(ctx, link), localAddr, peerID)); err != nil {
			sdkCtx.Logger().Error("removeIdentityLink: UnverifiedLinkExp.Remove failed", "err", err)
		}
	}
	_ = params
	cnt, getErr := k.IdentityLinkCount.Get(ctx, localAddr)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("removeIdentityLink: IdentityLinkCount.Get failed", "err", getErr)
	}
	if cnt > 0 {
		if err := k.setIdentityLinkCount(ctx, localAddr, cnt-1); err != nil {
			sdkCtx.Logger().Error("removeIdentityLink: IdentityLinkCount.Set failed", "err", err)
		}
	}
	if link.Status == types.IdentityLinkStatus_IDENTITY_LINK_STATUS_VERIFIED {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeIdentityLinkRevoked,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, localAddr),
			sdk.NewAttribute(types.AttributeKeyPeerID, peerID),
			sdk.NewAttribute(types.AttributeKeyRemoteIdentity, link.RemoteIdentity)))
	}
}

// removePeerRepAttestations deletes every reputation attestation sourced
// from peerID, with its expiry-queue entry, through the (peer_id,
// local_address) index so the walk visits only this peer's entries.
// Reports whether the peer's attestations are fully drained.
func (k Keeper) removePeerRepAttestations(ctx context.Context, peerID string, maxPrune uint64, pruned *uint64) bool {
	done := true
	var locals []string
	rng := collections.NewPrefixedPairRange[string, string](peerID)
	if err := k.RepAttestationsByPeer.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		if *pruned+uint64(len(locals)) >= maxPrune {
			done = false
			return true, nil
		}
		locals = append(locals, key.K2())
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePeerRepAttestations: RepAttestationsByPeer.Walk failed", "err", err)
	}
	for _, local := range locals {
		k.removeRepAttestation(ctx, local, peerID)
		*pruned++
	}
	return done
}

// removePeerOutboundAttestations deletes every outbound attestation naming
// peerID. OutboundAttestations is keyed by a global sequence with no peer
// index and can be large, so the walk resumes from *cursor and every entry
// it visits — match or not — consumes budget, keeping the total work O(N)
// across blocks. Reports whether the walk reached the end.
func (k Keeper) removePeerOutboundAttestations(ctx context.Context, peerID string, maxPrune uint64, pruned *uint64, cursor *uint64) bool {
	done := true
	var matched []uint64
	rng := new(collections.Range[uint64]).StartInclusive(*cursor)
	if err := k.OutboundAttestations.Walk(ctx, rng, func(id uint64, att types.OutboundAttestation) (bool, error) {
		if *pruned >= maxPrune {
			done = false
			return true, nil
		}
		if att.PeerId == peerID {
			matched = append(matched, id)
		}
		*pruned++
		*cursor = id + 1
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removePeerOutboundAttestations: OutboundAttestations.Walk failed", "err", err)
	}
	for _, id := range matched {
		if err := k.OutboundAttestations.Remove(ctx, id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("removePeerOutboundAttestations: OutboundAttestations.Remove failed", "err", err)
		}
	}
	return done
}

// --- Phase 11 ---

func (k Keeper) monitorBridgeOperators(ctx context.Context, sdkCtx sdk.Context, now int64, params types.Params) error {
	// Bound the walk to maxPrunePerBlock to prevent unbounded iteration every block.
	var checked uint64
	maxCheck := params.MaxPrunePerBlock
	return k.BridgeBindings.Walk(ctx, nil, func(_ collections.Pair[string, string], binding types.BridgeBinding) (bool, error) {
		if checked >= maxCheck {
			return true, nil
		}
		checked++
		// Suspended bindings don't get inactivity warnings (the operator
		// is in UNDERFUNDED state on x/service; warning is redundant).
		if binding.Suspended {
			return false, nil
		}
		epochSec := int64(params.RateLimitWindow.Seconds())
		if epochSec > 0 && binding.LastSubmissionAt > 0 {
			epochsSince := (now - binding.LastSubmissionAt) / epochSec
			if commontypes.Saturate[uint64](epochsSince) > params.BridgeInactivityThreshold {
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeBridgeInactiveWarning,
					sdk.NewAttribute(types.AttributeKeyOperator, binding.Address),
					sdk.NewAttribute(types.AttributeKeyPeerID, binding.PeerId)))
			}
		}
		// Stake-insufficient monitoring removed: x/service emits
		// service.operator_underfunded directly on the transition, and
		// federation's AfterOperatorUnderfunded hook flips the binding's
		// suspended flag. Off-chain monitors should watch
		// service.operator_underfunded for this signal.
		return false, nil
	})
}

// --- Phase 12 ---

func (k Keeper) cleanStaleRateLimitCounters(ctx context.Context, now int64, params types.Params) error {
	// Bound all walks to maxPrunePerBlock to prevent unbounded iteration.
	maxPrune := params.MaxPrunePerBlock
	var pruned uint64

	// Per-block caps: at most one entry per direction per block. Prune
	// everything strictly below the current height — the current block's
	// entry is still live until EndBlocker returns; deleting it here is
	// fine because no further txs can land in this block.
	currentHeight := sdk.UnwrapSDKContext(ctx).BlockHeight()
	err := k.InboundPerBlock.Walk(ctx, nil, func(height int64, _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if height < currentHeight {
			if err := k.InboundPerBlock.Remove(ctx, height); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("cleanStaleRateLimitCounters: InboundPerBlock.Remove failed", "err", err)
			}
			pruned++
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	err = k.OutboundPerBlock.Walk(ctx, nil, func(height int64, _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if height < currentHeight {
			if err := k.OutboundPerBlock.Remove(ctx, height); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("cleanStaleRateLimitCounters: OutboundPerBlock.Remove failed", "err", err)
			}
			pruned++
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	// Per-peer sliding-window counters: keep two windows of history so
	// the current-window calculation can still consult the previous
	// window; anything older is dead state.
	windowSec := int64(params.RateLimitWindow.Seconds())
	if windowSec <= 0 {
		return nil
	}
	cutoff := now - 2*windowSec

	err = k.InboundRateLimits.Walk(ctx, nil, func(key collections.Pair[string, int64], _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if key.K2() < cutoff {
			if err := k.InboundRateLimits.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("cleanStaleRateLimitCounters: InboundRateLimits.Remove failed", "err", err)
			}
			pruned++
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	return k.OutboundRateLimits.Walk(ctx, nil, func(key collections.Pair[string, int64], _ uint64) (bool, error) {
		if pruned >= maxPrune {
			return true, nil
		}
		if key.K2() < cutoff {
			if err := k.OutboundRateLimits.Remove(ctx, key); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("cleanStaleRateLimitCounters: OutboundRateLimits.Remove failed", "err", err)
			}
			pruned++
		}
		return false, nil
	})
}

// --- Helpers ---

func (k Keeper) cleanupArbiterData(ctx context.Context, contentID uint64) {
	rng := collections.NewPrefixedPairRange[uint64, string](contentID)
	if err := k.ArbiterSubmissions.Walk(ctx, rng, func(key collections.Pair[uint64, string], _ types.ArbiterHashSubmission) (bool, error) {
		if err := k.ArbiterSubmissions.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterData: ArbiterSubmissions.Remove failed", "err", err)
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterData: ArbiterSubmissions.Walk failed", "err", err)
	}
	if err := k.ArbiterHashCounts.Walk(ctx, rng, func(key collections.Pair[uint64, string], _ uint32) (bool, error) {
		if err := k.ArbiterHashCounts.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterData: ArbiterHashCounts.Remove failed", "err", err)
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterData: ArbiterHashCounts.Walk failed", "err", err)
	}
}
