package keeper

import (
	"context"
	"time"

	"cosmossdk.io/collections"
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
	_, extGroup, found := k.getGroupByPolicy(ctx, msg.Authority)
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

	// 5. CHECK: Rate Limit (Cumulative per Epoch)
	// Track cumulative spending per epoch (1 epoch = 1 day = 86400 seconds).
	// This prevents multiple transactions within the same epoch from draining the treasury.
	if extGroup.MaxSpendPerEpoch != nil && extGroup.MaxSpendPerEpoch.GT(math.NewInt(0)) {
		limit := *extGroup.MaxSpendPerEpoch
		epochDay := ctx.BlockTime().Unix() / 86400

		// Get the uspark amount from this transaction
		requestedUspark := msg.Amount.AmountOf("uspark")

		// Single-transaction check
		if requestedUspark.GT(limit) {
			return nil, errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"spend request %s uspark exceeds epoch limit of %s uspark", requestedUspark, limit)
		}

		// Cumulative epoch check
		key := collections.Join(msg.Authority, epochDay)
		cumulativeSpent := math.ZeroInt()
		if prev, err := k.EpochSpending.Get(ctx, key); err == nil {
			var ok bool
			cumulativeSpent, ok = math.NewIntFromString(prev)
			if !ok {
				cumulativeSpent = math.ZeroInt()
			}
		}

		newTotal := cumulativeSpent.Add(requestedUspark)
		if newTotal.GT(limit) {
			return nil, errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"cumulative spend this epoch %s + request %s = %s exceeds limit %s uspark",
				cumulativeSpent, requestedUspark, newTotal, limit)
		}

		// Record updated cumulative spending
		if err := k.EpochSpending.Set(ctx, key, newTotal.String()); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update epoch spending tracker")
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
