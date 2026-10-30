package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"
)

// applyAutoVerdict applies a stashed PendingVerifierVerdict against the
// VerificationRecord. Runs from finalizeAutoResolutions when the
// escalation window expires without escalation. Performs three things:
//
//  1. A verdict report to x/rep, which owns the shared accountability
//     record: per-kind upheld/overturned counters, verdict streaks, the
//     rolling accuracy ring, the overturn cooldown, and streak demotion.
//  2. DREAM bond slash on overturn (+ half-slash bounty minted to the
//     challenger per spec §7). SlashBond stamps last_slash_epoch, which
//     the reward distribution reads as its no-pay-this-epoch gate.
//  3. SPARK fee disbursement on the escrowed challenge fee — full
//     refund to challenger on UPHELD, 50/50 split (verifier reward /
//     burn) on REJECTED. Content status flips to REJECTED on UPHELD
//     and back to VERIFIED on REJECTED.
//
// Settlement is single-shot. The verdict applies only when the queue
// entry is the one written for the record's current quorum
// (entryDeadline == arbiter_escalation_deadline), no jury escalation is
// open (an escalated dispute belongs to MsgResolveEscalatedChallenge or
// the jury TIMEOUT, never to this path), and the content is still
// CHALLENGED / DISPUTED. Applying it zeroes the escrowed challenge fee
// and clears the stash, so nothing can pay or slash for the same dispute
// again.
//
// A downstream failure (bank send, x/rep call) is returned. The EndBlocker
// runs this in a cache context (runSettlement), so on failure nothing
// commits and the settlement is retried (FED-29).
func (k Keeper) applyAutoVerdict(
	ctx context.Context,
	contentID uint64,
	entryDeadline int64,
	blockTime int64,
	params types.Params,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	record, err := k.VerificationRecords.Get(ctx, contentID)
	if err != nil {
		return nil
	}
	if record.ArbiterEscalationDeadline != entryDeadline {
		// A queue entry from an earlier quorum of this content; the
		// current round has its own entry.
		return nil
	}
	if has, _ := k.EscalatedChallenges.Has(ctx, contentID); has {
		return nil
	}
	if content, cerr := k.Content.Get(ctx, contentID); cerr == nil && !isDisputeStatus(content.Status) {
		return nil
	}
	var settleErr error
	switch record.PendingVerifierVerdict {
	case types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT:
		settleErr = k.applyAutoVerdictRejected(ctx, contentID, &record, record.QuorumFalsifiedContent, params)
	case types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG:
		settleErr = k.applyAutoVerdictUpheld(ctx, contentID, &record, record.QuorumFalsifiedContent, blockTime, params)
	default:
		// UNSPECIFIED: escalation cleared the verdict OR no quorum was
		// reached. Nothing to apply.
		return nil
	}

	// The escrow is spent and the verdict consumed: zero the one and clear
	// the other so the record can never be settled a second time.
	record.EscrowedChallengeFee = math.ZeroInt()
	record.LastChallengeResolvedAt = blockTime
	record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED
	clearSettlementRetry(&record)
	if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
		sdkCtx.Logger().Warn("auto-verdict: persist record failed",
			"content_id", contentID, "error", err)
		settleErr = errors.Join(settleErr, err)
	}
	return settleErr
}

