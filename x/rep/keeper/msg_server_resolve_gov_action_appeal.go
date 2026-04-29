package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// ResolveGovActionAppeal resolves a pending appeal via commons council
// Operations Committee authority. Executes the bond flow and forum counter
// update per the verdict, then transitions the appeal to its terminal status.
//
// UPHELD:     50% of appellant bond burned, 50% retained in rep module (tops
//             up the sentinel reward pool). Forum counter RecordSentinelActionUpheld.
// OVERTURNED: 100% refund to appellant. Sentinel slashed DefaultSentinelOverturnSlash
//             DREAM. Forum counter RecordSentinelActionOverturned (which may trigger
//             demotion on streak).
//
// TIMEOUT and UNSPECIFIED verdicts are rejected — TIMEOUT is driven only by
// the EndBlocker (see TimeoutExpiredAppeals).
func (k msgServer) ResolveGovActionAppeal(ctx context.Context, msg *types.MsgResolveGovActionAppeal) (*types.MsgResolveGovActionAppealResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Resolver); err != nil {
		return nil, errorsmod.Wrap(err, "invalid resolver address")
	}

	if !k.isCouncilAuthorized(ctx, msg.Resolver, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority,
			"only commons council operations committee can resolve gov action appeals")
	}

	appeal, err := k.GovActionAppeal.Get(ctx, msg.AppealId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrAppealNotFound, fmt.Sprintf("appeal %d", msg.AppealId))
	}

	if appeal.Status != types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
		return nil, errorsmod.Wrapf(types.ErrAppealNotPending,
			"appeal %d has status %s", msg.AppealId, appeal.Status.String())
	}

	if msg.Verdict != types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD &&
		msg.Verdict != types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED {
		return nil, errorsmod.Wrapf(types.ErrInvalidAppealVerdict,
			"verdict must be UPHELD or OVERTURNED, got %s", msg.Verdict.String())
	}

	bond, err := parseIntOrZero(appeal.AppealBond)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid appeal bond on appeal record")
	}
	appellantAddr, err := sdk.AccAddressFromBech32(appeal.Appellant)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid appellant address on appeal record")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	switch msg.Verdict {
	case types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD:
		// Half of the bond is burned; the other half is moved to the sentinel
		// reward pool sub-address. Both halves flow through the rep module
		// account briefly so BurnCoins (which requires module-account Burner
		// permission) has a registered identity to burn from.
		if bond.IsPositive() {
			half := bond.QuoRaw(2)
			remainder := bond.Sub(half)
			if half.IsPositive() {
				burnCoins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, half))
				if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), authtypes.NewModuleAddress(types.ModuleName), burnCoins); err != nil {
					return nil, errorsmod.Wrap(err, "failed to move appeal bond half to module account")
				}
				if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burnCoins); err != nil {
					return nil, errorsmod.Wrap(err, "failed to burn appeal bond half")
				}
			}
			if remainder.IsPositive() {
				poolCoins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, remainder))
				if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), SentinelRewardPoolAddress(), poolCoins); err != nil {
					return nil, errorsmod.Wrap(err, "failed to forward appeal bond remainder to sentinel pool")
				}
			}
		}

		// Release the sentinel's reserved bond (the action was upheld, so no
		// slash applies — the reservation must be freed so future actions can
		// draw on the same pool).
		if fk := k.late.forumKeeper; fk != nil {
			sentinelAddr, sErr := fk.GetActionSentinel(ctx, appeal.ActionType, appeal.ActionTarget)
			if sErr != nil {
				sdkCtx.Logger().Warn("failed to resolve sentinel for upheld appeal",
					"appeal_id", msg.AppealId, "error", sErr)
			} else if sentinelAddr != "" {
				committed, cErr := fk.GetActionCommittedAmount(ctx, appeal.ActionType, appeal.ActionTarget)
				if cErr != nil {
					sdkCtx.Logger().Warn("failed to read sentinel committed amount on uphold",
						"sentinel", sentinelAddr, "appeal_id", msg.AppealId, "error", cErr)
				} else if committed.IsPositive() {
					if err := k.ReleaseBond(ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinelAddr, committed); err != nil {
						sdkCtx.Logger().Warn("failed to release sentinel bond on uphold",
							"sentinel", sentinelAddr, "appeal_id", msg.AppealId, "error", err)
					}
				}
			}
		}

		// Forum counter update (best-effort — logs warning on missing record).
		if fk := k.late.forumKeeper; fk != nil {
			if err := fk.RecordSentinelActionUpheld(ctx, appeal.ActionType, appeal.ActionTarget); err != nil {
				sdkCtx.Logger().Warn("failed to record sentinel action upheld",
					"appeal_id", msg.AppealId, "error", err)
			}
		}

	case types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED:
		// Full bond refund to appellant.
		if bond.IsPositive() {
			refundCoins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, bond))
			if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), appellantAddr, refundCoins); err != nil {
				return nil, errorsmod.Wrap(err, "failed to refund appeal bond")
			}
		}

		// Resolve sentinel from forum records (before the forum adapter
		// updates counters — lookup is idempotent).
		var sentinelAddr string
		if fk := k.late.forumKeeper; fk != nil {
			sentinelAddr, err = fk.GetActionSentinel(ctx, appeal.ActionType, appeal.ActionTarget)
			if err != nil {
				sdkCtx.Logger().Warn("failed to resolve sentinel for overturned appeal",
					"appeal_id", msg.AppealId, "error", err)
			}
		}

		// Slash the sentinel (if resolvable). Missing sentinel is a soft
		// error — forum's record may have been GC'd.
		if sentinelAddr != "" {
			slashAmount := math.NewInt(types.DefaultSentinelOverturnSlash)
			if err := k.SlashBond(ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinelAddr, slashAmount, "appeal_overturned"); err != nil {
				sdkCtx.Logger().Warn("failed to slash sentinel bond on overturn",
					"sentinel", sentinelAddr, "appeal_id", msg.AppealId, "error", err)
			}
		}

		// Forum counter update (handles demotion-on-streak internally).
		if fk := k.late.forumKeeper; fk != nil {
			if err := fk.RecordSentinelActionOverturned(ctx, appeal.ActionType, appeal.ActionTarget); err != nil {
				sdkCtx.Logger().Warn("failed to record sentinel action overturned",
					"appeal_id", msg.AppealId, "error", err)
			}
		}
	}

	appeal.Status = msg.Verdict
	if err := k.GovActionAppeal.Set(ctx, msg.AppealId, appeal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update appeal")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"gov_action_appeal_resolved",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", msg.AppealId)),
			sdk.NewAttribute("verdict", msg.Verdict.String()),
			sdk.NewAttribute("resolver", msg.Resolver),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("action_type", appeal.ActionType.String()),
			sdk.NewAttribute("action_target", appeal.ActionTarget),
		),
	)

	return &types.MsgResolveGovActionAppealResponse{}, nil
}
