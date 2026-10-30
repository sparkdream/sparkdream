package keeper

import (
	"context"
	"errors"
	"fmt"

	commontypes "sparkdream/x/common/types"
	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
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
	if k.late.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "reputation module not available")
	}
	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	if !k.late.repKeeper.IsActiveMember(ctx, creatorAddr) {
		return nil, types.ErrNotMember
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	trustLevel, err := k.late.repKeeper.GetTrustLevel(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get trust level")
	}
	if trustLevel < commontypes.Saturate[reptypes.TrustLevel](params.NominationMinTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"trust level %d < required %d", trustLevel, params.NominationMinTrustLevel)
	}

	// 3. Validate rationale length
	if commontypes.Saturate[uint32](len(msg.Rationale)) > params.NominationRationaleMaxLength {
		return nil, errorsmod.Wrapf(types.ErrRationaleTooLong,
			"rationale length %d exceeds max %d", len(msg.Rationale), params.NominationRationaleMaxLength)
	}

	// 4. Validate content_ref format (canonical ids, content exists)
	author, err := k.resolveContentRef(ctx, msg.ContentRef)
	if err != nil {
		return nil, err
	}
	// The content's creator must be an active member (spec "Content
	// eligibility"; SEA-08).
	if author != "" {
		authorAddr, aErr := k.addressCodec.StringToBytes(author)
		if aErr != nil {
			return nil, errorsmod.Wrapf(types.ErrInvalidContentRef, "content author %s is not a valid address", author)
		}
		member, mErr := k.late.repKeeper.GetMember(ctx, authorAddr)
		if mErr != nil || member.Address == "" || member.Status != reptypes.MemberStatus_MEMBER_STATUS_ACTIVE {
			return nil, errorsmod.Wrapf(types.ErrNotMember, "content author %s is not an active member", author)
		}
	}

	// 5. Count existing nominations by this creator for this season; reject if >= MaxNominationsPerMember
	// 6. Check no duplicate content_ref for this season
	//
	// Both are point reads of the (season, ref) set and the per-(season,
	// nominator) counter, summed over the creator's recent addresses
	// (SEA-20): no walk of the season's nominations.
	if dup, err := k.NominationsByRef.Has(ctx, collections.Join(season.Number, msg.ContentRef)); err != nil {
		return nil, err
	} else if dup {
		return nil, types.ErrAlreadyNominated
	}
	var creatorNomCount uint64
	for _, alias := range k.recentAddressesOf(ctx, msg.Creator) {
		n, err := k.NominationCountBy.Get(ctx, collections.Join(season.Number, alias))
		if err != nil && !errors.Is(err, collections.ErrNotFound) {
			return nil, err
		}
		creatorNomCount += n
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
		Author:         author,
	}

	// 10. Save to Nomination collection
	if err := k.SetNomination(ctx, nomination); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save nomination")
	}
	if err := k.indexNewNomination(ctx, nomination); err != nil {
		return nil, errorsmod.Wrap(err, "failed to index nomination")
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