// applyAutoVerdictRejected handles CHALLENGE_REJECTED (verifier was right):
// counter bumps + escrowed fee split (50% to verifier as SPARK reward,
// 50% burned) + content stays VERIFIED.
//
// Takes record by pointer so the prior_rejected_challenges bump is
// visible to the caller (applyAutoVerdict / applyJuryVerdict persist
// the record after this returns).
func (k Keeper) applyAutoVerdictRejected(
	ctx context.Context,
	contentID uint64,
	record *types.VerificationRecord,
	contentFalsified bool,
	params types.Params,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var errs []error

	// Report the upheld verdict to x/rep. It bumps the per-kind upheld
	// counter, the accuracy ring, epoch_appeals_resolved and the streaks --
	// including how many consecutive upheld verdicts clear an overturn
	// streak, which is now BondedRoleConfig.upheld_to_reset_overturns rather
	// than a federation-local param read here.
	if k.late.repKeeper != nil {
		if err := k.late.repKeeper.RecordRoleOutcome(ctx,
			reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, record.Verifier,
			reptypes.ActionKindFederationVerify, true); err != nil {
			sdkCtx.Logger().Warn("auto-verdict (rejected): record outcome failed",
				"verifier", record.Verifier, "error", err)
			errs = append(errs, fmt.Errorf("record outcome: %w", err))
		}
	}
	consecutiveUpheld := k.verifierConsecutiveUpheld(ctx, record.Verifier)

	// The verifier was right, so their commitment goes back to available.
	// Without this, every vindicated verification leaked
	// params.VerifierSlashAmount of committed bond until a slash freed it
	// (P0.1 of the Mastodon live link).
	if err := k.releaseVerifierCommitmentErr(ctx, record); err != nil {
		errs = append(errs, fmt.Errorf("release commitment: %w", err))
	}

	// Bump prior_rejected_challenges so the next challenger pays the
	// escalating fee (matches MsgChallengeVerification's 2^N multiplier).
	record.PriorRejectedChallenges++

	// The verifier was right. For CHALLENGED content that means the
	// verification stands: restore it to VERIFIED. For DISPUTED content the
	// verifier had disagreed with the operator, so being right means the
	// operator's hash was false (contentFalsified): the content is REJECTED
	// and the operator's rejection counters bump (spec §3.9.2 step 4). Skip
	// if content is gone (peer removal could have pruned it during the
	// escalation window).
	if content, cerr := k.Content.Get(ctx, contentID); cerr == nil {
		if contentFalsified {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED
			k.bumpOperatorRejected(ctx, content)
		} else {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		}
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			sdkCtx.Logger().Warn("auto-verdict (rejected): persist content failed",
				"content_id", contentID, "error", err)
			errs = append(errs, err)
		}
	}

	// Disburse the escrowed challenge fee: 50% to verifier as SPARK
	// reward, 50% burned. No-op if the fee was zero (defensive — should
	// not happen if MsgChallengeVerification ran first).
	if err := k.disburseChallengeFeeOnRejected(ctx, *record); err != nil {
		errs = append(errs, err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeRejected,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier),
		sdk.NewAttribute(types.AttributeKeyChallenger, record.Challenger),
		sdk.NewAttribute("consecutive_upheld", fmt.Sprintf("%d", consecutiveUpheld)),
	))
	return errors.Join(errs...)
}

