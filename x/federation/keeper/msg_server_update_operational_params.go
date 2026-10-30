package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	// 1. Verify authority: x/gov or the Operations Committee POLICY
	//    address. A single committee member could otherwise set
	//    content_ttl to minutes (censoring federation) or lift every
	//    state-growth limit with no proposal.
	if !k.isCouncilPolicy(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "must be governance or the Operations Committee policy address")
	}

	// 2. Merge into current params (only overwrite the operational subset)
	op := msg.OperationalParams
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
	currentParams.BridgeInactivityThreshold = op.BridgeInactivityThreshold
	currentParams.MaxPrunePerBlock = op.MaxPrunePerBlock

	// 3. Validate the merged params: the same range table MsgUpdateParams
	//    enforces (spec §4.13), so the operational path cannot reach values
	//    governance itself could not set.
	if err := currentParams.Validate(); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParamValue, err.Error())
	}

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
