package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"sparkdream/x/commons/types"
)

// UndelegateTreasury starts unbonding treasury stake (treasury_staking.go).
// It has no activation or term gate: a group may always bring its stake
// home.
func (k msgServer) UndelegateTreasury(goCtx context.Context, msg *types.MsgUndelegateTreasury) (*types.MsgUndelegateTreasuryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	name, _, err := k.treasuryGroup(ctx, msg.Authority)
	if err != nil {
		return nil, err
	}
	if err := k.validateStakeAmount(ctx, msg.Amount); err != nil {
		return nil, err
	}

	res, err := k.routeAs(ctx, &stakingtypes.MsgUndelegate{
		DelegatorAddress: msg.Authority,
		ValidatorAddress: msg.ValidatorAddress,
		Amount:           msg.Amount,
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "undelegation failed")
	}
	var out stakingtypes.MsgUndelegateResponse
	if err := k.routedResponse(res, &out); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"treasury_undelegated",
		sdk.NewAttribute("group", name),
		sdk.NewAttribute("policy_address", msg.Authority),
		sdk.NewAttribute("validator", msg.ValidatorAddress),
		sdk.NewAttribute("amount", msg.Amount.String()),
	))
	return &types.MsgUndelegateTreasuryResponse{CompletionTime: out.CompletionTime.Unix()}, nil
}
