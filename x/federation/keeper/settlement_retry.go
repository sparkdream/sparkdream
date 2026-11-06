package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// EndBlocker settlement retries (FED-29).
//
// Every settlement the EndBlocker performs (no-quorum expiry, automatic
// verdict, jury TIMEOUT) runs in its own cache context and commits only
// when every side effect succeeded. On failure nothing of it commits: the
// escrowed challenge / escalation fee stays on the record (the escrow-
// solvency invariant keeps holding), the content stays in dispute, and the
// settlement is retried from SettlementRetryQueue with doubling backoff.
// After MaxSettlementAttempts the record leaves the queue and keeps its
// escrow until peer removal (which refunds it) or, for a jury TIMEOUT, a
// committee MsgResolveEscalatedChallenge settles it.

// MaxSettlementAttempts bounds the attempts of one EndBlocker settlement.
const MaxSettlementAttempts = 5

// SettlementRetryBaseSeconds is the backoff before the first retry; it
// doubles with every further failure (5 min, 10 min, 20 min, 40 min).
const SettlementRetryBaseSeconds int64 = 300

// runSettlement runs settle in a cache context and commits it only on
// success. A failure is recorded on the VerificationRecord and queued for a
// retry (or abandoned after the last attempt). It reports whether the
// settlement committed.
func (k Keeper) runSettlement(ctx context.Context, contentID uint64, kind types.SettlementKind, now int64, settle func(ctx context.Context) error) bool {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	cacheCtx, write := sdkCtx.CacheContext()
	err := settle(cacheCtx)
	if err == nil {
		// A settlement that was owed a retry is settled now.
		if record, rerr := k.VerificationRecords.Get(cacheCtx, contentID); rerr == nil && settlementRetryOwed(record) {
			clearSettlementRetry(&record)
			err = k.VerificationRecords.Set(cacheCtx, contentID, record)
		}
	}
	if err == nil {
		write()
		return true
	}
	k.deferSettlement(ctx, contentID, kind, now, err)
	return false
}

func settlementRetryOwed(r types.VerificationRecord) bool {
	return r.SettlementRetryAt != 0 || r.SettlementAttempts != 0 ||
		r.SettlementRetryKind != types.SettlementKind_SETTLEMENT_KIND_UNSPECIFIED
}

// clearSettlementRetry resets the retry bookkeeping of a settled record.
// A queue entry left behind is stale (its key no longer matches
// settlement_retry_at) and the retry walk drops it.
func clearSettlementRetry(r *types.VerificationRecord) {
	r.SettlementRetryKind = types.SettlementKind_SETTLEMENT_KIND_UNSPECIFIED
	r.SettlementAttempts = 0
	r.SettlementRetryAt = 0
}

// deferSettlement records a failed settlement attempt on the record and
// queues the retry, or abandons it after MaxSettlementAttempts.
func (k Keeper) deferSettlement(ctx context.Context, contentID uint64, kind types.SettlementKind, now int64, settleErr error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record, err := k.VerificationRecords.Get(ctx, contentID)
	if err != nil {
		// Nothing left to settle against (peer removal refunded it).
		return
	}
	if record.SettlementRetryAt > 0 {
		if err := k.SettlementRetryQueue.Remove(ctx, collections.Join(record.SettlementRetryAt, contentID)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("deferSettlement: SettlementRetryQueue.Remove failed", "err", err)
		}
	}
	if record.SettlementRetryKind != kind {
		record.SettlementAttempts = 0
	}
	record.SettlementRetryKind = kind
	record.SettlementAttempts++
	attempt := record.SettlementAttempts
	abandoned := attempt >= MaxSettlementAttempts
	if abandoned {
		record.SettlementRetryAt = 0
	} else {
		record.SettlementRetryAt = now + SettlementRetryBaseSeconds<<(attempt-1)
		if err := k.SettlementRetryQueue.Set(ctx, collections.Join(record.SettlementRetryAt, contentID)); err != nil {
			sdkCtx.Logger().Error("settlement retry: queue failed", "content_id", contentID, "error", err)
		}
	}
	if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
		sdkCtx.Logger().Error("settlement retry: persist record failed", "content_id", contentID, "error", err)
	}
	sdkCtx.Logger().Warn("EndBlocker settlement failed; nothing committed",
		"content_id", contentID, "step", kind.String(), "attempt", attempt, "abandoned", abandoned, "error", settleErr)
	if abandoned {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeSettlementAbandoned,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
			sdk.NewAttribute("step", kind.String()),
			sdk.NewAttribute("attempt", fmt.Sprintf("%d", attempt)),
			sdk.NewAttribute(types.AttributeKeyError, settleErr.Error()),
		))
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeSettlementFailure,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute("step", kind.String()),
		sdk.NewAttribute("attempt", fmt.Sprintf("%d", attempt)),
		sdk.NewAttribute("retry_at", fmt.Sprintf("%d", record.SettlementRetryAt)),
		sdk.NewAttribute(types.AttributeKeyError, settleErr.Error()),
	))
}

