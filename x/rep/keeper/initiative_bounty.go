package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Initiative bounties: DREAM escrowed against one initiative by members who
// want the work done, paid to the assignee only if it completes.
//
// The budget is minted; a bounty moves DREAM that already exists, so it is the
// non-inflationary way to signal demand. Being contingent on completion it is
// also a route for DREAM between members, and DREAM is not meant to be
// monetized, so every rule here exists to keep that route no cheaper than a
// tip: the total is capped against the budget (every DREAM moved must ride on
// work that real, non-affiliated conviction pushed over the line), each funder
// is capped per epoch, nobody affiliated with the work may fund it, the release
// is taxed like any member-to-member transfer, and the payout counts against
// the assignee's recipient-side transfer limits.
//
// Distinct from a review bounty, which pays reviewers per verdict filed and is
// never contingent on approval.

// GetInitiativeBounty returns the escrowed bounty for an initiative, or a zero
// record when none exists.
func (k Keeper) GetInitiativeBounty(ctx context.Context, initiativeID uint64) types.InitiativeBounty {
	b, err := k.InitiativeBounty.Get(ctx, initiativeID)
	if err != nil {
		return types.InitiativeBounty{InitiativeId: initiativeID, Amount: math.ZeroInt()}
	}
	if b.Amount.IsNil() {
		b.Amount = math.ZeroInt()
	}
	return b
}

// HasInitiativeBountyContribution reports whether addr holds a live
// contribution to the initiative's bounty. A funder may not then take the work.
func (k Keeper) HasInitiativeBountyContribution(ctx context.Context, initiativeID uint64, addr string) bool {
	b, err := k.InitiativeBounty.Get(ctx, initiativeID)
	if err != nil {
		return false
	}
	for _, c := range b.Contributions {
		if c.Funder == addr {
			return true
		}
	}
	return false
}

// EscrowInitiativeBounty locks DREAM from a funder against an initiative.
func (k Keeper) EscrowInitiativeBounty(ctx context.Context, funder sdk.AccAddress, initiativeID uint64, amount math.Int) (math.Int, error) {
	if amount.IsNil() || !amount.IsPositive() {
		return math.ZeroInt(), errorsmod.Wrap(types.ErrInvalidAmount, "bounty amount must be positive")
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	if amount.LT(params.MinInitiativeBountyContribution) {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrExceedsInitiativeBountyLimit,
			"contribution %s is below the minimum %s", amount, params.MinInitiativeBountyContribution)
	}

	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return math.ZeroInt(), err
	}
	// Funding after work is submitted is paying a known person for a known
	// deliverable: a directed transfer, not a bid for work.
	switch initiative.Status {
	case types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED:
	default:
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"a bounty can only be funded on an OPEN or ASSIGNED initiative; %d is %s", initiativeID, initiative.Status)
	}
	if k.IsAffiliatedWithProject(ctx, initiative, funder.String()) {
		return math.ZeroInt(), errorsmod.Wrap(types.ErrUnauthorized,
			"the assignee, apprentice, initiative creator and project creator cannot fund its bounty")
	}

	bounty := k.GetInitiativeBounty(ctx, initiativeID)
	if uint32(len(bounty.Contributions)) >= params.MaxInitiativeBountyContributions {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrExceedsInitiativeBountyLimit,
			"initiative %d already has %d contributions", initiativeID, len(bounty.Contributions))
	}
	// The cap that bounds laundering volume: no more than the work is worth.
	ceiling := params.InitiativeBountyMaxBudgetRatio.MulInt(DerefInt(initiative.Budget)).TruncateInt()
	if bounty.Amount.Add(amount).GT(ceiling) {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrExceedsInitiativeBountyLimit,
			"bounty would exceed %s (%s of the %s budget); %s already escrowed",
			ceiling, params.InitiativeBountyMaxBudgetRatio, DerefInt(initiative.Budget), bounty.Amount)
	}

	member, err := k.Member.Get(ctx, funder.String())
	if err != nil {
		return math.ZeroInt(), types.ErrMemberNotFound
	}
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	funded := math.ZeroInt()
	if member.LastInitiativeBountyEpoch == epoch {
		funded = DerefInt(member.InitiativeBountyFundedThisEpoch)
	}
	if funded.Add(amount).GT(params.MaxInitiativeBountyPerFunderEpoch) {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrExceedsInitiativeBountyLimit,
			"%s of %s already funded this epoch", funded, params.MaxInitiativeBountyPerFunderEpoch)
	}

	// DREAM lives on the member record, so escrow is a lock on the funder's own
	// balance plus this record of the claim against it.
	if err := k.LockDREAM(ctx, funder, amount); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to lock bounty: %w", err)
	}
	// LockDREAM persisted the member; re-read before recording the epoch spend.
	member, err = k.Member.Get(ctx, funder.String())
	if err != nil {
		return math.ZeroInt(), err
	}
	member.InitiativeBountyFundedThisEpoch = PtrInt(funded.Add(amount))
	member.LastInitiativeBountyEpoch = epoch
	if err := k.Member.Set(ctx, funder.String(), member); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	bounty.InitiativeId = initiativeID
	bounty.Amount = bounty.Amount.Add(amount)
	bounty.Contributions = append(bounty.Contributions, types.InitiativeBountyContribution{
		Funder:   funder.String(),
		Amount:   amount,
		FundedAt: sdkCtx.BlockHeight(),
	})
	if err := k.InitiativeBounty.Set(ctx, initiativeID, bounty); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"initiative_bounty_funded",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		sdk.NewAttribute("funder", funder.String()),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("total", bounty.Amount.String()),
	))
	return bounty.Amount, nil
}

