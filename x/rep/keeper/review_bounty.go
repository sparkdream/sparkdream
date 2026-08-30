package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Review bounties: DREAM escrowed against one initiative to attract reviewer
// attention to it.
//
// Once completion above review_required_above_budget cannot happen without a
// verdict, reviewer attention becomes the scarcest input in the system and the
// flat budget-derived fee cannot express "this one matters more". A bounty is
// how the people who want a particular initiative looked at bid that attention
// up — and, for permissionless work, how the creator pays for the review their
// own minting consumes instead of diluting everyone.
//
// Paid per verdict filed and split across the round's reviewers, exactly like
// the fee. Never on completion: a bounty contingent on the work being approved
// is a bribe to approve.

// GetReviewBounty returns the escrowed bounty for an initiative, or a zero
// record when none exists.
func (k Keeper) GetReviewBounty(ctx context.Context, initiativeID uint64) types.ReviewBounty {
	b, err := k.ReviewBounty.Get(ctx, initiativeID)
	if err != nil {
		return types.ReviewBounty{InitiativeId: initiativeID, Amount: math.ZeroInt()}
	}
	if b.Amount.IsNil() {
		b.Amount = math.ZeroInt()
	}
	return b
}

// FundReviewBounty escrows DREAM from a funder against an initiative.
func (k Keeper) EscrowReviewBounty(ctx context.Context, funder sdk.AccAddress, initiativeID uint64, amount math.Int) (math.Int, error) {
	if amount.IsNil() || !amount.IsPositive() {
		return math.ZeroInt(), fmt.Errorf("%w: bounty amount must be positive", types.ErrInvalidRequest)
	}
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return math.ZeroInt(), err
	}
	// Funding something already finished escrows DREAM nothing can ever pay out
	// or refund automatically.
	switch initiative.Status {
	case types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED,
		types.InitiativeStatus_INITIATIVE_STATUS_CLOSED:
		return math.ZeroInt(), fmt.Errorf("%w: initiative %d is %s", types.ErrInvalidInitiativeStatus,
			initiativeID, initiative.Status)
	}

	// DREAM lives on the member record, not in bank, so "escrow" is a lock on
	// the funder's own balance plus this record of the claim against it — the
	// same shape as a challenge stake. Locked DREAM does not decay and cannot
	// be spent, so the bounty is genuinely committed without needing an account
	// that DREAM has no way to hold.
	if err := k.LockDREAM(ctx, funder, amount); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to lock bounty: %w", err)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bounty := k.GetReviewBounty(ctx, initiativeID)
	bounty.InitiativeId = initiativeID
	bounty.Amount = bounty.Amount.Add(amount)
	bounty.Contributions = append(bounty.Contributions, types.ReviewBountyContribution{
		Funder:   funder.String(),
		Amount:   amount,
		FundedAt: sdkCtx.BlockHeight(),
	})
	if err := k.ReviewBounty.Set(ctx, initiativeID, bounty); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"review_bounty_funded",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		sdk.NewAttribute("funder", funder.String()),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("total", bounty.Amount.String()),
	))
	return bounty.Amount, nil
}

// MarkReviewBountyCommitted bars reclaim from the moment the first verdict is
// filed. Reviewers commit bond and do the reading on the strength of the
// advertised bounty, so a withdrawal after that point is a bait-and-switch.
func (k Keeper) MarkReviewBountyCommitted(ctx context.Context, initiativeID uint64) error {
	bounty, err := k.ReviewBounty.Get(ctx, initiativeID)
	if err != nil {
		return nil // nothing escrowed
	}
	if bounty.Committed {
		return nil
	}
	bounty.Committed = true
	return k.ReviewBounty.Set(ctx, initiativeID, bounty)
}

