package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ResolveDisplayNameAppeal resolves a display name appeal after jury verdict or governance decision.
// Authorized: Commons Council policy address, Commons Operations Committee, or governance authority.
func (k msgServer) ResolveDisplayNameAppeal(ctx context.Context, msg *types.MsgResolveDisplayNameAppeal) (*types.MsgResolveDisplayNameAppealResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Check authorization
	if !k.IsAuthorizedForGamification(ctx, msg.Authority) {
		return nil, errorsmod.Wrap(types.ErrNotAuthorized, "sender not authorized for appeal resolution")
	}

	if err := k.ResolveDisplayNameAppealInternal(ctx, msg.Member, msg.AppealSucceeded); err != nil {
		return nil, err
	}

	return &types.MsgResolveDisplayNameAppealResponse{}, nil
}

// ResolveDisplayNameAppealInternal is the core resolution logic, exposed as a public method
// on Keeper for cross-module use (e.g., x/rep jury system calling back after verdict).
func (k Keeper) ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error {
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

	// 2. Check moderation is still active
	if !moderation.Active {
		return types.ErrAppealAlreadyResolved
	}

	// 3. Check an appeal exists
	if moderation.AppealChallengeId == "" {
		return types.ErrNoAppealToResolve
	}

	// 4. Reconstruct report challenge ID to find reporter's stake
	reportChallengeID := fmt.Sprintf("dn:%s:%d", moderation.Member, moderation.ModeratedAt)

	// 5. Load stake records
	reportStake, reportStakeErr := k.DisplayNameReportStake.Get(ctx, reportChallengeID)
	appealStake, appealStakeErr := k.DisplayNameAppealStake.Get(ctx, moderation.AppealChallengeId)

	// 6. Settle stakes based on outcome. A stake whose lock x/rep reports
	// as gone (its holder was zeroed and the zeroing hook missed the
	// record) settles as nothing released, so one zeroed side can never
	// wedge the case (SEA-101).
	if appealSucceeded {
		// Appeal succeeded: name was fine, reporter was wrong
		// Winner = appellant (stake returned), Loser = reporter (stake burned)
		if appealStakeErr == nil && reportStakeErr == nil {
			if err := k.late.dreamOps.SettleStakesOrGone(ctx, appealStake.Appellant, appealStake.Amount.Uint64(), reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to settle stakes: %s", err)
			}
		} else if appealStakeErr == nil {
			if _, err := k.late.dreamOps.UnlockOrGone(ctx, appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to unlock appellant stake: %s", err)
			}
		} else if reportStakeErr == nil {
			if _, err := k.late.dreamOps.BurnLockedOrGone(ctx, reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to burn reporter stake: %s", err)
			}
		}

		// Restore display name to member's profile
		profile, err := k.MemberProfile.Get(ctx, member)
		if err == nil {
			profile.DisplayName = moderation.RejectedName
			if err := k.MemberProfile.Set(ctx, member, profile); err != nil {
				return errorsmod.Wrap(err, "failed to restore display name")
			}
		}
	} else {
		// Appeal failed: name was indeed bad, appellant was wrong
		// Winner = reporter (stake returned), Loser = appellant (stake burned)
		if reportStakeErr == nil && appealStakeErr == nil {
			if err := k.late.dreamOps.SettleStakesOrGone(ctx, reportStake.Reporter, reportStake.Amount.Uint64(), appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to settle stakes: %s", err)
			}
		} else if appealStakeErr == nil {
			if _, err := k.late.dreamOps.BurnLockedOrGone(ctx, appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to burn appellant stake: %s", err)
			}
		} else if reportStakeErr == nil {
			if _, err := k.late.dreamOps.UnlockOrGone(ctx, reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to unlock reporter stake: %s", err)
			}
		}
		// Display name stays cleared (already cleared during report)
	}

	// 7. Update moderation record. An upheld report blocks the rejected
	// name for this member for good (SEA-23).
	moderation.Active = false
	moderation.AppealSucceeded = appealSucceeded
	if !appealSucceeded {
		blockName(&moderation, moderation.RejectedName)
	}
	if err := k.SetDisplayNameModeration(ctx, member, moderation); err != nil {
		return errorsmod.Wrap(err, "failed to update moderation record")
	}

	// 8. Clean up stake records
	if reportStakeErr == nil {
		if err := k.DisplayNameReportStake.Remove(ctx, reportChallengeID); err != nil {
			return err
		}
	}
	if appealStakeErr == nil {
		if err := k.DisplayNameAppealStake.Remove(ctx, moderation.AppealChallengeId); err != nil {
			return err
		}
	}

	// 9. Emit event
	verdict := "rejected"
	if appealSucceeded {
		verdict = "upheld"
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"display_name_appeal_resolved",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("verdict", verdict),
			sdk.NewAttribute("appeal_succeeded", fmt.Sprintf("%t", appealSucceeded)),
			sdk.NewAttribute("rejected_name", moderation.RejectedName),
		),
	)

	return nil
}

// ResolveAppealTimeoutInternal closes an appeal that nobody resolved within
// AppealResolutionPeriodMultiplier x display_name_appeal_period_blocks of
// the appeal (SEA-23). With no verdict
// neither side is penalised: both stakes are returned, the name is restored
// (it was only ever hidden pending review) and it is NOT blocked. Called by
// the BeginBlocker.
func (k Keeper) ResolveAppealTimeoutInternal(ctx context.Context, member string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// The record follows a migrated member (REP-R-101): a resolution
	// naming the old address reaches it through the alias.
	member = k.canonical(ctx, member)
	moderation, err := k.DisplayNameModeration.Get(ctx, member)
	if err != nil {
		return types.ErrDisplayNameNotModerated
	}
	if !appealQueued(moderation) {
		return types.ErrNoAppealToResolve
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if sdkCtx.BlockHeight() <= appealDeadline(moderation.AppealedAt, params) {
		return types.ErrAppealPeriodNotExpired
	}

	reportChallengeID := fmt.Sprintf("dn:%s:%d", moderation.Member, moderation.ModeratedAt)
	if reportStake, err := k.DisplayNameReportStake.Get(ctx, reportChallengeID); err == nil {
		if _, err := k.late.dreamOps.UnlockOrGone(ctx, reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
			return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to unlock reporter stake: %s", err)
		}
		if err := k.DisplayNameReportStake.Remove(ctx, reportChallengeID); err != nil {
			return err
		}
	}
	if appealStake, err := k.DisplayNameAppealStake.Get(ctx, moderation.AppealChallengeId); err == nil {
		if _, err := k.late.dreamOps.UnlockOrGone(ctx, appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
			return errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to unlock appellant stake: %s", err)
		}
		if err := k.DisplayNameAppealStake.Remove(ctx, moderation.AppealChallengeId); err != nil {
			return err
		}
	}

	if profile, err := k.MemberProfile.Get(ctx, member); err == nil && profile.DisplayName == "" {
		profile.DisplayName = moderation.RejectedName
		if err := k.MemberProfile.Set(ctx, member, profile); err != nil {
			return errorsmod.Wrap(err, "failed to restore display name")
		}
	}

	moderation.Active = false
	moderation.AppealSucceeded = true
	if err := k.SetDisplayNameModeration(ctx, member, moderation); err != nil {
		return errorsmod.Wrap(err, "failed to update moderation record")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("display_name_appeal_timed_out",
		sdk.NewAttribute("member", member),
		sdk.NewAttribute("rejected_name", moderation.RejectedName),
		sdk.NewAttribute("stakes", "returned"),
	))
	return nil
}
