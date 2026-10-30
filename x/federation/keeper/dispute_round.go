package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"
	servicetypes "sparkdream/x/service/types"
)

// isDisputeStatus reports whether a content status means a verification
// dispute is still open (and its escrow undisbursed). Every settlement path
// moves the content off these statuses as it disburses, so the status is
// the single-shot marker the settlement paths check before acting.
func isDisputeStatus(status types.FederatedContentStatus) bool {
	return status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED ||
		status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED
}

// arbitrationOpen reports whether arbiters may still vote on the dispute
// round of record at blockTime, and why not otherwise:
//
//   - an escalation hands the round to the Operations Committee, so no
//     vote may re-arm the automatic verdict after it;
//   - the first quorum is final: once a verdict is stashed no later vote
//     can replace it or push its escalation deadline back;
//   - votes are accepted only until arbiter_resolution_deadline.
func (k Keeper) arbitrationOpen(ctx context.Context, record types.VerificationRecord, blockTime int64) error {
	if has, _ := k.EscalatedChallenges.Has(ctx, record.ContentId); has {
		return errorsmod.Wrap(types.ErrArbitrationClosed, "the dispute was escalated to the Operations Committee")
	}
	if record.PendingVerifierVerdict != types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED {
		return errorsmod.Wrap(types.ErrArbitrationClosed, "the arbiter quorum was already reached")
	}
	if record.ArbiterResolutionDeadline != 0 && blockTime > record.ArbiterResolutionDeadline {
		return errorsmod.Wrapf(types.ErrArbitrationClosed, "arbiter resolution deadline %d has passed", record.ArbiterResolutionDeadline)
	}
	return nil
}

// hasArbiterData reports whether any arbiter submission is still stored for
// contentID. A new dispute round must not start while one is: the old
// round's cleanup would otherwise delete the new round's votes.
func (k Keeper) hasArbiterData(ctx context.Context, contentID uint64) bool {
	found := false
	rng := collections.NewPrefixedPairRange[uint64, string](contentID)
	if err := k.ArbiterSubmissions.Walk(ctx, rng, func(collections.Pair[uint64, string], types.ArbiterHashSubmission) (bool, error) {
		found = true
		return true, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("hasArbiterData: ArbiterSubmissions.Walk failed", "err", err)
	}
	if found {
		return true
	}
	if err := k.ArbiterHashCounts.Walk(ctx, rng, func(collections.Pair[uint64, string], uint32) (bool, error) {
		found = true
		return true, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("hasArbiterData: ArbiterHashCounts.Walk failed", "err", err)
	}
	return found
}

// cleanupArbiterDataBudget deletes up to budget arbiter submissions and hash
// counts of contentID. It reports how many entries it deleted and whether
// the content's arbiter data is now gone. The set is large but finite (one
// submission per identified operator or shield identity), and it can no
// longer grow once the round is closed, so a caller that keeps its queue
// entry and resumes next block always converges.
func (k Keeper) cleanupArbiterDataBudget(ctx context.Context, contentID uint64, budget uint64) (uint64, bool) {
	rng := collections.NewPrefixedPairRange[uint64, string](contentID)
	var subKeys, countKeys []collections.Pair[uint64, string]
	done := true
	if err := k.ArbiterSubmissions.Walk(ctx, rng, func(key collections.Pair[uint64, string], _ types.ArbiterHashSubmission) (bool, error) {
		if uint64(len(subKeys)) >= budget {
			done = false
			return true, nil
		}
		subKeys = append(subKeys, key)
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterDataBudget: ArbiterSubmissions.Walk failed", "err", err)
	}
	if done {
		if err := k.ArbiterHashCounts.Walk(ctx, rng, func(key collections.Pair[uint64, string], _ uint32) (bool, error) {
			if commontypes.Saturate[uint64](len(subKeys)+len(countKeys)) >= budget {
				done = false
				return true, nil
			}
			countKeys = append(countKeys, key)
			return false, nil
		}); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterDataBudget: ArbiterHashCounts.Walk failed", "err", err)
		}
	}
	for _, key := range subKeys {
		if err := k.ArbiterSubmissions.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterDataBudget: ArbiterSubmissions.Remove failed", "err", err)
		}
	}
	for _, key := range countKeys {
		if err := k.ArbiterHashCounts.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("cleanupArbiterDataBudget: ArbiterHashCounts.Remove failed", "err", err)
		}
	}
	return commontypes.Saturate[uint64](len(subKeys) + len(countKeys)), done
}

