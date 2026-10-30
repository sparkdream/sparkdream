package keeper

import (
	"bytes"
	"context"
	"fmt"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ModerateContent(ctx context.Context, msg *types.MsgModerateContent) (*types.MsgModerateContentResponse, error) {
	// 1. Verify authority is Operations Committee
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
	}

	// 2. Verify content exists
	content, err := k.Content.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", msg.ContentId)
	}

	// 3a. UNRESOLVED is system-assigned and terminal: the arbiter
	//     resolution window closed with no quorum, so the chain recorded
	//     that no finding was ever made. Moderating it away would erase
	//     that record and let an OpsComm member manufacture a VERIFIED
	//     status for content nobody verified. Validating only new_status
	//     (below) guards the target but not the source.
	if content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_UNRESOLVED {
		return nil, errorsmod.Wrapf(types.ErrContentTerminal,
			"content %d is UNRESOLVED (the arbiter resolution window closed with no quorum)",
			msg.ContentId)
	}
	// SUPERSEDED is terminal for the same reason: the record was retired
	// before anyone could verify it, and moderating it to VERIFIED or ACTIVE
	// would vouch for bytes that no longer exist at the source.
	if content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_SUPERSEDED {
		return nil, errorsmod.Wrapf(types.ErrContentTerminal,
			"content %d is SUPERSEDED by content %d", msg.ContentId, content.SupersededBy)
	}

	// 3. Validate new status. Moderation hides, rejects or restores
	//    (spec §6.18); it never attests. VERIFIED is not a target: a record
	//    moved there would carry no VerificationRecord, no committed
	//    verifier bond and no challenge window, so it could never be
	//    challenged. Bridged content is restored to the status its own
	//    verification history supports (restoreStatus); ACTIVE is the
	//    restore target only for content received over IBC.
	switch msg.NewStatus {
	case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN,
		types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED:
	case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE:
		restored, err := k.restoreStatus(ctx, content)
		if err != nil {
			return nil, err
		}
		msg.NewStatus = restored
	default:
		return nil, errorsmod.Wrapf(types.ErrInvalidParamValue, "invalid moderation status %s (hide, reject or restore to active)", msg.NewStatus)
	}

	// 3b. Never during an open dispute: the arbiter, auto-verdict and jury
	//     paths own the status until they settle, and a moderated status
	//     would strand the escrowed fees (their settlement keys off
	//     CHALLENGED / DISPUTED) and be overwritten by the verdict anyway.
	if k.hasOpenDispute(ctx, content) {
		return nil, errorsmod.Wrapf(types.ErrDisputeInProgress, "content %d", msg.ContentId)
	}

	// 3c. Hiding is the fast brake any single Operations Committee member
	//     may pull (like MsgSuspendPeer). Rejecting or restoring changes
	//     what the record attests and needs the committee POLICY.
	if msg.NewStatus != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN &&
		!k.isCouncilPolicy(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "reject / restore needs x/gov or the Operations Committee policy address")
	}

	// 4. Update status
	content.Status = msg.NewStatus
	if err := k.Content.Set(ctx, msg.ContentId, content); err != nil {
		return nil, err
	}

	// 5. Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeFederatedContentModerated,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute(types.AttributeKeyNewStatus, msg.NewStatus.String()),
			sdk.NewAttribute(types.AttributeKeyReason, msg.Reason),
			sdk.NewAttribute(types.AttributeKeyUpdatedBy, msg.Authority)),
	)

	return &types.MsgModerateContentResponse{}, nil
}

// restoreStatus is the status a moderated record goes back to. IBC content
// (no submitting operator) is ACTIVE. Bridged content returns to VERIFIED
// only if a verification of it stands (a VerificationRecord that was not
// overturned), and to PENDING_VERIFICATION while its verification window is
// still open; anything else cannot be restored.
func (k Keeper) restoreStatus(ctx context.Context, content types.FederatedContent) (types.FederatedContentStatus, error) {
	if content.SubmittedBy == "" {
		return types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE, nil
	}
	if record, err := k.VerificationRecords.Get(ctx, content.Id); err == nil {
		if record.Outcome == types.VerificationOutcome_VERIFICATION_OUTCOME_UPHELD ||
			record.Outcome == types.VerificationOutcome_VERIFICATION_OUTCOME_OVERTURNED {
			return 0, errorsmod.Wrapf(types.ErrContentTerminal, "content %d had a challenge against its verification upheld", content.Id)
		}
		if !bytes.Equal(record.VerifierHash, content.ContentHash) {
			return 0, errorsmod.Wrapf(types.ErrContentTerminal, "content %d was never verified (its verifier disagreed)", content.Id)
		}
		return types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED, nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	if now <= content.ReceivedAt+int64(params.VerificationWindow.Seconds()) {
		return types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION, nil
	}
	return 0, errorsmod.Wrapf(types.ErrContentTerminal, "content %d was never verified and its verification window has closed", content.Id)
}

// hasOpenDispute reports whether content is mid-dispute: CHALLENGED or
// DISPUTED, a stashed auto-verdict waiting out its escalation window, or a
// jury escalation open.
func (k Keeper) hasOpenDispute(ctx context.Context, content types.FederatedContent) bool {
	if content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED ||
		content.Status == types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED {
		return true
	}
	if record, err := k.VerificationRecords.Get(ctx, content.Id); err == nil &&
		record.PendingVerifierVerdict != types.PendingVerifierVerdict_PENDING_VERIFIER_VERDICT_UNSPECIFIED {
		return true
	}
	has, getErr := k.EscalatedChallenges.Has(ctx, content.Id)
	if getErr != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("Has failed", "err", getErr)
	}
	return has
}