// applyAutoVerdictUpheld handles CHALLENGE_UPHELD (verifier was wrong):
// counter bumps + escalating overturn cooldown + DREAM slash + half-slash
// bounty to challenger + full challenge fee refund + content → REJECTED.
//
// Takes record by pointer for parity with applyAutoVerdictRejected;
// currently this helper doesn't mutate the record itself (the slash
// state lives on x/rep's RoleActivity and BondedRole), but the pointer
// signature lets future field updates flow back to the caller without
// re-discovering the bug fixed in PR #N.
func (k Keeper) applyAutoVerdictUpheld(
	ctx context.Context,
	contentID uint64,
	record *types.VerificationRecord,
	contentFalsified bool,
	blockTime int64,
	params types.Params,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var errs []error

	// Report the overturn to x/rep. It bumps the per-kind overturned counter
	// and the accuracy ring, advances the overturn streak, applies the
	// ESCALATING overturn cooldown (base * 2^(streak-1), capped at 7 days --
	// configured via BondedRoleConfig.overturn_cooldown_escalates, which
	// SyncVerifierBondedRoleConfig writes through from
	// verifier_overturn_base_cooldown), and demotes the bond once the streak
	// crosses the threshold.
	//
	// Ordering against the SlashBond below is not load-bearing in either
	// direction: SlashBond takes the harsher of its recomputed status and the
	// existing one, so a streak demotion applied here survives the slash.
	if k.late.repKeeper != nil {
		if err := k.late.repKeeper.RecordRoleOutcome(ctx,
			reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, record.Verifier,
			reptypes.ActionKindFederationVerify, false); err != nil {
			sdkCtx.Logger().Warn("auto-verdict (upheld): record outcome failed",
				"verifier", record.Verifier, "error", err)
			errs = append(errs, fmt.Errorf("record outcome: %w", err))
		}
	}
	consecutiveOverturns, slashCount, cooldownUntil := k.verifierOverturnState(ctx, record.Verifier)

	// Record that the verification was overturned, so nothing (e.g. a
	// moderation restore) can treat this record as a standing verification.
	record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_UPHELD

	// Slash DREAM bond. Half is later minted back to the challenger as
	// bounty (the SlashBond burn + MintDREAM half-back pattern conserves
	// the net "50% burned" outcome described in spec §7).
	//
	// The amount is the RESERVATION SNAPSHOT (record.CommittedAmount), not
	// the live param: MsgVerifyContent reserved exactly that much via
	// ReserveBond, and SlashBond decrements TotalCommittedBond by whatever
	// it slashes. Slashing the live param after a mid-dispute governance
	// change to verifier_slash_amount would free more or less than was
	// reserved and drift the committed-bond accounting permanently. Falls
	// back to the param only for records written before the snapshot
	// existed.
	slashAmount := record.CommittedAmount
	if slashAmount.IsNil() || !slashAmount.IsPositive() {
		slashAmount = params.VerifierSlashAmount
	}
	if k.late.repKeeper != nil && !slashAmount.IsNil() && slashAmount.IsPositive() {
		reason := fmt.Sprintf("federation: challenge upheld (slash_count=%d)", slashCount)
		if err := k.late.repKeeper.SlashBond(ctx,
			reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			record.Verifier,
			slashAmount,
			reason); err != nil {
			sdkCtx.Logger().Warn("verifier slash via rep keeper failed",
				"verifier", record.Verifier, "amount", slashAmount.String(), "error", err)
			errs = append(errs, fmt.Errorf("slash verifier bond: %w", err))
		} else {
			// The slash freed the whole commitment (SlashBond decrements
			// TotalCommittedBond itself), so the record is settled. Stamping
			// the flag here is what stops a later EndBlocker walk over a
			// stale ArbiterResolutionQueue entry from issuing a SECOND,
			// saturating ReleaseBond that would silently eat a different
			// concurrent commitment of the same verifier. Reachable today
			// via an early OpsComm MsgResolveEscalatedChallenge(UPHELD),
			// which clears PendingVerifierVerdict and tears down the
			// EscalatedChallenge while the arbiter resolution window is
			// still open — both of Phase 7's other guards go false.
			record.CommitmentReleased = true

			// Mint half-slash DREAM bounty to challenger. Skip when
			// challenger is empty (e.g. DISPUTED auto-resolution where
			// there was no challenger).
			bounty := slashAmount.QuoRaw(2)
			if bounty.IsPositive() && record.Challenger != "" {
				if challengerAddr, addrErr := sdk.AccAddressFromBech32(record.Challenger); addrErr == nil {
					if err := k.late.repKeeper.MintDREAM(ctx, challengerAddr, bounty); err != nil {
						sdkCtx.Logger().Warn("auto-verdict (upheld): mint challenger bounty failed",
							"challenger", record.Challenger, "amount", bounty.String(), "error", err)
						errs = append(errs, fmt.Errorf("mint challenger bounty: %w", err))
					}
				}
			}
		}
	}

	// The verifier was wrong. For CHALLENGED content the verifier had
	// matched the operator, so the operator's claim is falsified too:
	// content → REJECTED and the submitting operator's rejection counters
	// bump. For DISPUTED content the verifier had disagreed with the
	// operator, so the operator's original hash stands — content → VERIFIED,
	// operator not blamed (spec §3.9.2 step 4) — unless the arbiter quorum
	// matched neither of them (contentFalsified): then both were wrong and
	// the content is REJECTED.
	if content, cerr := k.Content.Get(ctx, contentID); cerr == nil {
		if contentFalsified {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED
			k.bumpOperatorRejected(ctx, content)
		} else {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		}
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			sdkCtx.Logger().Warn("auto-verdict (upheld): persist content failed",
				"content_id", contentID, "error", err)
			errs = append(errs, err)
		}
	}

	// Refund 100% of escrowed challenge fee to challenger.
	if err := k.refundChallengeFee(ctx, *record); err != nil {
		errs = append(errs, err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeUpheld,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier),
		sdk.NewAttribute(types.AttributeKeyChallenger, record.Challenger),
		sdk.NewAttribute(types.AttributeKeySlashAmount, slashAmountString(slashAmount)),
		sdk.NewAttribute("consecutive_overturns", fmt.Sprintf("%d", consecutiveOverturns)),
	))
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeVerifierCooldownApplied,
		sdk.NewAttribute("address", record.Verifier),
		sdk.NewAttribute("cooldown_until", fmt.Sprintf("%d", cooldownUntil)),
		sdk.NewAttribute("consecutive_overturns", fmt.Sprintf("%d", consecutiveOverturns)),
	))
	return errors.Join(errs...)
}

