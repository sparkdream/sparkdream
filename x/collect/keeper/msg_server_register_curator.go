package keeper

import (
	"context"
	"strconv"

	"sparkdream/x/collect/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RegisterCurator(ctx context.Context, msg *types.MsgRegisterCurator) (*types.MsgRegisterCuratorResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Creator must be active x/rep member at or above min_curator_trust_level
	if !k.meetsMinTrustLevel(ctx, msg.Creator, params.MinCuratorTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrTrustLevelTooLow, "must be at or above %s", params.MinCuratorTrustLevel)
	}

	// bond_amount >= min_curator_bond
	if msg.BondAmount.LT(params.MinCuratorBond) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientBond, "bond %s < min %s", msg.BondAmount, params.MinCuratorBond)
	}

	// Not already registered as active curator
	existing, err := k.Curator.Get(ctx, msg.Creator)
	if err == nil && existing.Active {
		return nil, errorsmod.Wrap(types.ErrAlreadyCurator, msg.Creator)
	}

	// Lock DREAM bond via repKeeper.LockDREAM
	if err := k.repKeeper.LockDREAM(ctx, creatorAddr, msg.BondAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM bond")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Create Curator record
	curator := types.Curator{
		Address:           msg.Creator,
		BondAmount:        msg.BondAmount,
		RegisteredAt:      sdkCtx.BlockHeight(),
		TotalReviews:      0,
		ChallengedReviews: 0,
		Active:            true,
		PendingChallenges: 0,
	}
	if err := k.Curator.Set(ctx, msg.Creator, curator); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store curator")
	}

	// Emit curator_registered event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("curator_registered",
		sdk.NewAttribute("curator", msg.Creator),
		sdk.NewAttribute("bond_amount", msg.BondAmount.String()),
		sdk.NewAttribute("registered_at", strconv.FormatInt(sdkCtx.BlockHeight(), 10)),
	))

	return &types.MsgRegisterCuratorResponse{}, nil
}
