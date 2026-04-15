package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	// 1. Verify authority via Operations Committee
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
	}

	// 2. (Validate operational params against ranges — TODO: implement validation)

	// 3. Merge into current params (only overwrite the operational subset)
	currentParams, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	op := msg.OperationalParams
	currentParams.MaxInboundPerBlock = op.MaxInboundPerBlock
	currentParams.MaxOutboundPerBlock = op.MaxOutboundPerBlock
	currentParams.MaxContentBodySize = op.MaxContentBodySize
	currentParams.MaxContentUriSize = op.MaxContentUriSize
	currentParams.MaxProtocolMetadataSize = op.MaxProtocolMetadataSize
	currentParams.ContentTtl = op.ContentTtl
	currentParams.AttestationTtl = op.AttestationTtl
	currentParams.GlobalMaxTrustCredit = op.GlobalMaxTrustCredit
	currentParams.TrustDiscountRate = op.TrustDiscountRate
	currentParams.BridgeInactivityThreshold = op.BridgeInactivityThreshold
	currentParams.MaxPrunePerBlock = op.MaxPrunePerBlock

	if err := k.Params.Set(ctx, currentParams); err != nil {
		return nil, err
	}

	// 4. Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeOperationalParamsUpdated,
			sdk.NewAttribute(types.AttributeKeyUpdatedBy, msg.Authority)),
	)

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}
