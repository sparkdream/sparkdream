package keeper

import (
	"bytes"
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// UpdateServiceTypeConfig creates or updates a ServiceTypeConfig
// registry entry. Authority = x/gov module address. See §3.2.
//
// Enforces:
//   - signer is the x/gov module address (ErrUnauthorizedGovAuthority);
//   - msg.Config.Validate per §3.2 (regex on service_type, bps ranges,
//     aggregate≥unilateral, positive durations, valid min_bond);
//   - on update: service_type field of the incoming config matches the
//     existing entry's key (service_type is immutable once created).
//
// Emits service.service_type_updated. The `changed_fields` attribute
// is set to "create" on initial create or "update" on subsequent calls
// — finer-grained diff tracking is out of scope for v1.
func (k msgServer) UpdateServiceTypeConfig(ctx context.Context, msg *types.MsgUpdateServiceTypeConfig) (*types.MsgUpdateServiceTypeConfigResponse, error) {
	signerBytes, err := k.addrBytes(msg.Authority)
	if err != nil {
		return nil, types.ErrUnauthorizedGovAuthority.Wrap("invalid authority address")
	}
	if !bytes.Equal(signerBytes, k.authority) {
		return nil, types.ErrUnauthorizedGovAuthority.Wrapf("expected %x, got %x", k.authority, signerBytes)
	}

	if err := msg.Config.Validate(); err != nil {
		return nil, err
	}

	// Service-type immutability on update: if an existing entry is keyed
	// at the same string, that's an update. We only allow it (no rename).
	// If the key doesn't exist yet, this is a create.
	changedFields := "create"
	if _, err := k.ServiceTypes.Get(ctx, msg.Config.ServiceType); err == nil {
		changedFields = "update"
	}

	if err := k.ServiceTypes.Set(ctx, msg.Config.ServiceType, msg.Config); err != nil {
		return nil, err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(
		types.NewServiceTypeUpdatedEvent(msg.Config.ServiceType, msg.Config.Enabled, changedFields),
	)

	return &types.MsgUpdateServiceTypeConfigResponse{}, nil
}