// WithdrawInitiativeBounty returns a funder's own matured contributions. Only
// while the initiative is OPEN: once someone is assigned they are working on
// the strength of the advertised bounty, and pulling it is a bait-and-switch.
func (k Keeper) WithdrawInitiativeBounty(ctx context.Context, funder sdk.AccAddress, initiativeID uint64) (math.Int, error) {
	bounty, err := k.InitiativeBounty.Get(ctx, initiativeID)
	if err != nil {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrInvalidRequest, "no bounty on initiative %d", initiativeID)
	}
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return math.ZeroInt(), err
	}
	if initiative.Status != types.InitiativeStatus_INITIATIVE_STATUS_OPEN {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrInvalidInitiativeStatus,
			"initiative %d is %s; a bounty can only be reclaimed while it is OPEN", initiativeID, initiative.Status)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()

	refund := math.ZeroInt()
	remaining := make([]types.InitiativeBountyContribution, 0, len(bounty.Contributions))
	for _, c := range bounty.Contributions {
		matured := height >= c.FundedAt+int64(params.InitiativeBountyReclaimDelay)
		if c.Funder == funder.String() && matured {
			refund = refund.Add(c.Amount)
			continue
		}
		remaining = append(remaining, c)
	}
	if !refund.IsPositive() {
		return math.ZeroInt(), errorsmod.Wrapf(types.ErrInvalidRequest,
			"nothing reclaimable for %s on initiative %d (delay is %d blocks)",
			funder, initiativeID, params.InitiativeBountyReclaimDelay)
	}
	if err := k.UnlockDREAM(ctx, funder, refund); err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to release bounty: %w", err)
	}

	bounty.Amount = bounty.Amount.Sub(refund)
	bounty.Contributions = remaining
	if err := k.persistOrClearInitiativeBounty(ctx, initiativeID, bounty); err != nil {
		return math.ZeroInt(), err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"initiative_bounty_reclaimed",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
		sdk.NewAttribute("funder", funder.String()),
		sdk.NewAttribute("amount", refund.String()),
	))
	return refund, nil
}

