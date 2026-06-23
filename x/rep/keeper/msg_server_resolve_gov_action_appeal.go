package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// govActionAppealInitiativeType is the appeal-type string stored on the jury
// review (ChallengerClaim) for moderation appeals. TallyJuryVotes dispatches on
// it to apply the jury verdict through applyGovActionAppealVerdict.
const govActionAppealInitiativeType = "gov_action_appeal"

// ResolveGovActionAppeal resolves a pending appeal via commons council
// Operations Committee authority. This is the manual-override entry point; the
// same verdict logic is invoked automatically from TallyJuryVotes when a jury
// reaches a verdict (see applyGovActionAppealVerdict).
func (k msgServer) ResolveGovActionAppeal(ctx context.Context, msg *types.MsgResolveGovActionAppeal) (*types.MsgResolveGovActionAppealResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Resolver); err != nil {
		return nil, errorsmod.Wrap(err, "invalid resolver address")
	}

	if !k.isCouncilAuthorized(ctx, msg.Resolver, "commons", "operations") {
		return nil, errorsmod.Wrap(types.ErrNotGovAuthority,
			"only commons council operations committee can resolve gov action appeals")
	}

	if err := k.applyGovActionAppealVerdict(ctx, msg.AppealId, msg.Verdict, msg.Resolver, msg.Reason); err != nil {
		return nil, err
	}

	return &types.MsgResolveGovActionAppealResponse{}, nil
}