// settleNoQuorum settles a dispute whose arbiter resolution window closed
// without a quorum: the verifier's commitment is released, an escrowed
// challenge fee follows the no-finding rule (half back, half burned), and
// the content leaves the dispute. It acts only while no verdict is stashed,
// no jury escalation is open and the dispute is still open, so a retry
// after another path settled it is a no-op.
func (k Keeper) settleNoQuorum(ctx context.Context, contentID uint64, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	record, rerr := k.VerificationRecords.Get(ctx, contentID)
	if rerr != nil {
		return nil
	}
	escalated, getErr := k.EscalatedChallenges.Has(ctx, contentID)
	if getErr != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("Has failed", "err", getErr)
	}
	content, cerr := k.Content.Get(ctx, contentID)
	openDispute := (cerr == nil && isDisputeStatus(content.Status)) ||
		(cerr != nil && !record.CommitmentReleased)
	if record.PendingVerifierVerdict != types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED ||
		escalated || !openDispute {
		return nil
	}
	if err := k.releaseVerifierCommitmentErr(ctx, &record); err != nil {
		return fmt.Errorf("release commitment: %w", err)
	}
	// No quorum is no finding: the escrowed challenge fee follows the
	// jury-TIMEOUT rule (half back, half burned) instead of sitting in the
	// module account forever. Only a CHALLENGED record holds an
	// undisbursed fee; when the content was TTL-pruned mid-dispute the
	// record's escrow is the only trace left and is split all the same
	// (FED-103), or the fee would sit in the module account forever.
	if (cerr == nil && content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED) ||
		(cerr != nil && !record.EscrowedChallengeFee.IsNil() && record.EscrowedChallengeFee.IsPositive()) {
		if err := k.splitChallengeFeeNoFinding(ctx, record); err != nil {
			return err
		}
		record.EscrowedChallengeFee = math.ZeroInt()
	}
	record.LastChallengeResolvedAt = now
	clearSettlementRetry(&record)
	if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeArbiterResolutionExpired,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID))))
	k.settleNoQuorumContent(ctx, sdkCtx, contentID, record.Verifier)
	return nil
}

// retryFailedSettlements re-runs the EndBlocker settlements whose retry is
// due. Each visited entry costs one unit of the prune budget.
func (k Keeper) retryFailedSettlements(ctx context.Context, now int64, maxPrune, pruned uint64, params types.Params) (uint64, error) {
	if pruned >= maxPrune {
		return pruned, nil
	}
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join(now+1, uint64(0)))
	var due []collections.Pair[int64, uint64]
	if err := k.SettlementRetryQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned+uint64(len(due)) >= maxPrune {
			return true, nil
		}
		due = append(due, key)
		return false, nil
	}); err != nil {
		return pruned, err
	}
	for _, key := range due {
		pruned++
		contentID := key.K2()
		if err := k.SettlementRetryQueue.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("retryFailedSettlements: SettlementRetryQueue.Remove failed", "err", err)
		}
		record, err := k.VerificationRecords.Get(ctx, contentID)
		if err != nil || record.SettlementRetryAt != key.K1() {
			// Stale: the record is gone or another path settled it.
			continue
		}
		kind := record.SettlementRetryKind
		var settle func(ctx context.Context) error
		switch kind {
		case types.SettlementKind_SETTLEMENT_KIND_NO_QUORUM:
			settle = func(ctx context.Context) error { return k.settleNoQuorum(ctx, contentID, now) }
		case types.SettlementKind_SETTLEMENT_KIND_AUTO_VERDICT:
			deadline := record.ArbiterEscalationDeadline
			settle = func(ctx context.Context) error {
				return k.applyAutoVerdict(ctx, contentID, deadline, now, params)
			}
		case types.SettlementKind_SETTLEMENT_KIND_JURY_TIMEOUT:
			settle = func(ctx context.Context) error {
				return k.applyJuryVerdict(ctx, contentID, types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT, now, params)
			}
		default:
			continue
		}
		k.runSettlement(ctx, contentID, kind, now, settle)
	}
	return pruned, nil
}
