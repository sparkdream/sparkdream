package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// RegisterOperator creates a new live operator record and escrows the
// bond. See x-service-spec.md §5.1 for validation and §3.1 for the
// initial-field-population rule. MUST be signed by `creator` directly
// (the msg-server harness ensures this via signer-vs-creator check).
//
// Cross-module dependencies (CommonsKeeper.IsGroupPolicyAddress) are
// nil-checked: when the keeper is wired in standalone mode (early
// development / unit tests without app.go), the IsGroupAddress check
// is skipped with a logged warning rather than failing.
func (k msgServer) RegisterOperator(ctx context.Context, msg *types.MsgRegisterOperator) (*types.MsgRegisterOperatorResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Address shape & self-controller check (§3.3).
	creatorBytes, err := k.addrBytes(msg.Creator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid creator address")
	}
	if _, err := k.addrBytes(msg.Controller); err != nil {
		return nil, types.ErrControllerNotGroup.Wrap("invalid controller address")
	}
	if msg.Creator == msg.Controller {
		return nil, types.ErrSelfController
	}

	// Controller MUST be a registered x/commons Group policy address (§3.3).
	// Nil-check: when running in standalone mode (no commons keeper wired),
	// skip this check — production wiring sets it via SetCrossModuleKeepers.
	if k.commonsKeeper() != nil {
		if !k.commonsKeeper().IsGroupPolicyAddress(ctx, msg.Controller) {
			return nil, types.ErrControllerNotGroup
		}
	}

	// Service type must exist and be enabled.
	cfg, err := k.resolveServiceTypeConfig(ctx, msg.ServiceType)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, types.ErrServiceTypeDisabled.Wrap(msg.ServiceType)
	}

	// Bond amount: bond denom comes from x/identity at runtime; user only
	// supplies the amount.
	bondDenom := k.BondDenom(ctx)
	if msg.BondAmount.IsNil() || msg.BondAmount.IsNegative() {
		return nil, types.ErrInsufficientBond.Wrapf("bond_amount must be a positive Int")
	}
	if msg.BondAmount.LT(cfg.MinBondAmount) {
		return nil, types.ErrInsufficientBond.Wrapf("bond %s%s < min %s%s", msg.BondAmount, bondDenom, cfg.MinBondAmount, bondDenom)
	}
	bondCoin := sdk.NewCoin(bondDenom, msg.BondAmount)

	// Metadata size cap.
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if uint32(len(msg.Metadata)) > params.MaxMetadataBytes {
		return nil, types.ErrInvalidMetadataSize.Wrapf("%d > %d", len(msg.Metadata), params.MaxMetadataBytes)
	}

	// No live record for (creator, service_type).
	if _, exists := k.GetOperator(ctx, creatorBytes, msg.ServiceType); exists {
		return nil, types.ErrOperatorAlreadyExists.Wrapf("%s / %s", msg.Creator, msg.ServiceType)
	}

	// No prior SLASHED record for (creator, service_type).
	hasSlashed, err := k.HasSlashedRecord(ctx, creatorBytes, msg.ServiceType)
	if err != nil {
		return nil, err
	}
	if hasSlashed {
		return nil, types.ErrOperatorPreviouslySlashed.Wrapf("%s / %s", msg.Creator, msg.ServiceType)
	}

	// max_active_operators_per_address cap (counts live records across
	// service types; archived don't count — see §3.1 / §4.2 notes).
	liveCount, err := k.CountLiveOperatorsForAddress(ctx, creatorBytes)
	if err != nil {
		return nil, err
	}
	if liveCount >= params.MaxActiveOperatorsPerAddress {
		return nil, types.ErrTooManyOperatorsForAddress.Wrapf("address %s has %d live records (cap %d)", msg.Creator, liveCount, params.MaxActiveOperatorsPerAddress)
	}

	// Escrow bond to module's bond pool.
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, sdk.AccAddress(creatorBytes), types.ModuleName, sdk.NewCoins(bondCoin)); err != nil {
		return nil, err
	}

	// Build the new operator record per §3.1 / §5.1 field-init rules.
	currentHeight := sdkCtx.BlockHeight()
	op := types.Operator{
		Address:                 msg.Creator,
		ServiceType:             msg.ServiceType,
		Controller:              msg.Controller,
		BondAmount:              msg.BondAmount,
		Metadata:                msg.Metadata,
		Status:                  types.OperatorStatus_OPERATOR_STATUS_ACTIVE,
		UnderfundedSince:        0,
		UnbondCompleteAt:        0,
		Tier1SlashedInWindow:    sdkmath.ZeroInt(),
		Tier1WindowStart:        currentHeight,
		Tier1WindowStartBond:    msg.BondAmount,
		RegisteredAt:            currentHeight,
		RetiredAt:               0,
		TotalLifetimeBondBlocks: sdkmath.ZeroInt(),
		LastBondBlockUpdateAt:   currentHeight,
	}
	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(types.NewOperatorRegisteredEvent(
		msg.Creator, msg.ServiceType, msg.Controller, bondCoin,
	))
	emitOperatorRegistered(msg.ServiceType)

	return &types.MsgRegisterOperatorResponse{}, nil
}