// PayInitiativeBounty releases the bounty to the assignee of a completing
// initiative and clears the record. Returns the gross amount drawn from the
// funders.
//
// The release is taxed at transfer_tax_rate, like any member choosing to move
// DREAM to another member, and the net counts against the assignee's
// recipient-side transfer limits. Whatever would exceed those limits is
// refunded to the funders untaxed; the initiative still completes.
//
// It is a balance move, not burn-and-mint: the DREAM already exists, so it must
// not count toward the epoch mint cap or the season's minted total.
func (k Keeper) PayInitiativeBounty(ctx context.Context, initiative types.Initiative) (math.Int, error) {
	bounty, err := k.InitiativeBounty.Get(ctx, initiative.Id)
	if err != nil || bounty.Amount.IsNil() || !bounty.Amount.IsPositive() {
		return math.ZeroInt(), nil
	}
	assignee, err := sdk.AccAddressFromBech32(initiative.Assignee)
	if err != nil {
		return math.ZeroInt(), k.RefundInitiativeBounty(ctx, initiative.Id, "no assignee to pay")
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	member, err := k.Member.Get(ctx, assignee.String())
	if err != nil {
		return math.ZeroInt(), k.RefundInitiativeBounty(ctx, initiative.Id, "assignee is not a member")
	}
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return math.ZeroInt(), err
	}
	headroom, err := k.TransferReceiveHeadroom(ctx, params, &member)
	if err != nil {
		return math.ZeroInt(), err
	}

	taxOf := func(gross math.Int) math.Int {
		if params.TransferTaxRate.IsZero() {
			return math.ZeroInt()
		}
		return params.TransferTaxRate.MulInt(gross).TruncateInt()
	}
	gross := bounty.Amount
	if gross.Sub(taxOf(gross)).GT(headroom) {
		// Largest gross whose net fits the headroom.
		keep := math.LegacyOneDec().Sub(params.TransferTaxRate)
		if keep.IsPositive() {
			gross = math.LegacyNewDecFromInt(headroom).Quo(keep).TruncateInt()
		} else {
			gross = headroom
		}
		if gross.GT(bounty.Amount) {
			gross = bounty.Amount
		}
		for gross.IsPositive() && gross.Sub(taxOf(gross)).GT(headroom) {
			gross = gross.SubRaw(1)
		}
	}

	if !gross.IsPositive() {
		return math.ZeroInt(), k.RefundInitiativeBounty(ctx, initiative.Id, "assignee at transfer receive limit")
	}
	tax := taxOf(gross)
	net := gross.Sub(tax)

	// Draw gross from the funders pro rata to their contributions; the rest of
	// each contribution is returned untaxed.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	drawn := math.ZeroInt()
	type draw struct {
		funder string
		take   math.Int
	}
	draws := make([]draw, 0, len(bounty.Contributions))
	for i, c := range bounty.Contributions {
		take := c.Amount.Mul(gross).Quo(bounty.Amount)
		if i == len(bounty.Contributions)-1 {
			take = gross.Sub(drawn) // remainder, so no dust strands
		}
		if take.GT(c.Amount) {
			take = c.Amount
		}
		funderAddr, aErr := sdk.AccAddressFromBech32(c.Funder)
		if aErr != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, funderAddr, c.Amount); err != nil {
			return math.ZeroInt(), fmt.Errorf("failed to release bounty from %s: %w", c.Funder, err)
		}
		if take.IsPositive() {
			fm, err := k.Member.Get(ctx, c.Funder)
			if err != nil {
				return math.ZeroInt(), err
			}
			if fm.DreamBalance.LT(take) {
				take = *fm.DreamBalance
			}
			*fm.DreamBalance = fm.DreamBalance.Sub(take)
			if err := k.Member.Set(ctx, c.Funder, fm); err != nil {
				return math.ZeroInt(), err
			}
		}
		drawn = drawn.Add(take)
		if take.IsPositive() {
			draws = append(draws, draw{funder: c.Funder, take: take})
		}
		if refund := c.Amount.Sub(take); refund.IsPositive() {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"initiative_bounty_refunded",
				sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
				sdk.NewAttribute("funder", c.Funder),
				sdk.NewAttribute("amount", refund.String()),
				sdk.NewAttribute("reason", "assignee at transfer receive limit"),
			))
		}
	}
	if !drawn.Equal(gross) {
		// Only reachable if a funder's balance had drifted below its lock.
		// Pay what was actually drawn rather than creating DREAM.
		gross = drawn
		tax = taxOf(gross)
		net = gross.Sub(tax)
	}

	// Charge the burned tax to the funders' lifetime_burned in proportion to
	// what each paid in, as TransferDREAM charges it to the sender, so the
	// member ledger and SeasonBurned move together.
	attributed := math.ZeroInt()
	for i, d := range draws {
		share := tax.Mul(d.take).Quo(gross)
		if i == len(draws)-1 {
			share = tax.Sub(attributed)
		}
		attributed = attributed.Add(share)
		if !share.IsPositive() {
			continue
		}
		fm, err := k.Member.Get(ctx, d.funder)
		if err != nil {
			return math.ZeroInt(), err
		}
		fm.LifetimeBurned = PtrInt(DerefInt(fm.LifetimeBurned).Add(share))
		if err := k.Member.Set(ctx, d.funder, fm); err != nil {
			return math.ZeroInt(), err
		}
	}

	// Re-read the assignee: a funder cannot be the assignee, but the funder
	// writes above must not be clobbered by a stale copy in any case.
	member, err = k.Member.Get(ctx, assignee.String())
	if err != nil {
		return math.ZeroInt(), err
	}
	if err := k.ApplyPendingDecay(ctx, &member); err != nil {
		return math.ZeroInt(), err
	}
	if err := k.chargeTransferReceived(ctx, params, &member, net); err != nil {
		return math.ZeroInt(), err
	}
	member.DreamBalance = PtrInt(DerefInt(member.DreamBalance).Add(net))
	if err := k.Member.Set(ctx, assignee.String(), member); err != nil {
		return math.ZeroInt(), err
	}
	if tax.IsPositive() {
		if err := k.TrackBurn(ctx, tax); err != nil {
			return math.ZeroInt(), err
		}
	}
	if err := k.InitiativeBounty.Remove(ctx, initiative.Id); err != nil {
		return math.ZeroInt(), err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"initiative_bounty_paid",
		sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiative.Id)),
		sdk.NewAttribute("assignee", initiative.Assignee),
		sdk.NewAttribute("amount", net.String()),
		sdk.NewAttribute("tax", tax.String()),
	))
	return gross, nil
}