// ReclaimReviewBounty returns a funder's own unpaid contributions.
func (k Keeper) WithdrawReviewBounty(ctx context.Context, funder sdk.AccAddress, initiativeID uint64) (math.Int, error) {
	bounty, err := k.ReviewBounty.Get(ctx, initiativeID)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("%w: no bounty on initiative %d", types.ErrInvalidRequest, initiativeID)
	}
	if bounty.Committed {
		return math.ZeroInt(), fmt.Errorf("%w: a verdict has been filed on initiative %d; the bounty is committed to paying for it",
			types.ErrInvalidRequest, initiativeID)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()

	refund := math.ZeroInt()
	remaining := make([]types.ReviewBountyContribution, 0, len(bounty.Contributions))
	for _, c := range bounty.Contributions {
		matured := height >= c.FundedAt+int64(params.ReviewBountyReclaimDelay)
		if c.Funder == funder.String() && matured {
			refund = refund.Add(c.Amount)
			continue
		}
		remaining = append(remaining, c)
	}
	if !refund.IsPositive() {
		return math.ZeroInt(), fmt.Errorf("%w: nothing reclaimable for %s on initiative %d (delay is %d blocks)",
			types.ErrInvalidRequest, funder, initiativeID, params.ReviewBountyReclaimDelay)
	}

	if err := k.UnlockDREAM(ctx, funder, refund); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to release bounty: %w", err)
	}

	bounty.Amount = bounty.Amount.Sub(refund)
	bounty.Contributions = remaining
	if err := k.persistOrClearBounty(ctx, initiativeID, bounty); err != nil {
		return math.ZeroInt(), err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"review_bounty_reclaimed",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		sdk.NewAttribute("funder", funder.String()),
		sdk.NewAttribute("amount", refund.String()),
	))
	return refund, nil
}

// PayReviewBounty splits the escrowed bounty across the reviewers who filed on
// the round that resolved the initiative, and clears the record.
//
// Split per verdict filed, never per approval — the same rule as the DREAM fee,
// for the same reason.
func (k Keeper) PayReviewBounty(ctx context.Context, initiative types.Initiative) (math.Int, error) {
	bounty, err := k.ReviewBounty.Get(ctx, initiative.Id)
	if err != nil || bounty.Amount.IsNil() || !bounty.Amount.IsPositive() {
		return math.ZeroInt(), nil
	}
	reviews, err := k.GetInitiativeReviews(ctx, initiative.Id, initiative.ReviewRound)
	if err != nil {
		return math.ZeroInt(), err
	}
	if len(reviews) == 0 {
		// No verdict on the resolving round: refund rather than forfeit, or
		// funding a bounty would be a gamble on somebody else's behaviour.
		return math.ZeroInt(), k.RefundReviewBounty(ctx, initiative.Id, "no verdict filed")
	}

	share := bounty.Amount.QuoRaw(int64(len(reviews)))
	if !share.IsPositive() {
		return math.ZeroInt(), k.RefundReviewBounty(ctx, initiative.Id, "bounty too small to split")
	}

	// Draw the payout off the funders' locked balances, then mint the same
	// total to the reviewers: a net-neutral move that leaves supply unchanged
	// and never routes through the transfer tax, which exists to throttle
	// peer-to-peer gifting rather than to skim earned pay.
	if err := k.drawDownContributions(ctx, &bounty, share.MulRaw(int64(len(reviews)))); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	paid := math.ZeroInt()
	for _, r := range reviews {
		addr, aErr := sdk.AccAddressFromBech32(r.Reviewer)
		if aErr != nil {
			continue
		}
		if err := k.MintDREAM(ctx, addr, share); err != nil {
			return math.ZeroInt(), fmt.Errorf("failed to pay review bounty to %s: %w", r.Reviewer, err)
		}
		paid = paid.Add(share)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"review_bounty_paid",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
			sdk.NewAttribute("reviewer", r.Reviewer),
			sdk.NewAttribute("amount", share.String()),
		))
	}

	// Truncation dust stays escrowed only if it can still be refunded; simplest
	// and least surprising is to return it to the funders.
	bounty.Amount = bounty.Amount.Sub(paid)
	if bounty.Amount.IsPositive() {
		if err := k.refundContributionsProRata(ctx, initiative.Id, bounty, "split remainder"); err != nil {
			return paid, err
		}
		return paid, nil
	}
	return paid, k.ReviewBounty.Remove(ctx, initiative.Id)
}

