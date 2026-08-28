package keeper

import (
	"context"
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
// No-op when verdict is UNSPECIFIED (escalation cleared it). All side
// effects are scoped so a downstream failure (e.g. bank refund) logs
// but does not roll back the upstream counter updates — the EndBlocker
// must keep making forward progress on the queue.
func (k Keeper) applyAutoVerdict(
	ctx context.Context,
	contentID uint64,
	blockTime int64,
	params types.Params,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	record, err := k.VerificationRecords.Get(ctx, contentID)
	if err != nil {
		return
	}
	switch record.PendingVerifierVerdict {
	case types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_RIGHT:
		k.applyAutoVerdictRejected(ctx, contentID, &record, params)
	case types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_VERIFIER_WRONG:
		k.applyAutoVerdictUpheld(ctx, contentID, &record, blockTime, params)
	default:
		// UNSPECIFIED: escalation cleared the verdict OR no quorum was
		// reached. Nothing to apply.
		return
	}

	// Mark resolution time + reset the verdict flag so a stale record
	// can't trigger re-application on a re-org / retry.
	record.LastChallengeResolvedAt = blockTime
	record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED
	if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
		sdkCtx.Logger().Warn("auto-verdict: persist record failed",
			"content_id", contentID, "error", err)
	}
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
	params types.Params,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

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
		}
	}
	consecutiveUpheld := k.verifierConsecutiveUpheld(ctx, record.Verifier)

	// Bump prior_rejected_challenges so the next challenger pays the
	// escalating fee (matches MsgChallengeVerification's 2^N multiplier).
	record.PriorRejectedChallenges++

	// Content stays VERIFIED — restore from CHALLENGED. Skip if content
	// is gone (peer removal could have pruned it during the escalation
	// window).
	if content, cerr := k.Content.Get(ctx, contentID); cerr == nil {
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			sdkCtx.Logger().Warn("auto-verdict (rejected): persist content failed",
				"content_id", contentID, "error", err)
		}
	}

	// Disburse the escrowed challenge fee: 50% to verifier as SPARK
	// reward, 50% burned. No-op if the fee was zero (defensive — should
	// not happen if MsgChallengeVerification ran first).
	k.disburseChallengeFeeOnRejected(ctx, *record)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeRejected,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier),
		sdk.NewAttribute(types.AttributeKeyChallenger, record.Challenger),
		sdk.NewAttribute("consecutive_upheld", fmt.Sprintf("%d", consecutiveUpheld)),
	))
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
	blockTime int64,
	params types.Params,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

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
		}
	}
	consecutiveOverturns, slashCount, cooldownUntil := k.verifierOverturnState(ctx, record.Verifier)

	// Slash DREAM bond. Half is later minted back to the challenger as
	// bounty (the SlashBond burn + MintDREAM half-back pattern conserves
	// the net "50% burned" outcome described in spec §7).
	slashAmount := params.VerifierSlashAmount
	if k.late.repKeeper != nil && !slashAmount.IsNil() && slashAmount.IsPositive() {
		reason := fmt.Sprintf("federation: challenge upheld (slash_count=%d)", slashCount)
		if err := k.late.repKeeper.SlashBond(ctx,
			reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
			record.Verifier,
			slashAmount,
			reason); err != nil {
			sdkCtx.Logger().Warn("verifier slash via rep keeper failed",
				"verifier", record.Verifier, "amount", slashAmount.String(), "error", err)
		} else {
			// Mint half-slash DREAM bounty to challenger. Skip when
			// challenger is empty (e.g. DISPUTED auto-resolution where
			// there was no challenger).
			bounty := slashAmount.QuoRaw(2)
			if bounty.IsPositive() && record.Challenger != "" {
				if challengerAddr, addrErr := sdk.AccAddressFromBech32(record.Challenger); addrErr == nil {
					if err := k.late.repKeeper.MintDREAM(ctx, challengerAddr, bounty); err != nil {
						sdkCtx.Logger().Warn("auto-verdict (upheld): mint challenger bounty failed",
							"challenger", record.Challenger, "amount", bounty.String(), "error", err)
					}
				}
			}
		}
	}

	// Content → REJECTED (operator's claim falsified by quorum), and the
	// submitting operator's rejection counters bump.
	//
	// content_rejected was declared and documented from the start but never
	// actually incremented — the counter sat at zero for every operator on
	// chain. It is now load-bearing: epoch_rejected is the eligibility gate on
	// operator compensation, so a falsified submission has to cost the
	// operator the epoch's pay.
	if content, cerr := k.Content.Get(ctx, contentID); cerr == nil {
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED
		if err := k.Content.Set(ctx, contentID, content); err != nil {
			sdkCtx.Logger().Warn("auto-verdict (upheld): persist content failed",
				"content_id", contentID, "error", err)
		}
		bridgeKey := collections.Join(content.SubmittedBy, content.PeerId)
		if binding, berr := k.BridgeBindings.Get(ctx, bridgeKey); berr == nil {
			binding.ContentRejected++
			binding.EpochRejected++
			if err := k.BridgeBindings.Set(ctx, bridgeKey, binding); err != nil {
				sdkCtx.Logger().Warn("auto-verdict (upheld): persist binding failed",
					"operator", content.SubmittedBy, "peer", content.PeerId, "error", err)
			}
		}
	}

	// Refund 100% of escrowed challenge fee to challenger.
	k.refundChallengeFee(ctx, *record)

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
}