// RefundInitiativeBounty returns every contribution to its funder in full,
// untaxed. Called on every terminal path other than completion.
func (k Keeper) RefundInitiativeBounty(ctx context.Context, initiativeID uint64, reason string) error {
	bounty, err := k.InitiativeBounty.Get(ctx, initiativeID)
	if err != nil {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, c := range bounty.Contributions {
		if !c.Amount.IsPositive() {
			continue
		}
		addr, aErr := sdk.AccAddressFromBech32(c.Funder)
		if aErr != nil {
			continue
		}
		if err := k.UnlockDREAM(ctx, addr, c.Amount); err != nil {
			return fmt.Errorf("failed to refund bounty to %s: %w", c.Funder, err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"initiative_bounty_refunded",
			sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
			sdk.NewAttribute("funder", c.Funder),
			sdk.NewAttribute("amount", c.Amount.String()),
			sdk.NewAttribute("reason", reason),
		))
	}
	return k.InitiativeBounty.Remove(ctx, initiativeID)
}

// dropInitiativeBountyContributions removes every contribution a member holds
// across all initiative bounties, without unlocking anything. Called by
// ZeroMember, which burns the member's whole balance, locked DREAM included.
func (k Keeper) dropInitiativeBountyContributions(ctx context.Context, addr string) error {
	var changed []types.InitiativeBounty
	if err := k.InitiativeBounty.Walk(ctx, nil, func(_ uint64, b types.InitiativeBounty) (bool, error) {
		kept := make([]types.InitiativeBountyContribution, 0, len(b.Contributions))
		dropped := math.ZeroInt()
		for _, c := range b.Contributions {
			if c.Funder == addr {
				dropped = dropped.Add(c.Amount)
				continue
			}
			kept = append(kept, c)
		}
		if dropped.IsPositive() || len(kept) != len(b.Contributions) {
			b.Contributions = kept
			b.Amount = b.Amount.Sub(dropped)
			changed = append(changed, b)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, b := range changed {
		if err := k.persistOrClearInitiativeBounty(ctx, b.InitiativeId, b); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) persistOrClearInitiativeBounty(ctx context.Context, initiativeID uint64, bounty types.InitiativeBounty) error {
	if !bounty.Amount.IsPositive() || len(bounty.Contributions) == 0 {
		return k.InitiativeBounty.Remove(ctx, initiativeID)
	}
	return k.InitiativeBounty.Set(ctx, initiativeID, bounty)
}
