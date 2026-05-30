package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AddForumRep credits per-tag forum reputation to a member. Mirrors
// AddReputation but writes to Member.ForumRepPerTag, which the trust-level
// ladder counts toward PROVISIONAL and ESTABLISHED only. Used by x/forum's
// PostConvictionStake accrual loop.
func (k Keeper) AddForumRep(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error {
	if amount.IsNil() || amount.IsZero() {
		return nil
	}
	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
	}

	if member.ForumRepPerTag == nil {
		member.ForumRepPerTag = make(map[string]string)
	}

	current := math.LegacyZeroDec()
	if s, ok := member.ForumRepPerTag[tag]; ok {
		current, _ = math.LegacyNewDecFromStr(s)
	}
	updated := current.Add(amount)
	member.ForumRepPerTag[tag] = updated.String()

	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"forum_rep_added",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("tag", tag),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("new_score", updated.String()),
		),
	)
	return nil
}

// DeductForumRep removes per-tag forum reputation from a member, floored at
// zero. Used by ExpireHiddenPosts to claw back the exact rep that a
// conviction stake produced when the underlying post is finalized as hidden.
func (k Keeper) DeductForumRep(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error {
	if amount.IsNil() || amount.IsZero() {
		return nil
	}
	if amount.IsNegative() {
		return types.ErrInvalidAmount
	}

	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return types.ErrMemberNotFound
	}
	if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
		return types.ErrMemberNotActive
	}

	if member.ForumRepPerTag == nil {
		return nil
	}

	s, ok := member.ForumRepPerTag[tag]
	if !ok {
		return nil
	}
	current, _ := math.LegacyNewDecFromStr(s)
	updated := current.Sub(amount)
	if updated.IsNegative() {
		updated = math.LegacyZeroDec()
	}
	if updated.IsZero() {
		delete(member.ForumRepPerTag, tag)
	} else {
		member.ForumRepPerTag[tag] = updated.String()
	}

	if err := k.Member.Set(ctx, memberAddr.String(), member); err != nil {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"forum_rep_deducted",
			sdk.NewAttribute("member", memberAddr.String()),
			sdk.NewAttribute("tag", tag),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("new_score", updated.String()),
		),
	)
	return nil
}

// GetForumRep returns a member's per-tag forum reputation. Zero if the
// member has none in this tag or is not registered.
func (k Keeper) GetForumRep(ctx context.Context, memberAddr sdk.AccAddress, tag string) math.LegacyDec {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return math.LegacyZeroDec()
	}
	s, ok := member.ForumRepPerTag[tag]
	if !ok {
		return math.LegacyZeroDec()
	}
	v, err := math.LegacyNewDecFromStr(s)
	if err != nil {
		return math.LegacyZeroDec()
	}
	return v
}

// SumForumRep returns the total forum reputation a member holds across all
// tags. Used by UpdateTrustLevel to count forum-earned rep toward the
// PROVISIONAL and ESTABLISHED thresholds.
func (k Keeper) SumForumRep(ctx context.Context, memberAddr sdk.AccAddress) math.LegacyDec {
	member, err := k.Member.Get(ctx, memberAddr.String())
	if err != nil {
		return math.LegacyZeroDec()
	}
	total := math.LegacyZeroDec()
	for _, s := range member.ForumRepPerTag {
		v, err := math.LegacyNewDecFromStr(s)
		if err != nil {
			continue
		}
		total = total.Add(v)
	}
	return total
}
