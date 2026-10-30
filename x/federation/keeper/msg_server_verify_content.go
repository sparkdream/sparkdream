package keeper

import (
	"bytes"
	"context"
	"errors"
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

	// 1. Verify creator is an eligible bonded verifier
	//    (ROLE_TYPE_FEDERATION_VERIFIER) through rep's shared
	//    EligibleForRole: NORMAL and RECOVERY verify outright; UNBONDING
	//    verifies only while the staying bond (current - pending) still
	//    covers the role minimum, so a partial unbond keeps the verifier
	//    working and a full exit does not; DEMOTED cannot verify. Same gate
	//    forum sentinels and collect curators use.
	role, err := k.late.repKeeper.EligibleForRole(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, msg.Creator)
	if err != nil {
		if errors.Is(err, reptypes.ErrBondedRoleNotFound) {
			return nil, errorsmod.Wrapf(types.ErrVerifierNotFound, "verifier %s not bonded", msg.Creator)
		}
		return nil, errorsmod.Wrapf(types.ErrVerifierNotActive, "verifier bond status is %s: %v", role.BondStatus, err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 2. Verify not in the shared overturn cooldown. Owned by x/rep on the
	//    role's RoleActivity record, so a lockout applies to the role rather
	//    than to this one surface.
	cooldownUntil := k.late.repKeeper.RoleOverturnCooldownUntil(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, msg.Creator)
	if cooldownUntil > 0 && blockTime < cooldownUntil {
		return nil, errorsmod.Wrapf(types.ErrVerifierOverturnCooldown, "cooldown until %d", cooldownUntil)
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
	//    Alias-aware (SameMember), so a migrated key does not turn the
	//    submitting operator into an independent verifier. One person
	//    running a bridge from an unrelated fresh address and verifying
	//    from their member address is NOT caught here (spec §14.6).
	if k.sameMember(ctx, content.SubmittedBy, msg.Creator) {
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
		EscrowedChallengeFee: math.ZeroInt(),
	}

	if hashMatch {
		// Match: content → VERIFIED. A peer under require_review lands
		// verified content HIDDEN instead: the verification stands (the
		// record, the commitment and the challenge window are the same),
		// and an Operations Committee restore (MsgModerateContent to
		// ACTIVE) publishes it as VERIFIED (restoreStatus).
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED
		awaitingReview := false
		if policy, perr := k.PeerPolicies.Get(ctx, content.PeerId); perr == nil && policy.RequireReview {
			content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN
			awaitingReview = true
		}
		record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_PENDING

		// Add to ChallengeWindowQueue
		if err := k.ChallengeWindow.Set(ctx, collections.Join(record.ChallengeWindowEnds, msg.ContentId)); err != nil {
			return nil, err
		}

		// Update operator's content_verified count
		bridgeKey := collections.Join(content.SubmittedBy, content.PeerId)
		bridge, err := k.BridgeBindings.Get(ctx, bridgeKey)
		if err == nil {
			bridge.ContentVerified++
			bridge.EpochVerified++
			if err := k.BridgeBindings.Set(ctx, bridgeKey, bridge); err != nil {
				return nil, err
			}
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeContentVerified,
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute(types.AttributeKeyVerifier, msg.Creator),
				sdk.NewAttribute(types.AttributeKeyPeerID, content.PeerId)),
		)
		if awaitingReview {
			emitContentAwaitingReview(ctx, msg.ContentId, content.PeerId)
		}
	} else {
		// Mismatch: content → DISPUTED, initiate two-phase resolution
		content.Status = types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED
		record.Outcome = types.VerificationOutcome_VERIFICATION_OUTCOME_CHALLENGED

		// Add to ArbiterResolutionQueue for two-phase resolution
		arbiterDeadline := blockTime + int64(params.ArbiterResolutionWindow.Seconds())
		record.ArbiterResolutionDeadline = arbiterDeadline
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

	// Report the verification to x/rep: it owns the per-kind counters the
	// reward distribution reads, and last_active_epoch on the bonded role.
	if err := k.late.repKeeper.RecordRoleAction(ctx,
		reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, msg.Creator,
		reptypes.ActionKindFederationVerify); err != nil {
		return nil, err
	}
	if err := k.late.repKeeper.RecordActivity(ctx, reptypes.RoleType_ROLE_TYPE_FEDERATION_VERIFIER, msg.Creator); err != nil {
		return nil, err
	}

	// Save all state
	if err := k.Content.Set(ctx, msg.ContentId, content); err != nil {
		return nil, err
	}
	if err := k.VerificationRecords.Set(ctx, msg.ContentId, record); err != nil {
		return nil, err
	}

	return &types.MsgVerifyContentResponse{}, nil
}
