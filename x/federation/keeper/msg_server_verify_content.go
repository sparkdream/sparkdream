package keeper

import (
	"bytes"
	"context"
	"fmt"

	"sparkdream/x/federation/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) VerifyContent(ctx context.Context, msg *types.MsgVerifyContent) (*types.MsgVerifyContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Verify creator is a bonded verifier with NORMAL or RECOVERY status
	verifier, err := k.Verifiers.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrVerifierNotFound, "verifier %s not found", msg.Creator)
	}
	if verifier.BondStatus != types.VerifierBondStatus_VERIFIER_BOND_STATUS_NORMAL &&
		verifier.BondStatus != types.VerifierBondStatus_VERIFIER_BOND_STATUS_RECOVERY {
		return nil, errorsmod.Wrapf(types.ErrVerifierNotActive, "verifier bond status is %s", verifier.BondStatus)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 2. Verify not in overturn cooldown
	if verifier.OverturnCooldownUntil > 0 && blockTime < verifier.OverturnCooldownUntil {
		return nil, errorsmod.Wrapf(types.ErrVerifierOverturnCooldown, "cooldown until %d", verifier.OverturnCooldownUntil)
	}

	// 3. Verify content exists and is PENDING_VERIFICATION (first-verifier-wins)
	content, err := k.Content.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", msg.ContentId)
	}
	if content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION {
		return nil, errorsmod.Wrapf(types.ErrContentNotPendingVerification, "content status is %s", content.Status)
	}

	// 4. Verify creator is NOT the bridge operator who submitted this content
	if content.SubmittedBy == msg.Creator {
		return nil, errorsmod.Wrap(types.ErrSelfVerification, "verifier cannot verify content submitted by their own bridge operator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 5. Verify within verification_window
	verificationDeadline := content.ReceivedAt + int64(params.VerificationWindow.Seconds())
	if blockTime > verificationDeadline {
		return nil, errorsmod.Wrapf(types.ErrVerificationWindowExpired, "window expired at %d", verificationDeadline)
	}

	// 6. Verify sufficient uncommitted bond
	availableBond := verifier.CurrentBond.Sub(verifier.TotalCommittedBond)
	if availableBond.LT(params.VerifierSlashAmount) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientVerifierBond, "available bond %s < slash amount %s", availableBond, params.VerifierSlashAmount)
	}

	// 7. Compare content_hash
	hashMatch := bytes.Equal(msg.ContentHash, content.ContentHash)

	// Create VerificationRecord
	record := types.VerificationRecord{
		ContentId:         msg.ContentId,
		Verifier:          msg.Creator,
		VerifierHash:      msg.ContentHash,
		VerifiedAt:        blockTime,
		ChallengeWindowEnds: blockTime + int64(params.ChallengeWindow.Seconds()),
		CommittedAmount:   params.VerifierSlashAmount,
		VerifierBondSnapshot: verifier.CurrentBond,
	}

	// Commit bond
	verifier.TotalCommittedBond = verifier.TotalCommittedBond.Add(params.VerifierSlashAmount)
	verifier.TotalVerifications++
	verifier.EpochVerifications++

	if hashMatch {
		// Match: content → VERIFIED
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_PENDING

		// Add to ChallengeWindowQueue
		if err := k.ChallengeWindow.Set(ctx, collections.Join(record.ChallengeWindowEnds, msg.ContentId)); err != nil {
			return nil, err
		}

		// Update operator's content_verified count
		bridgeKey := collections.Join(content.SubmittedBy, content.PeerId)
		bridge, err := k.BridgeOperators.Get(ctx, bridgeKey)
		if err == nil {
			bridge.ContentVerified++
			_ = k.BridgeOperators.Set(ctx, bridgeKey, bridge)
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeContentVerified,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute(types.AttributeKeyVerifier, msg.Creator),
				sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId)),
		)
	} else {
		// Mismatch: content → DISPUTED, initiate two-phase resolution
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED
		record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CHALLENGED

		// Add to ArbiterResolutionQueue for two-phase resolution
		arbiterDeadline := blockTime + int64(params.ArbiterResolutionWindow.Seconds())
		if err := k.ArbiterResolutionQueue.Set(ctx, collections.Join(arbiterDeadline, msg.ContentId)); err != nil {
			return nil, err
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeContentDisputed,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute(types.AttributeKeyVerifier, msg.Creator),
				sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId)),
		)
	}

	// Save all state
	if err := k.Content.Set(ctx, msg.ContentId, content); err != nil {
		return nil, err
	}
	if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
		return nil, err
	}
	if err := k.Verifiers.Set(ctx, msg.Creator, verifier); err != nil {
		return nil, err
	}

	return &types.MsgVerifyContentResponse{}, nil
}