// RefundReviewBounty returns every unpaid contribution to its funder. Called
// when an initiative ends without a verdict ever being filed.
func (k Keeper) RefundReviewBounty(ctx context.Context, initiativeID uint64, reason string) error {
	bounty, err := k.ReviewBounty.Get(ctx, initiativeID)
	if err != nil {
		return nil
	}
	return k.refundContributionsProRata(ctx, initiativeID, bounty, reason)
}

// refundContributionsProRata returns each funder their own remaining stake,
// scaled down if part of the escrow has already been paid out.
func (k Keeper) refundContributionsProRata(ctx context.Context, initiativeID uint64, bounty types.ReviewBounty, reason string) error {
	if bounty.Amount.IsNil() || !bounty.Amount.IsPositive() || len(bounty.Contributions) == 0 {
		return k.ReviewBounty.Remove(ctx, initiativeID)
	}
	contributed := math.ZeroInt()
	for _, c := range bounty.Contributions {
		contributed = contributed.Add(c.Amount)
	}
	if !contributed.IsPositive() {
		return k.ReviewBounty.Remove(ctx, initiativeID)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	returned := math.ZeroInt()
	for i, c := range bounty.Contributions {
		amount := c.Amount.Mul(bounty.Amount).Quo(contributed)
		if i == len(bounty.Contributions)-1 {
			amount = bounty.Amount.Sub(returned) // remainder, so no dust strands
		}
		if !amount.IsPositive() {
			continue
		}
		addr, aErr := sdk.AccAddressFromBech32(c.Funder)
		if aErr != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, addr, amount); err != nil {
			return fmt.Errorf("failed to release review bounty for %s: %w", c.Funder, err)
		}
		returned = returned.Add(amount)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"review_bounty_refunded",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("funder", c.Funder),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("reason", reason),
		))
	}
	return k.ReviewBounty.Remove(ctx, initiativeID)
}

// persistOrClearBounty writes the record back, or removes it when nothing is
// left escrowed — an empty record would otherwise linger forever.
func (k Keeper) persistOrClearBounty(ctx context.Context, initiativeID uint64, bounty types.ReviewBounty) error {
	if !bounty.Amount.IsPositive() || len(bounty.Contributions) == 0 {
		return k.ReviewBounty.Remove(ctx, initiativeID)
	}
	return k.ReviewBounty.Set(ctx, initiativeID, bounty)
}

// drawDownContributions consumes `total` from the funders' locked balances,
// oldest contribution first, unlocking and burning as it goes. The equal amount
// is minted to the reviewers by the caller, so supply is unchanged.
func (k Keeper) drawDownContributions(ctx context.Context, bounty *types.ReviewBounty, total math.Int) error {
	remaining := total
	kept := make([]types.ReviewBountyContribution, 0, len(bounty.Contributions))
	for _, c := range bounty.Contributions {
		if !remaining.IsPositive() {
			kept = append(kept, c)
			continue
		}
		take := c.Amount
		if take.GT(remaining) {
			take = remaining
		}
		addr, aErr := sdk.AccAddressFromBech32(c.Funder)
		if aErr != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, addr, take); err != nil {
			return fmt.Errorf("failed to release bounty from %s: %w", c.Funder, err)
		}
		if err := k.BurnDREAM(ctx, addr, take); err != nil {
			return fmt.Errorf("failed to draw bounty from %s: %w", c.Funder, err)
		}
		remaining = remaining.Sub(take)
		if rest := c.Amount.Sub(take); rest.IsPositive() {
			c.Amount = rest
			kept = append(kept, c)
		}
	}
	bounty.Contributions = kept
	return nil
}
