package keeper

import (
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

	// 3. Validate new status (only HIDDEN, ACTIVE/VERIFIED, REJECTED allowed for moderation)
	switch msg.NewStatus {
	case types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_HIDDEN,
		types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_ACTIVE,
		types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_VERIFIED,
		types.FederatedContentStatus_FEDERATED_CONTENT_STATUS_REJECTED:
		// valid moderation targets
	default:
		return nil, errorsmod.Wrapf(types.ErrInvalidParamValue, "invalid moderation status %s", msg.NewStatus)
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
