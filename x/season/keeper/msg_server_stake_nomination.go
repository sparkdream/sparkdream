package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// StakeNomination stakes DREAM on a nomination.
func (k msgServer) StakeNomination(ctx context.Context, msg *types.MsgStakeNomination) (*types.MsgStakeNominationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// 1. Get current season, validate status is SEASON_STATUS_NOMINATION
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrSeasonNotActive, "no active season found")
	}
	if season.Status != types.SeasonStatus_SEASON_STATUS_NOMINATION {
		return nil, types.ErrSeasonNotInNominationPhase
	}

	// 2. Get the nomination by ID, validate it exists
	nomination, err := k.Nomination.Get(ctx, msg.NominationId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrNominationNotFound, "nomination %d not found", msg.NominationId)
	}
	// SEA-13: only this season's nominations take stakes; a stake on a past
	// nomination would sit locked until the next transition.
	if nomination.Season != season.Number {
		return nil, errorsmod.Wrapf(types.ErrNominationNotFound,
			"nomination %d belongs to season %d, not the current season %d", msg.NominationId, nomination.Season, season.Number)
	}

	// 3. Check staker is a member with sufficient trust level
	if k.late.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "reputation module not available")
	}
	stakerAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	if !k.late.repKeeper.IsActiveMember(ctx, stakerAddr) {
		return nil, types.ErrNotMember
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	trustLevel, err := k.late.repKeeper.GetTrustLevel(ctx, stakerAddr)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get trust level")
	}
	if trustLevel < commontypes.Saturate[reptypes.TrustLevel](params.NominationStakeMinTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"trust level %d < required %d", trustLevel, params.NominationStakeMinTrustLevel)
	}

	// 4. Parse amount string as math.LegacyDec, validate >= NominationMinStake
	amount, err := math.LegacyNewDecFromStr(msg.Amount)
	if err != nil {
		return nil, errorsmod.Wrapf(err, "invalid stake amount: %s", msg.Amount)
	}
	// SEA-31: only whole micro-DREAM is locked, so only whole micro-DREAM
	// may score conviction.
	if !amount.Equal(amount.TruncateDec()) {
		return nil, errorsmod.Wrapf(types.ErrStakeAmountFractional, "amount %s", amount.String())
	}
	if amount.LT(params.NominationMinStake) {
		return nil, errorsmod.Wrapf(types.ErrStakeAmountTooLow,
			"amount %s < minimum %s", amount.String(), params.NominationMinStake.String())
	}

	// 5. Check staker hasn't already staked on this nomination
	stakeKey := fmt.Sprintf("%d/%s", msg.NominationId, msg.Creator)
	for _, alias := range k.recentAddressesOf(ctx, msg.Creator) {
		if has, _ := k.NominationStake.Has(ctx, fmt.Sprintf("%d/%s", msg.NominationId, alias)); has {
			return nil, types.ErrNominationStakeExists
		}
	}

	// 6. Check staker is neither the nominator nor the content's author
	// (no self-staking; SEA-08: the spec's rule is about the content author)
	if k.sameMember(ctx, msg.Creator, nomination.Nominator) {
		return nil, errorsmod.Wrap(types.ErrNominationStakeExists, "cannot stake on own nomination")
	}
	// SEA-30: the author is the one stored at Nominate. A nomination
	// without one (created before the field existed) re-resolves the ref,
	// and an unresolvable ref (hidden, deleted) refuses the stake: the ban
	// fails closed.
	author := nomination.Author
	if author == "" {
		a, aErr := k.resolveContentRef(ctx, nomination.ContentRef)
		if aErr != nil {
			return nil, errorsmod.Wrapf(aErr, "nomination %d content can no longer be verified", msg.NominationId)
		}
		author = a
	}
	if author != "" && k.sameMember(ctx, msg.Creator, author) {
		return nil, errorsmod.Wrap(types.ErrNominationStakeExists, "cannot stake on a nomination of your own content")
	}

	// 7. Convert amount to integer and lock DREAM
	amountInt := amount.TruncateInt()
	if amountInt.IsZero() {
		return nil, errorsmod.Wrap(types.ErrStakeAmountTooLow, "truncated amount is zero")
	}
	if !amountInt.IsUint64() {
		// SEA-105: Uint64 below would panic; refuse with a clean error.
		return nil, errorsmod.Wrapf(types.ErrStakeAmountTooHigh, "amount %s", amountInt)
	}
	if err := k.LockDREAM(ctx, msg.Creator, amountInt.Uint64()); err != nil {
		return nil, errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to lock DREAM for nomination stake: %s", err)
	}

	// 8. Create NominationStake record and save
	stake := types.NominationStake{
		NominationId:  msg.NominationId,
		Staker:        msg.Creator,
		Amount:        amount,
		StakedAtBlock: sdkCtx.BlockHeight(),
	}
	if err := k.NominationStake.Set(ctx, stakeKey, stake); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save nomination stake")
	}

	// 9. Update nomination's total_staked by adding amount
	nomination.TotalStaked = nomination.TotalStaked.Add(amount)

	// 10. Conviction is not recomputed here (SEA-20): that walks every
	// stake on the nomination, so late stakers on a popular nomination
	// would pay for all earlier ones. The retro phase computes the final
	// conviction; the nomination queries compute it on read.

	// 11. Save updated nomination
	if err := k.SetNomination(ctx, nomination); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update nomination")
	}

	// 12. Emit "nomination_staked" event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"nomination_staked",
			sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", msg.NominationId)),
			sdk.NewAttribute("staker", msg.Creator),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("total_staked", nomination.TotalStaked.String()),
			sdk.NewAttribute("conviction", nomination.Conviction.String()),
		),
	)

	return &types.MsgStakeNominationResponse{}, nil
}