// fileChallengeReport opens a system report against the bridge
// operator who submitted the now-rejected content. Phase 6 of the
// federation→service migration: federation calls
// serviceKeeper.OpenSystemReport with a dedupe key derived from the
// content_id, so repeat calls (the pending-report retry, re-orgs) don't
// open duplicate reports.
//
// Returns the x/service report id, or 0 with a nil error when there is
// nothing to file against (no service keeper wired in standalone-mode
// tests, or a SPARK_DREAM peer, which has no service-backed bridge).
func (k Keeper) fileChallengeReport(ctx context.Context, content types.FederatedContent, evidenceURI string) (uint64, error) {
	sk := k.serviceKeeper()
	if sk == nil {
		return 0, nil
	}

	peer, err := k.Peers.Get(ctx, content.PeerId)
	if err != nil {
		return 0, errorsmod.Wrapf(types.ErrPeerNotFound, "peer %q not found", content.PeerId)
	}
	serviceType, err := serviceTypeForPeer(peer.Type)
	if err != nil {
		// SPARK_DREAM peer; no service-backed bridge to slash.
		return 0, nil
	}

	operatorAddr, err := k.addressCodec.StringToBytes(content.SubmittedBy)
	if err != nil {
		return 0, errorsmod.Wrap(err, "invalid submitted_by address")
	}
	callerAddr := k.authKeeper.GetModuleAddress(types.ModuleName)

	// dedupeKey = sha256(content_id || ":federation:challenge"). Per
	// migration plan: include the challenge ID so a re-orged re-fire
	// of the same content's challenge resolves to the same report.
	idBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(idBuf, content.Id)
	keyHash := sha256.Sum256(append(idBuf, []byte(":federation:challenge")...))

	// slashBps=0 → service falls back to ServiceTypeConfig.challenge_default_slash_bps.
	reportID, _, err := sk.OpenSystemReport(ctx, callerAddr, operatorAddr, serviceType, 0, evidenceURI, keyHash[:])
	return reportID, err
}

// fileOrQueueChallengeReport files the operator report owed for content and
// records the outcome on record (the caller persists it):
//
//   - filed: challenge_report_id is set and any pending entry cleared;
//   - refused by x/service's per-caller system-report rate limit: the
//     evidence is kept in pending_challenge_report and the content queued
//     in PendingChallengeReports, and the EndBlocker retries it with the
//     same dedupe key — the report is delayed, never dropped;
//   - any other error (the operator already left x/service, ...) cannot
//     succeed later, so it is evented and dropped.
func (k Keeper) fileOrQueueChallengeReport(ctx context.Context, content types.FederatedContent, record *types.VerificationRecord, evidenceURI string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	reportID, err := k.fileChallengeReport(ctx, content, evidenceURI)
	switch {
	case err == nil:
		if reportID != 0 {
			record.ChallengeReportId = reportID
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeReportFiled,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", content.Id)),
				sdk.NewAttribute(types.AttributeKeyOperator, content.SubmittedBy),
				sdk.NewAttribute("report_id", fmt.Sprintf("%d", reportID)),
			))
		}
		record.PendingChallengeReport = ""
		if err := k.PendingChallengeReports.Remove(ctx, content.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("fileOrQueueChallengeReport: PendingChallengeReports.Remove failed", "err", err)
		}
	case errors.Is(err, servicetypes.ErrSystemReportRateLimited):
		record.PendingChallengeReport = evidenceURI
		if err := k.PendingChallengeReports.Set(ctx, content.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("fileOrQueueChallengeReport: PendingChallengeReports.Set failed", "err", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeReportDeferred,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", content.Id)),
			sdk.NewAttribute(types.AttributeKeyOperator, content.SubmittedBy),
			sdk.NewAttribute(types.AttributeKeyError, err.Error()),
		))
	default:
		record.PendingChallengeReport = ""
		if err := k.PendingChallengeReports.Remove(ctx, content.Id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("fileOrQueueChallengeReport: PendingChallengeReports.Remove failed", "err", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFederationHookFailure,
			sdk.NewAttribute("hook", "fileChallengeReport"),
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", content.Id)),
			sdk.NewAttribute(types.AttributeKeyError, err.Error()),
		))
	}
}

// retryPendingChallengeReports re-files operator reports that x/service
// refused under its rate limit. Each visited entry costs one unit of the
// prune budget. The walk stops at the first entry that is still rate
// limited: the limit is per caller, so every later entry would be refused
// too.
func (k Keeper) retryPendingChallengeReports(ctx context.Context, maxPrune, pruned uint64) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	var ids []uint64
	err := k.PendingChallengeReports.Walk(ctx, nil, func(id uint64) (bool, error) {
		if pruned+uint64(len(ids)) >= maxPrune {
			return true, nil
		}
		ids = append(ids, id)
		return false, nil
	})
	if err != nil {
		return pruned, err
	}
	for _, id := range ids {
		pruned++
		record, rerr := k.VerificationRecords.Get(ctx, id)
		content, cerr := k.Content.Get(ctx, id)
		if rerr != nil || cerr != nil || record.PendingChallengeReport == "" {
			// Record or content gone (peer removal, TTL): nothing left to
			// report against.
			if err := k.PendingChallengeReports.Remove(ctx, id); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("retryPendingChallengeReports: PendingChallengeReports.Remove failed", "err", err)
			}
			continue
		}
		k.fileOrQueueChallengeReport(ctx, content, &record, record.PendingChallengeReport)
		if err := k.VerificationRecords.Set(ctx, id, record); err != nil {
			return pruned, err
		}
		if record.PendingChallengeReport != "" {
			// Still rate limited.
			break
		}
	}
	return pruned, nil
}
