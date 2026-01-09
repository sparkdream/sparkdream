package keeper

import (
	"context"
	"time"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"sparkdream/x/commons/types"
)

func (k msgServer) SpendFromCommons(goCtx context.Context, msg *types.MsgSpendFromCommons) (*types.MsgSpendFromCommonsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// 1. Validate Addresses
	authorityAddr, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid authority address")
	}

	recipientAddr, err := k.addressCodec.StringToBytes(msg.Recipient)
	if err != nil {
		return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "invalid recipient address")
	}

	// 2. LOOKUP: Identify the Group
	// This ensures only registered groups can use this message.
	_, extGroup, found := k.getExtendedGroupByPolicy(ctx, msg.Authority)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrGroupNotFound,
			"signer %s is not a registered group policy", msg.Authority)
	}

	// 3. CHECK: Activation (Shell Groups)
	// If ActivationTime is in the future, funds are locked (Accumulation Phase).
	if extGroup.ActivationTime > 0 && ctx.BlockTime().Unix() < extGroup.ActivationTime {
		activationTime := time.Unix(extGroup.ActivationTime, 0)
		return nil, errorsmod.Wrapf(types.ErrGroupNotActive,
			"group is in pre-launch phase; active from %s", activationTime.String())
	}

	// 4. CHECK: Expiration (Zombie Groups)
	// If the Term has expired, the Group loses spending power until renewed by Parent.
	if extGroup.CurrentTermExpiration > 0 && ctx.BlockTime().Unix() > extGroup.CurrentTermExpiration {
		expirationTime := time.Unix(extGroup.CurrentTermExpiration, 0)
		return nil, errorsmod.Wrapf(types.ErrGroupExpired,
			"group term ended on %s; parent must renew membership", expirationTime.String())
	}

	// 5. CHECK: Rate Limit (Cap per Transaction)
	// Note: Ideally we track "Amount Spent This Epoch", but as a baseline safety,
	// we ensure this single transaction does not exceed the limit.
	if extGroup.MaxSpendPerEpoch != nil && extGroup.MaxSpendPerEpoch.GT(math.NewInt(0)) {
		// Check if the request exceeds the limit
		// We use IsAnyGT because msg.Amount is sdk.Coins (could be multiple denoms)
		limitCoin := sdk.NewCoin("uspark", *extGroup.MaxSpendPerEpoch)
		if !limitCoin.IsZero() && msg.Amount.IsAnyGT(sdk.NewCoins(limitCoin)) {
			return nil, errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"spend request %s exceeds group limit of %s", msg.Amount, limitCoin)
		}
	}

	// 6. EXECUTE
	// Transfer from the Group Policy (Authority) -> Recipient
	err = k.bankKeeper.SendCoins(ctx, authorityAddr, recipientAddr, msg.Amount)
	if err != nil {
		return nil, errorsmod.Wrap(err, "transfer failed")
	}

	return &types.MsgSpendFromCommonsResponse{}, nil
}
