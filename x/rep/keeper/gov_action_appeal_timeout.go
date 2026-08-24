package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// maxAppealTimeoutsPerBlock bounds the EndBlocker work to a safe amount of
// gas-equivalent state writes per block.
const maxAppealTimeoutsPerBlock = 50

// TimeoutExpiredAppeals walks pending GovActionAppeal records past their
// Deadline and gives each a terminal outcome.
//
// For each expired appeal it first attempts a deadline jury tally by running
// TallyJuryVotes, which enforces the quorum + supermajority rules centrally: if a
// quorum of the seated jury voted decisively it applies the verdict
// (overturn/uphold) through the normal dispatch. This closes the
// partial-participation gap — the vote-triggered path only fires once a
// *supermajority of votes is cast*, so an appeal where, say, 3 of 5 jurors voted
// decisively would otherwise expire un-decided.
//
// If no quorum voted (or the tally is inconclusive), the appeal TIMES OUT: half
// of the appellant bond is refunded and the other half burned. No forum counter
// update and no sentinel penalty — neither party is blamed for jurors failing to
// reach a verdict.
//
// Note: this is the only working deadline-resolution path for appeals. The
// generic jury EndBlocker loop (IterateActiveJuryReviews in abci.go) cannot
// resolve appeals — see the comment there.
func (k Keeper) TimeoutExpiredAppeals(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	processed := 0
	iter, err := k.GovActionAppeal.Iterate(ctx, nil)
	if err != nil {
		return fmt.Errorf("iterate gov action appeals: %w", err)
	}
	defer iter.Close()

	type pending struct {
		id     uint64
		appeal types.GovActionAppeal
	}
	var toProcess []pending

	for ; iter.Valid(); iter.Next() {
		if processed >= maxAppealTimeoutsPerBlock {
			break
		}
		kv, err := iter.KeyValue()
		if err != nil {
			continue
		}
		appeal := kv.Value
		if appeal.Status != types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
			continue
		}
		if appeal.Deadline == 0 || appeal.Deadline >= now {
			continue
		}
		toProcess = append(toProcess, pending{id: kv.Key, appeal: appeal})
		processed++
	}

	// Close the iterator before mutating.
	iter.Close()

	for _, p := range toProcess {
		// Deadline jury tally: if a quorum of the seated jury has voted, let the
		// verdict decide instead of a neutral timeout. TallyJuryVotes applies the
		// verdict via the gov_action_appeal dispatch (idempotent on appeal
		// status); if it resolves the appeal we skip the timeout branch.
		if review, rErr := k.GetJuryReview(ctx, p.appeal.InitiativeId); rErr == nil {
			// TallyJuryVotes enforces the quorum + supermajority rules and, for a
			// decisive appeal, applies the verdict via dispatch (idempotent on
			// status). If it resolves the appeal, skip the timeout fallback.
			if tErr := k.TallyJuryVotes(ctx, review.Id); tErr != nil {
				sdkCtx.Logger().Error("deadline jury tally failed",
					"appeal_id", p.id, "review_id", review.Id, "error", tErr)
			} else if updated, gErr := k.GovActionAppeal.Get(ctx, p.id); gErr == nil &&
				updated.Status != types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING {
				// Jury reached a verdict — appeal already finalized.
				continue
			}
		}

		bond, parseErr := parseIntOrZero(p.appeal.AppealBond)
		if parseErr != nil {
			sdkCtx.Logger().Error("invalid appeal bond on timeout",
				"appeal_id", p.id, "error", parseErr)
			continue
		}
		if bond.IsPositive() {
			half := bond.QuoRaw(2)
			refund := bond.Sub(half)
			if refund.IsPositive() {
				refundCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), refund))
				appellantAddr, addrErr := sdk.AccAddressFromBech32(p.appeal.Appellant)
				if addrErr != nil {
					sdkCtx.Logger().Error("invalid appellant on appeal",
						"appeal_id", p.id, "error", addrErr)
				} else if err := k.bankKeeper.SendCoins(
					ctx, AppealBondEscrowAddress(), appellantAddr, refundCoins,
				); err != nil {
					sdkCtx.Logger().Error("failed to refund appeal bond on timeout",
						"appeal_id", p.id, "error", err)
				}
			}
			if half.IsPositive() {
				// Round-trip through the rep module account so BurnCoins has a
				// module-account identity with Burner permission.
				burnCoins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), half))
				// Module-aware send: a plain SendCoins to the raw module address
				// creates a BaseAccount there and the BurnCoins below then panics
				// resolving it as a module account.
				if err := k.bankKeeper.SendCoinsFromAccountToModule(
					ctx, AppealBondEscrowAddress(), types.ModuleName, burnCoins,
				); err != nil {
					sdkCtx.Logger().Error("failed to move appeal bond half to module account",
						"appeal_id", p.id, "error", err)
				} else if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, burnCoins); err != nil {
					sdkCtx.Logger().Error("failed to burn appeal bond on timeout",
						"appeal_id", p.id, "error", err)
				}
			}
		}

		p.appeal.Status = types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT
		if err := k.GovActionAppeal.Set(ctx, p.id, p.appeal); err != nil {
			sdkCtx.Logger().Error("failed to update appeal on timeout",
				"appeal_id", p.id, "error", err)
			continue
		}

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"gov_action_appeal_timeout",
			sdk.NewAttribute("appeal_id", fmt.Sprintf("%d", p.id)),
			sdk.NewAttribute("action_type", p.appeal.ActionType.String()),
			sdk.NewAttribute("action_target", p.appeal.ActionTarget),
			sdk.NewAttribute("appellant", p.appeal.Appellant),
			sdk.NewAttribute("deadline", fmt.Sprintf("%d", p.appeal.Deadline)),
		))
	}

	return nil
}
