package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"

	"sparkdream/x/commons/types"
)

// WithdrawTreasuryRewards pays a treasury's pending staking rewards into the
// treasury (treasury_staking.go). Any account may sign: the withdraw address
// of a policy cannot be changed (MsgSetWithdrawAddress is a forbidden
// message), so the rewards can only land in the treasury itself.
func (k msgServer) WithdrawTreasuryRewards(goCtx context.Context, msg *types.MsgWithdrawTreasuryRewards) (*types.MsgWithdrawTreasuryRewardsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid creator address")
	}
	if _, _, err := k.treasuryGroup(ctx, msg.PolicyAddress); err != nil {
		// A deleted group's treasury keeps earning until its stake is
		// unbonded; its rewards are still swept to the heir.
		winding, werr := k.isWindingDown(ctx, msg.PolicyAddress)
		if werr != nil {
			return nil, werr
		}
		if !winding {
			return nil, err
		}
	}

	res, err := k.routeAs(ctx, &distrtypes.MsgWithdrawDelegatorReward{
		DelegatorAddress: msg.PolicyAddress,
		ValidatorAddress: msg.ValidatorAddress,
	})
	if err != nil {
		return nil, errorsmod.Wrap(err, "reward withdrawal failed")
	}
	var out distrtypes.MsgWithdrawDelegatorRewardResponse
	if err := k.routedResponse(res, &out); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"treasury_rewards_withdrawn",
		sdk.NewAttribute("policy_address", msg.PolicyAddress),
		sdk.NewAttribute("validator", msg.ValidatorAddress),
		sdk.NewAttribute("amount", out.Amount.String()),
	))
	return &types.MsgWithdrawTreasuryRewardsResponse{Amount: out.Amount}, nil
}