// refundChallengeFee sends the full escrowed challenge fee back to the
// challenger. No-op when the fee was zero or the challenger address is
// invalid. The fee was escrowed into the federation module account at
// MsgChallengeVerification time.
func (k Keeper) refundChallengeFee(ctx context.Context, record types.VerificationRecord) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if record.EscrowedChallengeFee.IsNil() || !record.EscrowedChallengeFee.IsPositive() {
		return
	}
	if record.Challenger == "" {
		return
	}
	challengerAddr, err := sdk.AccAddressFromBech32(record.Challenger)
	if err != nil {
		sdkCtx.Logger().Warn("refund challenge fee: invalid challenger address",
			"challenger", record.Challenger, "error", err)
		return
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), record.EscrowedChallengeFee))
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, challengerAddr, coins); err != nil {
		sdkCtx.Logger().Warn("refund challenge fee: bank send failed",
			"challenger", record.Challenger, "amount", record.EscrowedChallengeFee.String(), "error", err)
	}
}

// disburseChallengeFeeOnRejected splits the escrowed fee: 50% to the
// verifier as SPARK reward, 50% burned from the module account. The
// odd-unit remainder (if any) goes to the verifier — same convention
// as x/forum's spam-tax split.
func (k Keeper) disburseChallengeFeeOnRejected(ctx context.Context, record types.VerificationRecord) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if record.EscrowedChallengeFee.IsNil() || !record.EscrowedChallengeFee.IsPositive() {
		return
	}

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
			}
		}
	}

	// Burn the rest from the module account.
	if burnShare.IsPositive() {
		coins := sdk.NewCoins(sdk.NewCoin(bondDenom, burnShare))
		if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
			sdkCtx.Logger().Warn("disburse challenge fee: burn failed",
				"amount", burnShare.String(), "error", err)
		}
	}
}

// applyJuryVerdict applies a Phase 2 jury verdict to an open
// EscalatedChallenge. Called by MsgResolveEscalatedChallenge (OpsComm-
// submitted verdict) and by the EndBlocker jury-deadline sweep
// (TIMEOUT). Side effects mirror the Phase 1 auto-verdict path for
// UPHELD/REJECTED; TIMEOUT splits the challenge fee 50/50 (refund /
// burn) and applies no slash. Always disposes of the escalation fee
// (refund to escalator if jury overturned the auto-verdict; burned
// otherwise) and tears down both the EscalatedChallenge entry and
// its deadline queue entry.
//
// All side-effect failures are logged and swallowed — the EndBlocker
// must continue draining the queue, and an OpsComm tx must not roll
// back the verdict because of a downstream bank send failure.
func (k Keeper) applyJuryVerdict(
	ctx context.Context,
	contentID uint64,
	verdict types.JuryVerdict,
	blockTime int64,
	params types.Params,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	esc, eerr := k.EscalatedChallenges.Get(ctx, contentID)
	if eerr != nil {
		return
	}
	record, rerr := k.VerificationRecords.Get(ctx, contentID)
	if rerr != nil {
		// Record gone (content pruned during the jury window). Drop the
		// escalation entry to keep the EndBlocker progressing; burn the
		// escalation fee since there's no escalator state to refund to
		// safely.
		k.disposeEscalationFee(ctx, esc, false)
		k.removeEscalatedChallenge(ctx, esc)
		return
	}

	switch verdict {
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_UPHELD:
		k.applyAutoVerdictUpheld(ctx, contentID, &record, blockTime, params)
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_REJECTED:
		k.applyAutoVerdictRejected(ctx, contentID, &record, params)
	case types.JuryVerdict_JURY_VERDICT_CHALLENGE_TIMEOUT:
		k.applyJuryVerdictTimeout(ctx, contentID, record)
	default:
		return
	}

	// The helpers may have mutated `record` (e.g. PriorRejectedChallenges
	// on REJECTED). We persist the mutated value directly — no reload
	// needed, and a reload would clobber the in-memory updates.
	record.LastChallengeResolvedAt = blockTime
	record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED
	if err := k.VerificationRecords.Set(ctx, contentID, record); err != nil {
		sdkCtx.Logger().Warn("jury verdict: persist record failed",
			"content_id", contentID, "error", err)
	}

	overturned := juryOverturnedAutoVerdict(esc.AutoVerdictBeforeEscalation, verdict)
	k.disposeEscalationFee(ctx, esc, overturned)
	k.removeEscalatedChallenge(ctx, esc)
}