// bumpOperatorRejected increments the submitting operator's rejection
// counters for content whose hash a verdict found false.
//
// content_rejected was declared and documented from the start but never
// actually incremented — the counter sat at zero for every operator on
// chain. It is now load-bearing: epoch_rejected is the eligibility gate on
// operator compensation, so a falsified submission has to cost the
// operator the epoch's pay.
func (k Keeper) bumpOperatorRejected(ctx context.Context, content types.FederatedContent) {
	bridgeKey := collections.Join(content.SubmittedBy, content.PeerId)
	binding, err := k.BridgeBindings.Get(ctx, bridgeKey)
	if err != nil {
		return
	}
	binding.ContentRejected++
	binding.EpochRejected++
	if err := k.BridgeBindings.Set(ctx, bridgeKey, binding); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Warn("verdict: persist binding failed",
			"operator", content.SubmittedBy, "peer", content.PeerId, "error", err)
	}
}

// refundChallengeFee sends the full escrowed challenge fee back to the
// challenger. No-op when the fee was zero or the challenger address is
// invalid. The fee was escrowed into the federation module account at
// MsgChallengeVerification time.
func (k Keeper) refundChallengeFee(ctx context.Context, record types.VerificationRecord) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if record.EscrowedChallengeFee.IsNil() || !record.EscrowedChallengeFee.IsPositive() {
		return nil
	}
	if record.Challenger == "" {
		return nil
	}
	challengerAddr, err := sdk.AccAddressFromBech32(record.Challenger)
	if err != nil {
		sdkCtx.Logger().Warn("refund challenge fee: invalid challenger address",
			"challenger", record.Challenger, "error", err)
		return fmt.Errorf("refund challenge fee: %w", err)
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), record.EscrowedChallengeFee))
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, challengerAddr, coins); err != nil {
		sdkCtx.Logger().Warn("refund challenge fee: bank send failed",
			"challenger", record.Challenger, "amount", record.EscrowedChallengeFee.String(), "error", err)
		return fmt.Errorf("refund challenge fee: %w", err)
	}
	return nil
}

// disburseChallengeFeeOnRejected splits the escrowed fee: 50% to the
// verifier as SPARK reward, 50% burned from the module account. The
// odd-unit remainder (if any) goes to the verifier — same convention
// as x/forum's spam-tax split.
func (k Keeper) disburseChallengeFeeOnRejected(ctx context.Context, record types.VerificationRecord) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if record.EscrowedChallengeFee.IsNil() || !record.EscrowedChallengeFee.IsPositive() {
		return nil
	}
	var errs []error

	bondDenom := k.BondDenom(ctx)
	half := record.EscrowedChallengeFee.QuoRaw(2)
	verifierShare := record.EscrowedChallengeFee.Sub(half) // gets the odd unit
	burnShare := half

	// Pay verifier share.
	if verifierShare.IsPositive() && record.Verifier != "" {
		if verifierAddr, addrErr := sdk.AccAddressFromBech32(record.Verifier); addrErr == nil {
			coins := sdk.NewCoins(sdk.NewCoin(bondDenom, verifierShare))
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, verifierAddr, coins); err != nil {
				sdkCtx.Logger().Warn("disburse challenge fee: bank send to verifier failed",
					"verifier", record.Verifier, "amount", verifierShare.String(), "error", err)
				errs = append(errs, fmt.Errorf("pay verifier fee share: %w", err))
			}
		}
	}

	// Burn the rest from the module account.
	if burnShare.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(bondDenom, burnShare))
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
			sdkCtx.Logger().Warn("disburse challenge fee: burn failed",
				"amount", burnShare.String(), "error", err)
			errs = append(errs, fmt.Errorf("burn fee share: %w", err))
		}
	}
	return errors.Join(errs...)
}

