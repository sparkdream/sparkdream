package keeper

import (
	"context"
	"fmt"

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
		if uint32(trustLevel) < params.MinVerifierTrustLevel {
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

	// 4. Verify challenger is not the original verifier or submitting operator
	if msg.Creator == record.Verifier {
		return nil, errorsmod.Wrap(types.ErrSelfChallenge, "challenger is the verifier")
	}
	if msg.Creator == content.SubmittedBy {
		return nil, errorsmod.Wrap(types.ErrSelfChallenge, "challenger is the submitting operator")
	}

	// 5. Anti-censorship: check cooldown and escalating fee
	effectiveFee := params.ChallengeFee
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
		effectiveFee.Amount = effectiveFee.Amount.MulRaw(int64(multiplier))
	}

	// 6. Escrow challenge fee
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
	record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CHALLENGED
	record.Challenger = msg.Creator
	if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
		return nil, err
	}

	// 9. Start Phase 1 (arbiter resolution)
	arbiterDeadline := blockTime + int64(params.ArbiterResolutionWindow.Seconds())
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
