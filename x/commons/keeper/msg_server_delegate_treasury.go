package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"sparkdream/x/commons/types"
)

// DelegateTreasury stakes SPARK from a group treasury with a validator
// (treasury_staking.go). It passes the gates of MsgSpendFromCommons
// (activation, term, max_spend_per_epoch) and counts against the group's
// epoch budget: a group that may not spend an amount may not put it at
// stake either.
func (k msgServer) DelegateTreasury(goCtx context.Context, msg *types.MsgDelegateTreasury) (*types.MsgDelegateTreasuryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	name, group, err := k.treasuryGroup(ctx, msg.Authority)
	if err != nil {
		return nil, err
	}
	if err := k.validateStakeAmount(ctx, msg.Amount); err != nil {
		return nil, err
	}
	// The stake cap first: it is the one limit shared by every treasury,
	// and the answer a caller most needs when several limits would refuse.
	staked, bonded, headroom, err := k.TreasuryStake(ctx)
	if err != nil {
		return nil, err
	}
	if msg.Amount.Amount.GT(headroom) {
		return nil, errorsmod.Wrapf(types.ErrTreasuryStakeCap,
			"treasuries have %s staked of %s bonded; at most %s more may be delegated (cap %d bps)",
			staked, bonded, headroom, TreasuryStakeCapBps)
	}
	if err := k.checkTreasuryValidator(ctx, name, group, msg.ValidatorAddress); err != nil {
		return nil, err
	}
	if err := k.checkSpendGates(ctx, msg.Authority, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, err
	}

	if _, err := k.routeAs(ctx, &stakingtypes.MsgDelegate{
		DelegatorAddress: msg.Authority,
		ValidatorAddress: msg.ValidatorAddress,
		Amount:           msg.Amount,
	}); err != nil {
		return nil, errorsmod.Wrap(err, "delegation failed")
	}
	// After the delegation: the validator limit and the member-validator
	// rule over everything the treasury now holds.
	if err := k.checkTreasuryPositions(ctx, name, group, true); err != nil {
		return nil, err
	}
	if err := k.recordEpochSpend(ctx, msg.Authority, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, err
	}
	if err := k.TreasuryStakers.Set(ctx, msg.Authority); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"treasury_delegated",
		sdk.NewAttribute("group", name),
		sdk.NewAttribute("policy_address", msg.Authority),
		sdk.NewAttribute("validator", msg.ValidatorAddress),
		sdk.NewAttribute("amount", msg.Amount.String()),
	))
	return &types.MsgDelegateTreasuryResponse{}, nil
}