// applyJuryVerdict applies a Phase 2 jury verdict to an open
// EscalatedChallenge. Called by MsgResolveEscalatedChallenge (OpsComm-
// submitted verdict) and by the EndBlocker jury-deadline sweep
// (TIMEOUT). Side effects mirror the Phase 1 auto-verdict path for
// UPHELD/REJECTED; TIMEOUT splits the challenge fee 50/50 (refund /
// burn) and applies no slash. Always disposes of the escalation fee
// (refund to escalator if jury overturned the auto-verdict; burned
// otherwise) and tears down the EscalatedChallenge entry, its deadline
// queue entry and any arbiter escalation-queue entry of the round.
//
// Settlement is single-shot: the escrowed challenge fee is zeroed once
// disbursed, and an escalation whose content is no longer CHALLENGED /
// DISPUTED (something else already settled it) is closed with the
// escalation fee refunded instead of being settled again.
//
// When the verdict shows the submitting bridge operator's content hash
// was false (CHALLENGED content with the challenge UPHELD — the verifier
// had matched the operator — or DISPUTED content with the challenge
// REJECTED — the verifier had disagreed with the operator and was right),
// the x/service operator report is filed here. Escalation itself never
// reports the operator.
//
// Side-effect failures are returned. The EndBlocker runs the TIMEOUT in a
// cache context and retries it with nothing committed (settlement_failure,
// FED-29); the OpsComm tx returns them, so the tx and every side effect
// roll back and the verdict can be resubmitted.
func (k Keeper) applyJuryVerdict(
	ctx context.Context,
	contentID uint64,
	verdict types.JuryVerdict,
	blockTime int64,
	params types.Params,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	esc, eerr := k.EscalatedChallenges.Get(ctx, contentID)
	if eerr != nil {
		return nil
	}
	record, rerr := k.VerificationRecords.Get(ctx, contentID)
	if rerr != nil {
		// Record gone (content pruned during the jury window). Drop the
		// escalation entry to keep the EndBlocker progressing; burn the
		// escalation fee since there's no escalator state to refund to
		// safely.
		err := k.disposeEscalationFee(ctx, esc, false)
		k.removeEscalatedChallenge(ctx, esc)
		return err
	}
	content, cerr := k.Content.Get(ctx, contentID)
	if cerr == nil && !isDisputeStatus(content.Status) {
		// Already settled by another path. Nothing is left to decide, so
		// the escalator gets the fee back rather than paying for a
		// verdict that can no longer apply.
		err := k.disposeEscalationFee(ctx, esc, true)
		k.removeEscalatedChallenge(ctx, esc)
		return err
	}

	// The verdict falsifies the operator's content hash when it goes
	// against the operator's side: on CHALLENGED content the verifier had
	// matched the operator, so UPHELD (verifier wrong) falsifies it; on
	// DISPUTED content the verifier had disagreed with the operator, so
	// REJECTED (verifier right) falsifies it. Unlike an arbiter quorum, a
	// committee verdict cannot say "both were wrong".
	disputed := cerr == nil && content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED
	falsified := (!disputed && verdict == types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD) ||
		(disputed && verdict == types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED)

	var settleErr error
	switch verdict {
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD:
		settleErr = k.applyAutoVerdictUpheld(ctx, contentID, &record, falsified, blockTime, params)
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED:
		settleErr = k.applyAutoVerdictRejected(ctx, contentID, &record, falsified, params)
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT:
		settleErr = k.applyJuryVerdictTimeout(ctx, contentID, &record)
	default:
		return nil
	}

	// File the operator report when the verdict falsifies the operator's
	// content hash.
	if cerr == nil {
		if falsified {
			evidenceURI := fmt.Sprintf("content_id=%d jury_verdict=%s", contentID, verdict)
			k.fileOrQueueChallengeReport(ctx, content, &record, evidenceURI)
		}
	}

	// The helpers may have mutated `record` (e.g. PriorRejectedChallenges
	// on REJECTED). We persist the mutated value directly — no reload
	// needed, and a reload would clobber the in-memory updates. The escrow
	// was disbursed above, so it is zeroed: no later path may pay it again.
	record.EscrowedChallengeFee = math.ZeroInt()
	record.LastChallengeResolvedAt = blockTime
	record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED
	clearSettlementRetry(&record)
	if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
		sdkCtx.Logger().Warn("jury verdict: persist record failed",
			"content_id", contentID, "error", err)
		settleErr = errors.Join(settleErr, err)
	}
	// A quorum stashed before the escalation left its escalation-queue
	// entry behind; the jury owns the round now, so drop it.
	if record.ArbiterEscalationDeadline != 0 {
		if err := k.ArbiterEscalationQueue.Remove(ctx, collections.Join(record.ArbiterEscalationDeadline, contentID)); err != nil {
			return err
		}
	}

	overturned := juryOverturnedAutoVerdict(esc.AutoVerdictBeforeEscalation, verdict)
	if err := k.disposeEscalationFee(ctx, esc, overturned); err != nil {
		settleErr = errors.Join(settleErr, err)
	}
	k.removeEscalatedChallenge(ctx, esc)
	return settleErr
}

