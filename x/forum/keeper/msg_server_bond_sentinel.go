package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) BondSentinel(ctx context.Context, msg *types.MsgBondSentinel) (*types.MsgBondSentinelResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// Check reputation tier
	repTier := k.GetRepTier(ctx, msg.Creator)
	if repTier < types.DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrapf(types.ErrInsufficientReputation, "tier %d required, have %d", types.DefaultMinRepTierSentinel, repTier)
	}

	// Parse bond amount
	bondAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || bondAmount.IsNegative() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bond amount")
	}

	minBond := math.NewInt(1000) // DefaultMinSentinelBond
	if bondAmount.LT(minBond) {
		return nil, errorsmod.Wrapf(types.ErrBondAmountTooSmall, "minimum bond is %s DREAM", minBond.String())
	}

	// Load or create sentinel activity record
	sentinelActivity, err := k.SentinelActivity.Get(ctx, msg.Creator)
	if err != nil {
		// New sentinel
		sentinelActivity = types.SentinelActivity{
			Address:     msg.Creator,
			CurrentBond: "0",
			BondStatus:  types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		}
	}

	// Check if in demotion cooldown
	if sentinelActivity.DemotionCooldownUntil > now {
		return nil, errorsmod.Wrapf(types.ErrDemotionCooldown, "cannot bond until %d", sentinelActivity.DemotionCooldownUntil)
	}

	// Transfer DREAM from user to module (stub - actual transfer via x/rep)
	if err := k.TransferDREAM(ctx, msg.Creator, k.GetModuleAddress(), bondAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to transfer DREAM bond")
	}

	// Update bond
	currentBond, _ := math.NewIntFromString(sentinelActivity.CurrentBond)
	if sentinelActivity.CurrentBond == "" {
		currentBond = math.ZeroInt()
	}
	newBond := currentBond.Add(bondAmount)
	sentinelActivity.CurrentBond = newBond.String()

	// Update bond status
	demotionThreshold := math.NewInt(500) // DefaultSentinelDemotionThreshold
	if newBond.GTE(minBond) {
		sentinelActivity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL
	} else if newBond.GTE(demotionThreshold) {
		sentinelActivity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY
	} else {
		sentinelActivity.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED
	}

	// Store sentinel activity
	if err := k.SentinelActivity.Set(ctx, msg.Creator, sentinelActivity); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store sentinel activity")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"sentinel_bonded",
			sdk.NewAttribute("sentinel", msg.Creator),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("total_bond", sentinelActivity.CurrentBond),
			sdk.NewAttribute("bond_status", sentinelActivity.BondStatus.String()),
		),
	)

	return &types.MsgBondSentinelResponse{}, nil
}
