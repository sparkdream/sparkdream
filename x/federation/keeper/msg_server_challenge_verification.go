package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ChallengeVerification(ctx context.Context, msg *types.MsgChallengeVerification) (*types.MsgChallengeVerificationResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	_ = creatorBytes

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 1. Verify creator meets min_verifier_trust_level
	if k.late.repKeeper != nil {
		trustLevel, err := k.late.repKeeper.GetTrustLevel(ctx, sdk.AccAddress(creatorBytes))
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrTrustLevelInsufficient, "failed to get trust level")
		}
		if commontypes.Saturate[uint32](trustLevel) < params.MinVerifierTrustLevel {
			return nil, errorsmod.Wrapf(types.ErrTrustLevelInsufficient, "trust level %d < required %d", trustLevel, params.MinVerifierTrustLevel)
		}
	}

	// 2. Verify content exists and is VERIFIED
	content, err := k.Content.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", msg.ContentId)
	}
	if content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED {
		return nil, errorsmod.Wrapf(types.ErrContentNotVerified, "content status is %s", content.Status)
	}

	// 3. Verify within challenge_window
	record, err := k.VerificationRecords.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "no verification record for content %d", msg.ContentId)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	if blockTime > record.ChallengeWindowEnds {
		return nil, errorsmod.Wrapf(types.ErrChallengeWindowExpired, "window expired at %d", record.ChallengeWindowEnds)
	}

	// 3b. A new dispute round starts clean. An escalation still open, or
	//     arbiter data of the previous round not yet deleted (its cleanup
	//     runs against the EndBlocker prune budget), would otherwise leak
	//     into this round: old votes would count, and the old round's
	//     cleanup would delete the new round's votes.
	if has, _ := k.EscalatedChallenges.Has(ctx, msg.ContentId); has {
		return nil, errorsmod.Wrapf(types.ErrDisputeInProgress, "content %d still has an open escalation", msg.ContentId)
	}
	if k.hasArbiterData(ctx, msg.ContentId) {
		return nil, errorsmod.Wrapf(types.ErrDisputeInProgress, "the previous arbitration round of content %d is still being cleaned up", msg.ContentId)
	}

	// 4. Verify challenger is not the original verifier or submitting operator
	//    Alias-aware (SameMember): a member who migrated keys is still the
	//    verifier / operator of records written under the old address.
	if k.sameMember(ctx, msg.Creator, record.Verifier) {
		return nil, errorsmod.Wrap(types.ErrSelfChallenge, "challenger is the verifier")
	}
	if k.sameMember(ctx, msg.Creator, content.SubmittedBy) {
		return nil, errorsmod.Wrap(types.ErrSelfChallenge, "challenger is the submitting operator")
	}

	// 5. Anti-censorship: check cooldown and escalating fee
	effectiveFeeAmount := params.ChallengeFeeAmount
	if record.PriorRejectedChallenges > 0 {
		// Check cooldown
		if record.LastChallengeResolvedAt > 0 {
			cooldownEnd := record.LastChallengeResolvedAt + int64(params.ChallengeCooldown.Seconds())
			if blockTime < cooldownEnd {
				return nil, errorsmod.Wrapf(types.ErrChallengeCooldownActive, "cooldown until %d", cooldownEnd)
			}
		}
		// Escalating fee: challenge_fee * 2^(prior_rejected_challenges)
		// Cap the shift at 20 to prevent bit-shift overflow (2^20 = 1M multiplier is already extreme)
		shifts := record.PriorRejectedChallenges
		if shifts > 20 {
			shifts = 20
		}
		multiplier := uint64(1) << shifts
		effectiveFeeAmount = effectiveFeeAmount.MulRaw(commontypes.Saturate[int64](multiplier))
	}

	// 6. Escrow challenge fee — denom resolved at runtime from x/identity.
	effectiveFee := sdk.NewCoin(k.BondDenom(ctx), effectiveFeeAmount)
	challengerAddr, _ := k.addressCodec.StringToBytes(msg.Creator)
	feeCoins := sdk.NewCoins(effectiveFee)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, challengerAddr, types.ModuleName, feeCoins); err != nil {
		return nil, errorsmod.Wrapf(err, "failed to escrow challenge fee %s", effectiveFee)
	}

	// 7. Content status → CHALLENGED
	content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED
	if err := k.Content.Set(ctx, msg.ContentId, content); err != nil {
		return nil, err
	}

	// 8. VerificationRecord outcome → CHALLENGED, store challenger address
	// and the actual escrowed fee. Snapshot is needed because the auto-
	// resolve path can't recompute the escalating fee multiplier reliably
	// (PriorRejectedChallenges may move forward before resolution lands).
	// The round's arbiter deadlines are reset: votes are accepted until
	// arbiter_resolution_deadline, and no quorum has been reached yet.
	arbiterDeadline := blockTime + int64(params.ArbiterResolutionWindow.Seconds())
	record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CHALLENGED
	record.Challenger = msg.Creator
	record.EscrowedChallengeFee = effectiveFeeAmount
	record.PendingVerifierVerdict = types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED
	record.ArbiterResolutionDeadline = arbiterDeadline
	record.ArbiterEscalationDeadline = 0
	record.QuorumFalsifiedContent = false
	if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
		return nil, err
	}

	// 9. Start Phase 1 (arbiter resolution)
	if err := k.ArbiterResolutionQueue.Set(ctx, collections.Join(arbiterDeadline, msg.ContentId)); err != nil {
		return nil, err
	}

	// 10. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeVerificationChallenged,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute(types.AttributeKeyChallenger, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyVerifier, record.Verifier)),
	)

	return &types.MsgChallengeVerificationResponse{}, nil
}
