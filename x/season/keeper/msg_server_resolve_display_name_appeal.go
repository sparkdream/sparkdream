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

	// 6. Settle stakes based on outcome using shared dreamutil.SettleStakes
	if appealSucceeded {
		// Appeal succeeded: name was fine, reporter was wrong
		// Winner = appellant (stake returned), Loser = reporter (stake burned)
		if appealStakeErr == nil && reportStakeErr == nil {
			if err := k.dreamOps.SettleStakes(ctx, appealStake.Appellant, appealStake.Amount.Uint64(), reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to settle stakes")
			}
		} else if appealStakeErr == nil {
			if err := k.dreamOps.Unlock(ctx, appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to unlock appellant stake")
			}
		} else if reportStakeErr == nil {
			if err := k.dreamOps.Burn(ctx, reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to burn reporter stake")
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
			if err := k.dreamOps.SettleStakes(ctx, reportStake.Reporter, reportStake.Amount.Uint64(), appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to settle stakes")
			}
		} else if appealStakeErr == nil {
			if err := k.dreamOps.Burn(ctx, appealStake.Appellant, appealStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to burn appellant stake")
			}
		} else if reportStakeErr == nil {
			if err := k.dreamOps.Unlock(ctx, reportStake.Reporter, reportStake.Amount.Uint64()); err != nil {
				return errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to unlock reporter stake")
			}
		}
		// Display name stays cleared (already cleared during report)
	}

	// 7. Update moderation record
	moderation.Active = false
	moderation.AppealSucceeded = appealSucceeded
	if err := k.DisplayNameModeration.Set(ctx, member, moderation); err != nil {
		return errorsmod.Wrap(err, "failed to update moderation record")
	}

	// 8. Clean up stake records
	if reportStakeErr == nil {
		_ = k.DisplayNameReportStake.Remove(ctx, reportChallengeID)
	}
	if appealStakeErr == nil {
		_ = k.DisplayNameAppealStake.Remove(ctx, moderation.AppealChallengeId)
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
