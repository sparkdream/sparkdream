package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	// 1. Verify authority via Operations Committee
	if !k.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or Operations Committee")
	}

	// 2. Validate operational params against reasonable bounds
	op := msg.OperationalParams
	if op.MaxInboundPerBlock == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "max_inbound_per_block must be > 0")
	}
	if op.MaxOutboundPerBlock == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "max_outbound_per_block must be > 0")
	}
	if op.MaxContentBodySize == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "max_content_body_size must be > 0")
	}
	if op.MaxContentUriSize == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "max_content_uri_size must be > 0")
	}
	if op.MaxProtocolMetadataSize == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "max_protocol_metadata_size must be > 0")
	}
	if op.ContentTtl.Seconds() <= 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "content_ttl must be > 0")
	}
	if op.AttestationTtl.Seconds() <= 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "attestation_ttl must be > 0")
	}
	if op.TrustDiscountRate.IsNegative() || op.TrustDiscountRate.GT(math.LegacyOneDec()) {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "trust_discount_rate must be in [0, 1]")
	}
	if op.MaxPrunePerBlock == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, "max_prune_per_block must be > 0")
	}

	// 3. Merge into current params (only overwrite the operational subset)
	currentParams, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

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
