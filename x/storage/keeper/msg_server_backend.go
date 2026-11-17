package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

// UpdateBackend creates a backend when the name is new, and otherwise
// replaces its mutable fields. Governance only.
func (k msgServer) UpdateBackend(ctx context.Context, msg *types.MsgUpdateBackend) (*types.MsgUpdateBackendResponse, error) {
	if !k.isGov(msg.Authority) {
		return nil, types.ErrInvalidSigner.Wrapf("invalid authority %s", msg.Authority)
	}
	if err := k.wired(); err != nil {
		return nil, err
	}
	p, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	b := msg.Backend
	if err := b.Validate(); err != nil {
		return nil, types.ErrInvalidBackend.Wrap(err.Error())
	}
	prev, err := k.Backends.Get(ctx, b.Name)
	switch {
	case err == nil:
		if prev.ServiceType != b.ServiceType || prev.Kind != b.Kind || prev.LocatorFormat != b.LocatorFormat {
			return nil, types.ErrBackendFieldImmutable
		}
	case !errors.Is(err, collections.ErrNotFound):
		return nil, err
	}
	if b.DeclinesToClose > p.MaxBarredPerRequest {
		return nil, types.ErrInvalidBackend.Wrap("declines_to_close is above max_barred_per_request")
	}
	if err := k.checkUnbondingCovers(ctx, p, b.ServiceType); err != nil {
		return nil, err
	}
	if err := k.Backends.Set(ctx, b.Name, b); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.backend_updated",
		sdk.NewAttribute("backend", b.Name),
		sdk.NewAttribute("enabled", boolStr(b.Enabled)),
	))
	return &types.MsgUpdateBackendResponse{}, nil
}

// UpdateOperationalParams sets one backend's prices inside its governance
// bounds. Commons Operations Committee or governance.
func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	if err := k.wired(); err != nil {
		return nil, err
	}
	if !k.isGov(msg.Authority) && !k.late.commons.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, types.ErrInvalidSigner.Wrapf("address %s is not authorized to update operational params", msg.Authority)
	}
	b, err := k.Backends.Get(ctx, msg.Backend)
	if err != nil {
		return nil, types.ErrBackendNotFound.Wrap(msg.Backend)
	}
	if err := b.ValidatePrices(msg.PricePerByte, msg.MinFee); err != nil {
		return nil, err
	}
	// Open requests keep the fee fixed at their creation.
	b.PricePerByte = msg.PricePerByte
	b.MinFee = msg.MinFee
	if err := k.Backends.Set(ctx, b.Name, b); err != nil {
		return nil, err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.operational_params.updated",
		sdk.NewAttribute("authority", msg.Authority),
		sdk.NewAttribute("backend", b.Name),
		sdk.NewAttribute("price_per_byte", b.PricePerByte.String()),
		sdk.NewAttribute("min_fee", b.MinFee.String()),
	))
	return &types.MsgUpdateOperationalParamsResponse{}, nil
}
