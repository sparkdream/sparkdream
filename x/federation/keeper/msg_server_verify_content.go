package keeper

import (
	"bytes"
	"context"
	"fmt"

	"sparkdream/x/federation/types"
	reptypes "sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) VerifyContent(ctx context.Context, msg *types.MsgVerifyContent) (*types.MsgVerifyContentResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	if k.late.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrVerifierNotFound, "rep keeper not wired")
	}

	// 1. Verify creator is a bonded verifier (ROLE_TYPE_FEDERATION_VERIFIER)
	//    with NORMAL or RECOVERY status (DEMOTED cannot verify).
	role, err := k.late.repKeeper.GetBondedRole(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrVerifierNotFound, "verifier %s not bonded", msg.Creator)
	}
	if role.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL &&
		role.BondStatus != reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_RECOVERY {
		return nil, errorsmod.Wrapf(types.ErrVerifierNotActive, "verifier bond status is %s", role.BondStatus)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 2. Verify not in per-module overturn cooldown.
	activity, _ := k.VerifierActivity.Get(ctx, msg.Creator)
	if activity.Address == "" {
		activity.Address = msg.Creator
	}
	if activity.OverturnCooldownUntil > 0 && blockTime < activity.OverturnCooldownUntil {
		return nil, errorsmod.Wrapf(types.ErrVerifierOverturnCooldown, "cooldown until %d", activity.OverturnCooldownUntil)
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

	// 6. Reserve slash budget against the verifier's bonded role. Failure
	//    propagates as ErrInsufficientVerifierBond.
	if err := k.late.repKeeper.ReserveBond(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER,
		msg.Creator, params.VerifierSlashAmount); err != nil {
		return nil, errorsmod.Wrapf(types.ErrInsufficientVerifierBond, "reserve bond: %s", err)
	}

	// 7. Compare content_hash
	hashMatch := bytes.Equal(msg.ContentHash, content.ContentHash)

	// Create VerificationRecord — bond snapshot is the verifier's current_bond
	// at verify time (parsed from the math.Int-string on BondedRole).
	bondSnapshot, _ := math.NewIntFromString(role.CurrentBond)
	if bondSnapshot.IsNil() {
		bondSnapshot = math.ZeroInt()
	}
	record := types.VerificationRecord{
		ContentId:            msg.ContentId,
		Verifier:             msg.Creator,
		VerifierHash:         msg.ContentHash,
		VerifiedAt:           blockTime,
		ChallengeWindowEnds:  blockTime + int64(params.ChallengeWindow.Seconds()),
		CommittedAmount:      params.VerifierSlashAmount,
		VerifierBondSnapshot: bondSnapshot,
	}

	// Bump per-module verifier activity counters.
	activity.TotalVerifications++
	activity.EpochVerifications++

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

	// Stamp last_active_epoch on the generic bonded-role record.
	_ = k.late.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, msg.Creator)

	// Save all state
	if err := k.Content.Set(ctx, msg.ContentId, content); err != nil {
		return nil, err
	}
	if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
		return nil, err
	}
	if err := k.VerifierActivity.Set(ctx, msg.Creator, activity); err != nil {
		return nil, err
	}

	return &types.MsgVerifyContentResponse{}, nil
}
