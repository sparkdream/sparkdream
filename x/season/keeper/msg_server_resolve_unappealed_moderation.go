package keeper

import (
	"context"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveUnappealedModeration resolves a display name moderation where the appeal period
// expired without an appeal being filed. The report is upheld: reporter's stake is returned,
// display name stays cleared, and the moderation record is closed.
// Authorized: Commons Council policy address, Commons Operations Committee, or governance authority.
func (k msgServer) ResolveUnappealedModeration(ctx context.Context, msg *types.MsgResolveUnappealedModeration) (*types.MsgResolveUnappealedModerationResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Check authorization
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for moderation resolution")
	}

	if err := k.ResolveUnappealedModerationInternal(ctx, msg.Member); err != nil {
		return nil, err
	}

	return &types.MsgResolveUnappealedModerationResponse{}, nil
}

// ResolveUnappealedModerationInternal is the core resolution logic for unappealed moderations.
// It is a public method on Keeper for use by BeginBlock auto-resolution.
func (k Keeper) ResolveUnappealedModerationInternal(ctx context.Context, member string) error {
	if _, err := k.addressCodec.StringToBytes(member); err != nil {
		return errorsmod.Wrap(err, "invalid member address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 1. Load moderation record
	// The record follows a migrated member (REP-R-101): a resolution
	// naming the old address reaches it through the alias.
	member = k.canonical(ctx, member)
	moderation, err := k.DisplayNameModeration.Get(ctx, member)
	if err != nil {
		return types.ErrDisplayNameNotModerated
	}

	// 2. Must be active
	if !moderation.Active {
		return types.ErrAppealAlreadyResolved
	}

	// 3. Must NOT have an appeal (that's the whole point — unappealed)
	if moderation.AppealChallengeId != "" {
		return errorsmod.Wrap(types.ErrAppealAlreadySubmitted, "moderation has an appeal; use ResolveDisplayNameAppeal instead")
	}

	// 4. Verify appeal period has expired
	params, err := k.Params.Get(ctx)
	if err != nil {
		return errorsmod.Wrap(err, "failed to get params")
	}
	deadline := moderation.ModeratedAt + commontypes.Saturate[int64](params.DisplayNameAppealPeriodBlocks)
	if sdkCtx.BlockHeight() <= deadline {
		return types.ErrAppealPeriodNotExpired
	}

	// 5. Report upheld — unlock (return) reporter's stake
	reportChallengeID := fmt.Sprintf("dn:%s:%d", moderation.Member, moderation.ModeratedAt)
	reportStake, err := k.DisplayNameReportStake.Get(ctx, reportChallengeID)
	if err == nil {
		// A lock already gone (zeroed reporter) settles as returned
		// (SEA-101).
		if _, unlockErr := k.late.dreamOps.UnlockOrGone(ctx, reportStake.Reporter, reportStake.Amount.Uint64()); unlockErr != nil {
			return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to unlock reporter stake: %s", unlockErr)
		}
		if err := k.DisplayNameReportStake.Remove(ctx, reportChallengeID); err != nil {
			return err
		}
	}

	// 6. Close moderation record (display name stays cleared, and the
	// rejected name stays blocked for this member, SEA-23)
	moderation.Active = false
	moderation.AppealSucceeded = false
	moderation.AutoResolveRetryAt = 0
	blockName(&moderation, moderation.RejectedName)
	if err := k.SetDisplayNameModeration(ctx, member, moderation); err != nil {
		return errorsmod.Wrap(err, "failed to update moderation record")
	}

	// 7. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"display_name_unappealed_moderation_resolved",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("verdict", "report_upheld"),
			sdk.NewAttribute("rejected_name", moderation.RejectedName),
		),
	)

	return nil
}
