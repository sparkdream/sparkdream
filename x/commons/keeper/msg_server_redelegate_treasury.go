package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"sparkdream/x/commons/types"
)

// RedelegateTreasury moves treasury stake to another validator
// (treasury_staking.go). The destination is checked like a new delegation;
// the total staked does not change, so the stake cap and the epoch budget
// are not touched. Like undelegation it has no activation or term gate: it
// puts nothing more at stake, and it is how a group leaves a failing
// validator, or one a member has come to operate, without an unbonding
// wait.
func (k msgServer) RedelegateTreasury(goCtx context.Context, msg *types.MsgRedelegateTreasury) (*types.MsgRedelegateTreasuryResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	name, group, err := k.treasuryGroup(ctx, msg.Authority)
	if err != nil {
		return nil, err
	}
	if err := k.validateStakeAmount(ctx, msg.Amount); err != nil {
		return nil, err
	}
	if err := k.checkTreasuryValidator(ctx, name, group, msg.DstValidatorAddress); err != nil {
		return nil, err
	}

	res, err := k.routeAs(ctx, &stakingtypes.MsgBeginRedelegate{
		DelegatorAddress:    msg.Authority,
		ValidatorSrcAddress: msg.SrcValidatorAddress,
		ValidatorDstAddress: msg.DstValidatorAddress,
		Amount:              msg.Amount,
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "redelegation failed")
	}
	var out stakingtypes.MsgBeginRedelegateResponse
	if err := k.routedResponse(res, &out); err != nil {
		return nil, err
	}
	// After the move, so a treasury at the validator limit can move a whole
	// delegation to a new validator.
	if err := k.checkTreasuryPositions(ctx, name, group, false); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"treasury_redelegated",
		sdk.NewAttribute("group", name),
		sdk.NewAttribute("policy_address", msg.Authority),
		sdk.NewAttribute("src_validator", msg.SrcValidatorAddress),
		sdk.NewAttribute("dst_validator", msg.DstValidatorAddress),
		sdk.NewAttribute("amount", msg.Amount.String()),
	))
	return &types.MsgRedelegateTreasuryResponse{CompletionTime: out.CompletionTime.Unix()}, nil
}