// applyJuryVerdictTimeout handles CHALLENGE_TIMEOUT (Phase 2 jury did
// not reach a verdict): no slash, no counter penalty on the verifier,
// content reverts to VERIFIED (or HIDDEN when it was originally
// DISPUTED — the verifier-vs-operator disagreement is left unresolved).
// Escrowed challenge fee is split 50/50 (refund to challenger / burn).
//
// Takes the record by pointer (parity with the UPHELD/REJECTED helpers)
// because the verifier's commitment is released here — the jury reached
// no verdict against them, so the reserved slash budget returns to
// available — and the caller persists the mutated record.
func (k Keeper) applyJuryVerdictTimeout(
	ctx context.Context,
	contentID uint64,
	record *types.VerificationRecord,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var errs []error

	// No verdict means no finding against the verifier: give the
	// committed slash budget back (P0.1).
	if err := k.releaseVerifierCommitmentErr(ctx, record); err != nil {
		errs = append(errs, fmt.Errorf("release commitment: %w", err))
	}

	// Half-refund, half-burn the challenge fee.
	if err := k.splitChallengeFeeNoFinding(ctx, *record); err != nil {
		errs = append(errs, err)
	}

	// Content status: revert CHALLENGED → VERIFIED. DISPUTED stays
	// HIDDEN-equivalent because verifier-vs-operator disagreement
	// remains unresolved.
	if content, cerr := k.Content.Get(ctx, contentID); cerr == nil {
		switch content.Status {
		case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED:
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED:
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
		}
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			sdkCtx.Logger().Warn("jury timeout: persist content failed",
				"content_id", contentID, "error", err)
			errs = append(errs, err)
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeTimeout,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier),
		sdk.NewAttribute(types.AttributeKeyChallenger, record.Challenger),
	))
	return errors.Join(errs...)
}

// splitChallengeFeeNoFinding disposes of an escrowed challenge fee when the
// dispute ended without a finding (jury TIMEOUT, or an arbiter window that
// closed without quorum): half back to the challenger (the odd unit too),
// half burned. No-op when nothing was escrowed. The caller zeroes the
// record's escrow.
func (k Keeper) splitChallengeFeeNoFinding(ctx context.Context, record types.VerificationRecord) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if record.EscrowedChallengeFee.IsNil() || !record.EscrowedChallengeFee.IsPositive() {
		return nil
	}
	var errs []error
	bondDenom := k.BondDenom(ctx)
	half := record.EscrowedChallengeFee.QuoRaw(2)
	refund := record.EscrowedChallengeFee.Sub(half) // odd unit → challenger
	burn := half
	if refund.IsPositive() && record.Challenger != "" {
		if challengerAddr, addrErr := sdk.AccAddressFromBech32(record.Challenger); addrErr == nil {
			coins := sdk.NewCoins(sdk.NewCoin(bondDenom, refund))
			if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, challengerAddr, coins); err != nil {
				sdkCtx.Logger().Warn("no-finding: refund half-fee to challenger failed",
					"challenger", record.Challenger, "amount", refund.String(), "error", err)
				errs = append(errs, fmt.Errorf("refund half challenge fee: %w", err))
			}
		}
	} else if refund.IsPositive() {
		burn = record.EscrowedChallengeFee
	}
	if burn.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(bondDenom, burn))
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
			sdkCtx.Logger().Warn("no-finding: burn half-fee failed",
				"amount", burn.String(), "error", err)
			errs = append(errs, fmt.Errorf("burn half challenge fee: %w", err))
		}
	}
	return errors.Join(errs...)
}

// juryOverturnedAutoVerdict reports whether the jury verdict contradicts
// the auto-verdict snapshot. An UNSPECIFIED auto-verdict means there was
// nothing to overturn (escalation landed before quorum); a TIMEOUT jury
// verdict never counts as an overturn.
func juryOverturnedAutoVerdict(auto types.PendingVerifierVerdict, jury types.JuryVerdict) bool {
	if auto == types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED {
		return false
	}
	switch jury {
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD:
		// Jury says verifier was wrong; auto-verdict said verifier was
		// right ⇒ overturned.
		return auto == types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED:
		// Jury says verifier was right; auto-verdict said verifier was
		// wrong ⇒ overturned.
		return auto == types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG
	default:
		return false
	}
}

