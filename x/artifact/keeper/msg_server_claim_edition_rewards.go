package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// ClaimEditionRewards pays out what the signer's balances have accrued
// from retro funding.
func (k msgServer) ClaimEditionRewards(ctx context.Context, msg *types.MsgClaimEditionRewards) (*types.MsgClaimEditionRewardsResponse, error) {
	if _, err := k.decodeAddr("holder", msg.Holder); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.Refs), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	seen := map[[2]uint64]bool{}
	total := math.ZeroInt()
	for _, r := range msg.Refs {
		key := [2]uint64{r.ClassId, r.EditionId}
		if seen[key] {
			return nil, errorsmod.Wrapf(types.ErrInvalidBatch, "duplicate edition %d/%d", r.ClassId, r.EditionId)
		}
		seen[key] = true
		e, err := k.getEdition(ctx, r.ClassId, r.EditionId)
		if err != nil {
			return nil, err
		}
		holder, err := k.balanceHolder(ctx, r.ClassId, r.EditionId, msg.Holder)
		if err != nil {
			return nil, err
		}
		b, found, err := k.getBalance(ctx, r.ClassId, r.EditionId, holder)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, errorsmod.Wrapf(types.ErrBalanceNotFound, "no balance in edition %d/%d", r.ClassId, r.EditionId)
		}
		settle(&b, e)
		amount := b.Unclaimed
		b.Unclaimed = math.ZeroInt()
		if err := k.writeBalance(ctx, &e, b); err != nil {
			return nil, err
		}
		if err := k.refund(ctx, msg.Holder, amount); err != nil {
			return nil, err
		}
		total = total.Add(amount)
		emit(ctx, types.EventEditionRewardsClaimed, classAttr(r.ClassId), editionAttr(r.EditionId),
			sdk.NewAttribute("holder", msg.Holder), sdk.NewAttribute("amount", amount.String()))
	}
	return &types.MsgClaimEditionRewardsResponse{Claimed: total.String()}, nil
}
