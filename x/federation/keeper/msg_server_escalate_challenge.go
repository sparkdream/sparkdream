package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) EscalateChallenge(ctx context.Context, msg *types.MsgEscalateChallenge) (*types.MsgEscalateChallengeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Verify content is in CHALLENGED status with auto-resolution pending
	content, err := k.Content.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "content ID %d not found", msg.ContentId)
	}
	if content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_CHALLENGED &&
		content.Status != types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_DISPUTED {
		return nil, errorsmod.Wrapf(types.ErrNoAutoResolutionToEscalate, "content status is %s", content.Status)
	}

	// 2. Verify creator is the challenger or the verifier
	record, err := k.VerificationRecords.Get(ctx, msg.ContentId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrContentNotFound, "no verification record for content %d", msg.ContentId)
	}
	if msg.Creator != record.Verifier && msg.Creator != record.Challenger {
		return nil, errorsmod.Wrap(types.ErrNotChallengeParty, "signer must be challenger or verifier")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 3. Escrow escalation fee — denom resolved at runtime from x/identity.
	creatorAddr, _ := k.addressCodec.StringToBytes(msg.Creator)
	escalationFee := sdk.NewCoin(k.BondDenom(ctx), params.EscalationFeeAmount)
	feeCoins := sdk.NewCoins(escalationFee)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, feeCoins); err != nil {
		return nil, errorsmod.Wrapf(err, "failed to escrow escalation fee %s", escalationFee)
	}

	// 4. File a system report against the bridge operator that
	// submitted the disputed content. Phase 6 of the federation→
	// service migration: federation no longer maintains a parallel
	// jury creation path; it delegates to x/service.OpenSystemReport
	// and lets the standard tier-1 → jury flow handle resolution.
	// ReportTimeoutAction=ESCALATE on the bridge service types ensures
	// a silent controller can't park the slash forever.
	//
	// On standalone-mode (no service keeper wired), this is a no-op;
	// the escalation_fee escrow above still happens for completeness.
	evidenceURI := fmt.Sprintf("content_id=%d escalator=%s record_verifier=%s record_challenger=%s",
		msg.ContentId, msg.Creator, record.Verifier, record.Challenger)
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if rerr := k.Keeper.fileChallengeReport(ctx, content, evidenceURI); rerr != nil {
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeFederationHookFailure,
				sdk.NewAttribute("hook", "fileChallengeReport"),
				sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
				sdk.NewAttribute("error", rerr.Error()),
			),
		)
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeChallengeEscalated,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute(types.AttributeKeyUpdatedBy, msg.Creator)),
	)

	return &types.MsgEscalateChallengeResponse{}, nil
}