// disposeEscalationFee sends the escrowed escalation fee back to the
// escalator if the jury overturned the auto-verdict; otherwise burns
// it. No-op when the fee was zero.
func (k Keeper) disposeEscalationFee(
	ctx context.Context,
	esc types.EscalatedChallenge,
	overturned bool,
) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if esc.EscrowedEscalationFee.IsNil() || !esc.EscrowedEscalationFee.IsPositive() {
		return nil
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), esc.EscrowedEscalationFee))
	if overturned {
		if esc.Escalator == "" {
			// Defensive — burn rather than misroute.
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
				sdkCtx.Logger().Warn("dispose escalation fee: burn fallback failed",
					"amount", esc.EscrowedEscalationFee.String(), "error", err)
				return fmt.Errorf("burn escalation fee: %w", err)
			}
			return nil
		}
		escalatorAddr, err := sdk.AccAddressFromBech32(esc.Escalator)
		if err != nil {
			sdkCtx.Logger().Warn("dispose escalation fee: invalid escalator address",
				"escalator", esc.Escalator, "error", err)
			return fmt.Errorf("refund escalation fee: %w", err)
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, escalatorAddr, coins); err != nil {
			sdkCtx.Logger().Warn("dispose escalation fee: refund to escalator failed",
				"escalator", esc.Escalator, "amount", esc.EscrowedEscalationFee.String(), "error", err)
			return fmt.Errorf("refund escalation fee: %w", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeEscalationFeeRefunded,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", esc.ContentId)),
			sdk.NewAttribute("escalator", esc.Escalator),
			sdk.NewAttribute(types.AttributeKeyAmount, esc.EscrowedEscalationFee.String()),
		))
		return nil
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		sdkCtx.Logger().Warn("dispose escalation fee: burn failed",
			"amount", esc.EscrowedEscalationFee.String(), "error", err)
		return fmt.Errorf("burn escalation fee: %w", err)
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeEscalationFeeBurned,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", esc.ContentId)),
		sdk.NewAttribute(types.AttributeKeyAmount, esc.EscrowedEscalationFee.String()),
	))
	return nil
}

// removeEscalatedChallenge deletes the EscalatedChallenge entry and its
// deadline-queue entry. Idempotent — missing entries are skipped.
func (k Keeper) removeEscalatedChallenge(ctx context.Context, esc types.EscalatedChallenge) {
	if err := k.EscalatedChallenges.Remove(ctx, esc.ContentId); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removeEscalatedChallenge: EscalatedChallenges.Remove failed", "err", err)
	}
	if err := k.EscalatedChallengeDeadline.Remove(ctx, collections.Join(esc.JuryDeadline, esc.ContentId)); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("removeEscalatedChallenge: EscalatedChallengeDeadline.Remove failed", "err", err)
	}
}

// verifierConsecutiveUpheld reads the upheld streak back from x/rep for
// event emission. Zero when rep is unwired or the record is missing --
// events are informational and must never fail a verdict.
func (k Keeper) verifierConsecutiveUpheld(ctx context.Context, verifier string) uint64 {
	if k.late.repKeeper == nil {
		return 0
	}
	ra, err := k.late.repKeeper.GetRoleActivity(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifier)
	if err != nil {
		return 0
	}
	return ra.ConsecutiveUpheld
}

// verifierOverturnState reads back the post-verdict overturn streak, the
// lifetime slash count, and the cooldown x/rep just applied.
//
// slash_count is DERIVED from the overturned-verification count rather than
// stored: every upheld challenge slashes the verifier exactly once, so the two
// were always incremented on adjacent lines and a separate counter was pure
// duplication with a chance to drift.
func (k Keeper) verifierOverturnState(ctx context.Context, verifier string) (consecutiveOverturns, slashCount uint64, cooldownUntil int64) {
	if k.late.repKeeper == nil {
		return 0, 0, 0
	}
	ra, err := k.late.repKeeper.GetRoleActivity(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, verifier)
	if err != nil {
		return 0, 0, 0
	}
	return ra.ConsecutiveOverturns,
		ra.OverturnedActions[reptypes.ActionKindFederationVerify],
		ra.OverturnCooldownUntil
}

// slashAmountString returns the string form of a math.Int, normalizing
// nil to "0" so event payloads stay parseable.
func slashAmountString(amount math.Int) string {
	if amount.IsNil() {
		return "0"
	}
	return amount.String()
}