// applyJuryVerdictTimeout handles CHALLENGE_TIMEOUT (Phase 2 jury did
// not reach a verdict): no slash, no counter penalty on the verifier,
// content reverts to VERIFIED (or HIDDEN when it was originally
// DISPUTED — the verifier-vs-operator disagreement is left unresolved).
// Escrowed challenge fee is split 50/50 (refund to challenger / burn).
func (k Keeper) applyJuryVerdictTimeout(
	ctx context.Context,
	contentID uint64,
	record types.VerificationRecord,
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Half-refund, half-burn the challenge fee.
	if !record.EscrowedChallengeFee.IsNil() && record.EscrowedChallengeFee.IsPositive() {
		bondDenom := k.BondDenom(ctx)
		half := record.EscrowedChallengeFee.QuoRaw(2)
		refund := record.EscrowedChallengeFee.Sub(half) // odd unit → challenger
		burn := half
		if refund.IsPositive() && record.Challenger != "" {
			if challengerAddr, addrErr := sdk.AccAddressFromBech32(record.Challenger); addrErr == nil {
				coins := sdk.NewCoins(sdk.NewCoin(bondDenom, refund))
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, challengerAddr, coins); err != nil {
					sdkCtx.Logger().Warn("jury timeout: refund half-fee to challenger failed",
						"challenger", record.Challenger, "amount", refund.String(), "error", err)
				}
			}
		}
		if burn.IsPositive() {
			coins := sdk.NewCoins(sdk.NewCoin(bondDenom, burn))
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
				sdkCtx.Logger().Warn("jury timeout: burn half-fee failed",
					"amount", burn.String(), "error", err)
			}
		}
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
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeChallengeTimeout,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", contentID)),
		sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier),
		sdk.NewAttribute(types.AttributeKeyChallenger, record.Challenger),
	))
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
) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if esc.EscrowedEscalationFee.IsNil() || !esc.EscrowedEscalationFee.IsPositive() {
		return
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), esc.EscrowedEscalationFee))
	if overturned {
		if esc.Escalator == "" {
			// Defensive — burn rather than misroute.
			if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
				sdkCtx.Logger().Warn("dispose escalation fee: burn fallback failed",
					"amount", esc.EscrowedEscalationFee.String(), "error", err)
			}
			return
		}
		escalatorAddr, err := sdk.AccAddressFromBech32(esc.Escalator)
		if err != nil {
			sdkCtx.Logger().Warn("dispose escalation fee: invalid escalator address",
				"escalator", esc.Escalator, "error", err)
			return
		}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, escalatorAddr, coins); err != nil {
			sdkCtx.Logger().Warn("dispose escalation fee: refund to escalator failed",
				"escalator", esc.Escalator, "amount", esc.EscrowedEscalationFee.String(), "error", err)
			return
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeEscalationFeeRefunded,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", esc.ContentId)),
			sdk.NewAttribute("escalator", esc.Escalator),
			sdk.NewAttribute(types.AttributeKeyAmount, esc.EscrowedEscalationFee.String()),
		))
		return
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		sdkCtx.Logger().Warn("dispose escalation fee: burn failed",
			"amount", esc.EscrowedEscalationFee.String(), "error", err)
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeEscalationFeeBurned,
		sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", esc.ContentId)),
		sdk.NewAttribute(types.AttributeKeyAmount, esc.EscrowedEscalationFee.String()),
	))
}

// removeEscalatedChallenge deletes the EscalatedChallenge entry and its
// deadline-queue entry. Idempotent — missing entries are skipped.
func (k Keeper) removeEscalatedChallenge(ctx context.Context, esc types.EscalatedChallenge) {
	_ = k.EscalatedChallenges.Remove(ctx, esc.ContentId)
	_ = k.EscalatedChallengeDeadline.Remove(ctx, collections.Join(esc.JuryDeadline, esc.ContentId))
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
