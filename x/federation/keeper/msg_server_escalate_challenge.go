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
	if msg.Creator != record.Verifier {
		// TODO: also check against challenger address (need to store challenger on record or content)
		// For now, allow verifier only
		return nil, errorsmod.Wrap(types.ErrNotChallengeParty, "signer must be challenger or verifier")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 3. Escrow escalation fee
	creatorAddr, _ := k.addressCodec.StringToBytes(msg.Creator)
	feeCoins := sdk.NewCoins(params.EscalationFee)
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, feeCoins); err != nil {
		return nil, errorsmod.Wrapf(err, "failed to escrow escalation fee %s", params.EscalationFee)
	}

	// 4. Create x/rep jury initiative (TODO: implement full jury creation)
	// For now, emit the escalation event

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeChallengeEscalated,
			sdk.NewAttribute(types.AttributeKeyContentID, fmt.Sprintf("%d", msg.ContentId)),
			sdk.NewAttribute(types.AttributeKeyUpdatedBy, msg.Creator)),
	)

	return &types.MsgEscalateChallengeResponse{}, nil
}
