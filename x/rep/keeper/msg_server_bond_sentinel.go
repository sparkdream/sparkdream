package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultMinSentinelBondAmount is the minimum DREAM required to bond as a sentinel.
const DefaultMinSentinelBondAmount int64 = 1000

// DefaultSentinelDemotionCooldown is the seconds a sentinel must wait
// after being demoted before bonding again.
const DefaultSentinelDemotionCooldown int64 = 604800 // 7 days

// DefaultSentinelDemotionThreshold is the minimum bond to stay in RECOVERY
// status; below this the sentinel is DEMOTED.
const DefaultSentinelDemotionThreshold int64 = 500

// DefaultMinRepTierSentinel is the minimum reputation tier required to bond
// as a sentinel.
const DefaultMinRepTierSentinel uint64 = 3

func (k msgServer) BondSentinel(ctx context.Context, msg *types.MsgBondSentinel) (*types.MsgBondSentinelResponse, error) {
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	tier, err := k.GetReputationTier(ctx, creatorAddr)
	if err != nil {
		return nil, err
	}
	if tier < DefaultMinRepTierSentinel {
		return nil, errorsmod.Wrapf(types.ErrInsufficientReputation,
			"tier %d required, have %d", DefaultMinRepTierSentinel, tier)
	}

	bondAmount, ok := math.NewIntFromString(msg.Amount)
	if !ok || bondAmount.IsNegative() {
		return nil, errorsmod.Wrap(types.ErrInvalidAmount, "invalid bond amount")
	}

	minBond := math.NewInt(DefaultMinSentinelBondAmount)
	if bondAmount.LT(minBond) {
		return nil, errorsmod.Wrapf(types.ErrBondAmountTooSmall,
			"minimum bond is %s DREAM", minBond.String())
	}

	sa, err := k.SentinelActivity.Get(ctx, msg.Creator)
	if err != nil {
		sa = types.SentinelActivity{
			Address:     msg.Creator,
			CurrentBond: "0",
			BondStatus:  types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
		}
	}

	if sa.DemotionCooldownUntil > now {
		return nil, errorsmod.Wrapf(types.ErrDemotionCooldown,
			"cannot bond until %d", sa.DemotionCooldownUntil)
	}

	// Lock DREAM from the sentinel (author-bond pattern: locked balance
	// cannot decay and is slashable via UnlockDREAM + BurnDREAM).
	if err := k.LockDREAM(ctx, creatorAddr, bondAmount); err != nil {
		return nil, errorsmod.Wrap(err, "failed to lock DREAM bond")
	}

	currentBond := parseIntOrZero(sa.CurrentBond)
	newBond := currentBond.Add(bondAmount)
	sa.CurrentBond = newBond.String()

	demotionThreshold := math.NewInt(DefaultSentinelDemotionThreshold)
	switch {
	case newBond.GTE(minBond):
		sa.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL
	case newBond.GTE(demotionThreshold):
		sa.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_RECOVERY
	default:
		sa.BondStatus = types.SentinelBondStatus_SENTINEL_BOND_STATUS_DEMOTED
	}

	if err := k.SentinelActivity.Set(ctx, msg.Creator, sa); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store sentinel activity")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"sentinel_bonded",
			sdk.NewAttribute("sentinel", msg.Creator),
			sdk.NewAttribute("amount", msg.Amount),
			sdk.NewAttribute("total_bond", sa.CurrentBond),
			sdk.NewAttribute("bond_status", sa.BondStatus.String()),
		),
	)

	return &types.MsgBondSentinelResponse{}, nil
}
