package keeper

import (
	"context"
	"fmt"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Nominate nominates content for retroactive public goods funding.
func (k msgServer) Nominate(ctx context.Context, msg *types.MsgNominate) (*types.MsgNominateResponse, error) {
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

	// 2. Check creator is a member with sufficient trust level
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "reputation module not available")
	}
	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	if !k.repKeeper.IsMember(ctx, creatorAddr) {
		return nil, types.ErrNotMember
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	trustLevel, err := k.repKeeper.GetTrustLevel(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get trust level")
	}
	if trustLevel < reptypes.TrustLevel(params.NominationMinTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"trust level %d < required %d", trustLevel, params.NominationMinTrustLevel)
	}

	// 3. Validate rationale length
	if uint32(len(msg.Rationale)) > params.NominationRationaleMaxLength {
		return nil, errorsmod.Wrapf(types.ErrRationaleTooLong,
			"rationale length %d exceeds max %d", len(msg.Rationale), params.NominationRationaleMaxLength)
	}

	// 4. Validate content_ref format
	if err := k.ValidateContentRef(ctx, msg.ContentRef); err != nil {
		return nil, err
	}

	// 5. Count existing nominations by this creator for this season; reject if >= MaxNominationsPerMember
	// 6. Check no duplicate content_ref for this season
	var creatorNomCount uint64
	err = k.Nomination.Walk(ctx, nil, func(id uint64, nom types.Nomination) (bool, error) {
		if nom.Season != season.Number {
			return false, nil
		}
		if nom.Nominator == msg.Creator {
			creatorNomCount++
		}
		if nom.ContentRef == msg.ContentRef {
			return true, types.ErrAlreadyNominated
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	if creatorNomCount >= params.MaxNominationsPerMember {
		return nil, errorsmod.Wrapf(types.ErrMaxNominationsReached,
			"already have %d nominations (max %d)", creatorNomCount, params.MaxNominationsPerMember)
	}

	// 7. Create Nomination record with auto-incremented ID
	seqVal, err := k.NominationSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next nomination ID")
	}
	nominationID := seqVal + 1

	// 8-9. Set initial conviction, total_staked, reward_amount to zero Dec, rewarded = false
	nomination := types.Nomination{
		Id:             nominationID,
		Nominator:      msg.Creator,
		ContentRef:     msg.ContentRef,
		Rationale:      msg.Rationale,
		CreatedAtBlock: sdkCtx.BlockHeight(),
		Season:         season.Number,
		TotalStaked:    math.LegacyZeroDec(),
		Conviction:     math.LegacyZeroDec(),
		RewardAmount:   math.LegacyZeroDec(),
		Rewarded:       false,
	}

	// 10. Save to Nomination collection
	if err := k.Nomination.Set(ctx, nominationID, nomination); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save nomination")
	}

	// 11. Emit "nomination_created" event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"nomination_created",
			sdk.NewAttribute("nomination_id", fmt.Sprintf("%d", nominationID)),
			sdk.NewAttribute("nominator", msg.Creator),
			sdk.NewAttribute("content_ref", msg.ContentRef),
			sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
		),
	)

	// 12. Return nomination_id
	return &types.MsgNominateResponse{
		NominationId: nominationID,
	}, nil
}
