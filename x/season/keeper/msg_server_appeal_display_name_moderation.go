package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AppealDisplayNameModeration appeals a display name moderation decision.
// The appellant must stake DREAM which is burned if the appeal fails.
func (k msgServer) AppealDisplayNameModeration(ctx context.Context, msg *types.MsgAppealDisplayNameModeration) (*types.MsgAppealDisplayNameModerationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the moderation record
	moderation, err := k.DisplayNameModeration.Get(ctx, msg.Creator)
	if err != nil {
		return nil, types.ErrDisplayNameNotModerated
	}

	// Check moderation is active
	if !moderation.Active {
		return nil, types.ErrDisplayNameNotModerated
	}

	// Check not already appealed
	if moderation.AppealChallengeId != "" {
		return nil, types.ErrAppealAlreadySubmitted
	}

	// Check appeal period hasn't expired
	params, _ := k.Params.Get(ctx)
	if sdkCtx.BlockHeight() > moderation.ModeratedAt+int64(params.DisplayNameAppealPeriodBlocks) {
		return nil, types.ErrAppealPeriodExpired
	}

	// Escrow appellant's DREAM stake via x/rep integration
	if err := k.LockDREAM(ctx, msg.Creator, params.DisplayNameAppealStakeDream.Uint64()); err != nil {
		return nil, errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to escrow DREAM stake for appeal")
	}

	// Generate appeal challenge ID
	challengeID := fmt.Sprintf("dn_appeal:%s:%d", msg.Creator, sdkCtx.BlockHeight())

	// Update moderation record with appeal info
	moderation.AppealChallengeId = challengeID
	moderation.AppealedAt = sdkCtx.BlockHeight()

	if err := k.DisplayNameModeration.Set(ctx, msg.Creator, moderation); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update moderation")
	}

	// Store appeal stake record
	appealStake := types.DisplayNameAppealStake{
		ChallengeId: challengeID,
		Appellant:   msg.Creator,
		Amount:      params.DisplayNameAppealStakeDream,
	}
	if err := k.DisplayNameAppealStake.Set(ctx, challengeID, appealStake); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save appeal stake")
	}

	// Note: Jury review creation is triggered via event
	// The x/rep module can listen for this event and create a jury review
	// This decoupled approach avoids circular dependencies

	// Emit event with appeal details for jury review creation
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"display_name_appeal_submitted",
			sdk.NewAttribute("appellant", msg.Creator),
			sdk.NewAttribute("challenge_id", challengeID),
			sdk.NewAttribute("rejected_name", moderation.RejectedName),
			sdk.NewAttribute("reason", msg.AppealReason),
		),
	)

	return &types.MsgAppealDisplayNameModerationResponse{}, nil
}