// applyGovActionAppealVerdict executes the bond flow, sentinel slashing/release,
// forum counter update, and content reversal for an appeal verdict, then
// transitions the appeal to its terminal status. Shared by the manual committee
// resolver (MsgResolveGovActionAppeal) and the automatic jury path
// (TallyJuryVotes). Idempotent on appeal status: a non-PENDING appeal is
// rejected, so a jury verdict and a committee override cannot double-apply.
//
// UPHELD:     50% of appellant bond burned, 50% retained in rep module (tops up
//
//	the sentinel reward pool). Sentinel's reserved bond released. Forum
//	counter RecordSentinelActionUpheld.
//
// OVERTURNED: 100% refund to appellant. Sentinel slashed by the exact bond the
//
//	action reserved (forum's per-action committed_amount), so slash equals what
//	was reserved. Forum counter RecordSentinelActionOverturned (may trigger
//	demotion on streak) and ReverseSentinelAction (unhide/unlock/un-move/unpin).
//
// resolver and reason are recorded on the emitted event for audit (the manual
// path passes the committee member; the jury path passes "jury:<review id>").
func (k Keeper) applyGovActionAppealVerdict(ctx context.Context, appealID uint64, verdict types.GovAppealStatus, resolver, reason string) error {
	appeal, err := k.GovActionAppeal.Get(ctx, appealID)
	if err != nil {
		return errorsmod.Wrap(types.ErrAppealNotFound, fmt.Sprintf("appeal %d", appealID))
	}

	if appeal.Status != types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
		return errorsmod.Wrapf(types.ErrAppealNotPending,
			"appeal %d has status %s", appealID, appeal.Status.String())
	}

	if verdict != types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD &&
		verdict != types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED {
		return errorsmod.Wrapf(types.ErrInvalidAppealVerdict,
			"verdict must be UPHELD or OVERTURNED, got %s", verdict.String())
	}

	bond, err := parseIntOrZero(appeal.AppealBond)
	if err != nil {
		return errorsmod.Wrap(err, "invalid appeal bond on appeal record")
	}
	appellantAddr, err := sdk.AccAddressFromBech32(appeal.Appellant)
	if err != nil {
		return errorsmod.Wrap(err, "invalid appellant address on appeal record")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	switch verdict {
	case types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD:
		// Half of the bond is burned; the other half is moved to the sentinel
		// reward pool sub-address. Both halves flow through the rep module
		// account briefly so BurnCoins (which requires module-account Burner
		// permission) has a registered identity to burn from.
		if bond.IsPositive() {
			half := bond.QuoRaw(2)
			remainder := bond.Sub(half)
			if half.IsPositive() {
				burnCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), half))
				if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), authtypes.NewModuleAddress(types.ModuleName), burnCoins); err != nil {
					return errorsmod.Wrap(err, "failed to move appeal bond half to module account")
				}
				if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burnCoins); err != nil {
					return errorsmod.Wrap(err, "failed to burn appeal bond half")
				}
			}
			if remainder.IsPositive() {
				poolCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), remainder))
				if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), SentinelRewardPoolAddress(), poolCoins); err != nil {
					return errorsmod.Wrap(err, "failed to forward appeal bond remainder to sentinel pool")
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
					"appeal_id", appealID, "error", sErr)
			} else if sentinelAddr != "" {
				committed, cErr := fk.GetActionCommittedAmount(ctx, appeal.ActionType, appeal.ActionTarget)
				if cErr != nil {
					sdkCtx.Logger().Warn("failed to read sentinel committed amount on uphold",
						"sentinel", sentinelAddr, "appeal_id", appealID, "error", cErr)
				} else if committed.IsPositive() {
					if err := k.ReleaseBond(ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinelAddr, committed); err != nil {
						sdkCtx.Logger().Warn("failed to release sentinel bond on uphold",
							"sentinel", sentinelAddr, "appeal_id", appealID, "error", err)
					}
				}
			}
		}

		// Forum counter update (best-effort — logs warning on missing record).
		if fk := k.late.forumKeeper; fk != nil {
			if err := fk.RecordSentinelActionUpheld(ctx, k.CurrentSentinelRewardEpoch(ctx), appeal.ActionType, appeal.ActionTarget); err != nil {
				sdkCtx.Logger().Warn("failed to record sentinel action upheld",
					"appeal_id", appealID, "error", err)
			}
		}

	case types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED:
		// Full bond refund to appellant.
		if bond.IsPositive() {
			refundCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), bond))
			if err := k.bankKeeper.SendCoins(ctx, AppealBondEscrowAddress(), appellantAddr, refundCoins); err != nil {
				return errorsmod.Wrap(err, "failed to refund appeal bond")
			}
		}

		// Resolve sentinel from forum records (before the forum adapter
		// updates counters — lookup is idempotent).
		var sentinelAddr string
		if fk := k.late.forumKeeper; fk != nil {
			sentinelAddr, err = fk.GetActionSentinel(ctx, appeal.ActionType, appeal.ActionTarget)
			if err != nil {
				sdkCtx.Logger().Warn("failed to resolve sentinel for overturned appeal",
					"appeal_id", appealID, "error", err)
			}
		}

		// Slash the sentinel (if resolvable) by the EXACT bond this action
		// reserved, read back from the forum record. slash == reserved by
		// construction, so the penalty stays correct even if the forum
		// SentinelSlashAmount param ever drifts from a flat default — and the
		// reservation is cleared as part of the slash (SlashBond decrements
		// total_committed_bond too). Mirrors the UPHELD branch's committed-amount
		// release above. Missing sentinel / committed record is a soft error
		// (forum's record may have been GC'd) — skip the slash; the action is
		// still reversed below.
		if sentinelAddr != "" {
			if fk := k.late.forumKeeper; fk != nil {
				committed, cErr := fk.GetActionCommittedAmount(ctx, appeal.ActionType, appeal.ActionTarget)
				if cErr != nil {
					sdkCtx.Logger().Warn("failed to read sentinel committed amount on overturn",
						"sentinel", sentinelAddr, "appeal_id", appealID, "error", cErr)
				} else if committed.IsPositive() {
					if err := k.SlashBond(ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, sentinelAddr, committed, "appeal_overturned"); err != nil {
						sdkCtx.Logger().Warn("failed to slash sentinel bond on overturn",
							"sentinel", sentinelAddr, "appeal_id", appealID, "error", err)
					}
				}
			}
		}

		// Forum counter update (handles demotion-on-streak internally).
		if fk := k.late.forumKeeper; fk != nil {
			if err := fk.RecordSentinelActionOverturned(ctx, k.CurrentSentinelRewardEpoch(ctx), appeal.ActionType, appeal.ActionTarget); err != nil {
				sdkCtx.Logger().Warn("failed to record sentinel action overturned",
					"appeal_id", appealID, "error", err)
			}

			// Reverse the underlying content action: unhide / unlock / un-move /
			// unpin. Without this, the sentinel gets slashed but the user's
			// content stays affected — the appeal loop would be incomplete.
			// Errors here are logged and the appeal still finalizes; the
			// dangling-reference guard inside ReverseSentinelAction may
			// legitimately skip the reversal if the parent category has since
			// been deleted.
			if err := fk.ReverseSentinelAction(ctx, appeal.ActionType, appeal.ActionTarget); err != nil {
				sdkCtx.Logger().Warn("failed to reverse sentinel action on overturn",
					"appeal_id", appealID,
					"action_type", appeal.ActionType.String(),
					"action_target", appeal.ActionTarget,
					"error", err)
			}
		}
	}

	appeal.Status = verdict
	if err := k.GovActionAppeal.Set(ctx, appealID, appeal); err != nil {
		return errorsmod.Wrap(err, "failed to update appeal")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"gov_action_appeal_resolved",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", appealID)),
			sdk.NewAttribute("verdict", verdict.String()),
			sdk.NewAttribute("resolver", resolver),
			sdk.NewAttribute("reason", reason),
			sdk.NewAttribute("action_type", appeal.ActionType.String()),
			sdk.NewAttribute("action_target", appeal.ActionTarget),
		),
	)

	return nil
}

// findGovActionAppealByInitiative returns the PENDING GovActionAppeal whose
// InitiativeId matches the given jury review id. Used by TallyJuryVotes to map a
// resolved appeal jury review back to its appeal record.
func (k Keeper) findGovActionAppealByInitiative(ctx context.Context, initiativeID uint64) (types.GovActionAppeal, bool) {
	var found types.GovActionAppeal
	ok := false
	_ = k.GovActionAppeal.Walk(ctx, nil, func(id uint64, a types.GovActionAppeal) (bool, error) {
		if a.InitiativeId == initiativeID && a.Status == types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
			found = a
			ok = true
			return true, nil
		}
		return false, nil
	})
	return found, ok
}
