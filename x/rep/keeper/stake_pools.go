package keeper

import (
	"context"
	"errors"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetMemberStakePool retrieves the stake pool for a member
func (k Keeper) GetMemberStakePool(ctx context.Context, member sdk.AccAddress) (types.MemberStakePool, error) {
	pool, err := k.MemberStakePool.Get(ctx, member.String())
	if err != nil {
		return types.MemberStakePool{}, err
	}
	return pool, nil
}

// GetTagStakePool retrieves the stake pool for a tag
func (k Keeper) GetTagStakePool(ctx context.Context, tag string) (types.TagStakePool, error) {
	pool, err := k.TagStakePool.Get(ctx, tag)
	if err != nil {
		return types.TagStakePool{}, err
	}
	return pool, nil
}

// GetProjectStakeInfo retrieves stake info for a project
func (k Keeper) GetProjectStakeInfo(ctx context.Context, projectID uint64) (types.ProjectStakeInfo, error) {
	info, err := k.ProjectStakeInfo.Get(ctx, projectID)
	if err != nil {
		return types.ProjectStakeInfo{}, err
	}
	return info, nil
}

// updateStakePoolTotals moves every denominator that governs `stake`'s rewards
// by `delta`, which is negative on withdrawal. Routing both directions through
// one dispatch is what keeps TotalStaked in lockstep with stake.Amount at every
// mutation site — the asymmetry it replaces (increment on stake, no decrement
// anywhere) permanently diluted every remaining staker, because incoming
// revenue was divided by a denominator backed by DREAM that had already left.
//
// A project stake moves two denominators: its own ProjectStakeInfo and the
// shared seasonal pool it draws rewards from.
func (k Keeper) updateStakePoolTotals(ctx context.Context, stake types.Stake, delta math.Int) error {
	if delta.IsNil() || delta.IsZero() {
		return nil
	}

	switch stake.TargetType {
	case types.StakeTargetType_STAKE_TARGET_INITIATIVE:
		return k.UpdateSeasonalPoolTotalStaked(ctx, delta)

	case types.StakeTargetType_STAKE_TARGET_PROJECT:
		if err := k.updateProjectStakeInfoTotal(ctx, stake.TargetId, delta); err != nil {
			return err
		}
		return k.UpdateSeasonalPoolTotalStaked(ctx, delta)

	case types.StakeTargetType_STAKE_TARGET_MEMBER:
		return k.updateMemberStakePoolTotal(ctx, stake.TargetIdentifier, delta)

	case types.StakeTargetType_STAKE_TARGET_TAG:
		return k.updateTagStakePoolTotal(ctx, stake.TargetIdentifier, delta)
	}

	// Content conviction stakes have no reward pool, but they do carry the
	// per-member aggregate that backs max_total_content_stake_per_member, the
	// bound on how much DREAM one member can hold in them. Keeping it in step
	// here is also what makes their staked decay land on the right ledger:
	// decayStakes routes every shrink through this function. Author bonds are
	// neither pooled nor counted: slashable escrow with its own per-item cap.
	if types.IsContentConvictionType(stake.TargetType) {
		return k.adjustMemberContentStaked(ctx, stake.Staker, delta)
	}
	return nil
}

// adjustMemberContentStaked moves a member's aggregate content-stake total by
// delta, floored at zero. Read and written directly (not via GetMember) so a
// pure counter adjustment never rides the lazy-decay write path.
func (k Keeper) adjustMemberContentStaked(ctx context.Context, staker string, delta math.Int) error {
	member, err := k.Member.Get(ctx, staker)
	if err != nil {
		// CreateStake validated membership before locking; a missing record
		// here is stale state worth surfacing, not a total to drift past.
		return fmt.Errorf("failed to load member %s for content-stake aggregate: %w", staker, err)
	}
	updated := clampPoolTotal(ctx, "content-staked/"+staker, DerefInt(member.ContentStakedDream), delta)
	member.ContentStakedDream = PtrInt(updated)
	return k.Member.Set(ctx, staker, member)
}

// clampPoolTotal applies delta to a denominator, floored at zero. A negative
// result means a decrement site ran without a matching increment; that is a bug
// worth surfacing, but failing the transaction would strand the staker's DREAM,
// so the total is floored and the discrepancy logged.
func clampPoolTotal(ctx context.Context, label string, current, delta math.Int) math.Int {
	updated := current.Add(delta)
	if updated.IsNegative() {
		sdk.UnwrapSDKContext(ctx).Logger().Error("stake pool total would go negative; clamping to zero",
			"pool", label, "current", current.String(), "delta", delta.String())
		return math.ZeroInt()
	}
	return updated
}

// updateMemberStakePoolTotal moves a member pool's staked denominator.
func (k Keeper) updateMemberStakePoolTotal(ctx context.Context, memberAddr string, delta math.Int) error {
	pool, err := k.MemberStakePool.Get(ctx, memberAddr)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Create new pool
			pool = types.MemberStakePool{
				Member:            memberAddr,
				TotalStaked:       math.ZeroInt(),
				PendingRevenue:    math.ZeroInt(),
				AccRewardPerShare: math.LegacyZeroDec(),
				LastUpdated:       sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
			}
		} else {
			return err
		}
	}

	pool.TotalStaked = clampPoolTotal(ctx, "member/"+memberAddr, pool.TotalStaked, delta)
	pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	return k.MemberStakePool.Set(ctx, memberAddr, pool)
}

// updateTagStakePoolTotal moves a tag pool's staked denominator.
func (k Keeper) updateTagStakePoolTotal(ctx context.Context, tag string, delta math.Int) error {
	pool, err := k.TagStakePool.Get(ctx, tag)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Create new pool
			pool = types.TagStakePool{
				Tag:               tag,
				TotalStaked:       math.ZeroInt(),
				AccRewardPerShare: math.LegacyZeroDec(),
				LastUpdated:       sdk.UnwrapSDKContext(ctx).BlockTime().Unix(),
			}
		} else {
			return err
		}
	}

	pool.TotalStaked = clampPoolTotal(ctx, "tag/"+tag, pool.TotalStaked, delta)
	pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	return k.TagStakePool.Set(ctx, tag, pool)
}

// updateProjectStakeInfoTotal moves a project's staked denominator.
func (k Keeper) updateProjectStakeInfoTotal(ctx context.Context, projectID uint64, delta math.Int) error {
	info, err := k.ProjectStakeInfo.Get(ctx, projectID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Create new project stake info
			info = types.ProjectStakeInfo{
				ProjectId:           projectID,
				TotalStaked:         math.ZeroInt(),
				CompletionBonusPool: math.ZeroInt(),
			}
		} else {
			return err
		}
	}

	info.TotalStaked = clampPoolTotal(ctx, fmt.Sprintf("project/%d", projectID), info.TotalStaked, delta)

	return k.ProjectStakeInfo.Set(ctx, projectID, info)
}

// ReconcileStakePoolTotals recomputes every staked denominator — the member,
// tag and project pools plus the shared seasonal pool — by summing the live
// stakes that back them, and writes back any that disagree.
//
// This is the repair counterpart to updateStakePoolTotals: it is O(all stakes)
// and is intended for genesis import, where it heals state written before the
// decrement paths existed, rather than for the hot path.
func (k Keeper) ReconcileStakePoolTotals(ctx context.Context) error {
	memberTotals := map[string]math.Int{}
	tagTotals := map[string]math.Int{}
	projectTotals := map[uint64]math.Int{}
	contentTotals := map[string]math.Int{}
	seasonalTotal := math.ZeroInt()

	addTo := func(m map[string]math.Int, key string, amount math.Int) {
		if cur, ok := m[key]; ok {
			m[key] = cur.Add(amount)
			return
		}
		m[key] = amount
	}

	err := k.Stake.Walk(ctx, nil, func(_ uint64, stake types.Stake) (bool, error) {
		if stake.Amount.IsNil() || !stake.Amount.IsPositive() {
			return false, nil
		}
		switch stake.TargetType {
		case types.StakeTargetType_STAKE_TARGET_INITIATIVE:
			seasonalTotal = seasonalTotal.Add(stake.Amount)
		case types.StakeTargetType_STAKE_TARGET_PROJECT:
			seasonalTotal = seasonalTotal.Add(stake.Amount)
			if cur, ok := projectTotals[stake.TargetId]; ok {
				projectTotals[stake.TargetId] = cur.Add(stake.Amount)
			} else {
				projectTotals[stake.TargetId] = stake.Amount
			}
		case types.StakeTargetType_STAKE_TARGET_MEMBER:
			addTo(memberTotals, stake.TargetIdentifier, stake.Amount)
		case types.StakeTargetType_STAKE_TARGET_TAG:
			addTo(tagTotals, stake.TargetIdentifier, stake.Amount)
		default:
			// Content conviction stakes back the per-member aggregate capped
			// by max_total_content_stake_per_member. Author bonds are escrow
			// and are not counted.
			if types.IsContentConvictionType(stake.TargetType) {
				addTo(contentTotals, stake.Staker, stake.Amount)
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk stakes for pool reconciliation: %w", err)
	}

	if err := k.setSeasonalPoolTotalStaked(ctx, seasonalTotal); err != nil {
		return fmt.Errorf("failed to reconcile seasonal pool total staked: %w", err)
	}

	// Existing pools not represented in the live stake set are zeroed rather
	// than left stale — a pool with no backing stakes must not keep dividing
	// incoming revenue by a phantom denominator.
	if err := k.MemberStakePool.Walk(ctx, nil, func(addr string, pool types.MemberStakePool) (bool, error) {
		want, ok := memberTotals[addr]
		if !ok {
			want = math.ZeroInt()
		}
		delete(memberTotals, addr)
		if pool.TotalStaked.Equal(want) {
			return false, nil
		}
		pool.TotalStaked = want
		return false, k.MemberStakePool.Set(ctx, addr, pool)
	}); err != nil {
		return fmt.Errorf("failed to reconcile member stake pools: %w", err)
	}
	for addr, want := range memberTotals {
		if err := k.updateMemberStakePoolTotal(ctx, addr, want); err != nil {
			return fmt.Errorf("failed to create member stake pool for %s: %w", addr, err)
		}
	}

	if err := k.TagStakePool.Walk(ctx, nil, func(tag string, pool types.TagStakePool) (bool, error) {
		want, ok := tagTotals[tag]
		if !ok {
			want = math.ZeroInt()
		}
		delete(tagTotals, tag)
		if pool.TotalStaked.Equal(want) {
			return false, nil
		}
		pool.TotalStaked = want
		return false, k.TagStakePool.Set(ctx, tag, pool)
	}); err != nil {
		return fmt.Errorf("failed to reconcile tag stake pools: %w", err)
	}
	for tag, want := range tagTotals {
		if err := k.updateTagStakePoolTotal(ctx, tag, want); err != nil {
			return fmt.Errorf("failed to create tag stake pool for %s: %w", tag, err)
		}
	}

	if err := k.ProjectStakeInfo.Walk(ctx, nil, func(id uint64, info types.ProjectStakeInfo) (bool, error) {
		want, ok := projectTotals[id]
		if !ok {
			want = math.ZeroInt()
		}
		delete(projectTotals, id)
		if info.TotalStaked.Equal(want) {
			return false, nil
		}
		info.TotalStaked = want
		return false, k.ProjectStakeInfo.Set(ctx, id, info)
	}); err != nil {
		return fmt.Errorf("failed to reconcile project stake info: %w", err)
	}
	for id, want := range projectTotals {
		if err := k.updateProjectStakeInfoTotal(ctx, id, want); err != nil {
			return fmt.Errorf("failed to create project stake info for %d: %w", id, err)
		}
	}

	// Rebuild every member's aggregate content-stake total from the live
	// stakes — the same heal-the-drift contract as the pools above, including
	// zeroing aggregates whose backing stakes are gone. Also backfills nil on
	// records written before the field existed.
	if err := k.Member.Walk(ctx, nil, func(addr string, member types.Member) (bool, error) {
		want, ok := contentTotals[addr]
		if !ok {
			// A zero-value math.Int carries a nil big.Int and panics on
			// comparison; absent means "holds no content stakes", i.e. zero.
			want = math.ZeroInt()
		}
		if DerefInt(member.ContentStakedDream).Equal(want) {
			return false, nil
		}
		member.ContentStakedDream = PtrInt(want)
		return false, k.Member.Set(ctx, addr, member)
	}); err != nil {
		return fmt.Errorf("failed to reconcile member content-stake aggregates: %w", err)
	}

	return nil
}

// AccumulateMemberStakeRevenue adds revenue to a member's stake pool and
// returns the DREAM actually accrued to it. Zero when there is no pool or
// nothing staked to accrue against — the caller (CompleteInitiative) counts
// the returned figure against the per-season initiative reward cap, so it has
// to be the real accrual, not the notional share.
func (k Keeper) AccumulateMemberStakeRevenue(ctx context.Context, memberAddr sdk.AccAddress, amount math.Int) (math.Int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}

	revenueShare := amount.ToLegacyDec().Mul(params.MemberStakeRevenueShare).TruncateInt()

	pool, err := k.MemberStakePool.Get(ctx, memberAddr.String())
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// No stakers on this member, skip
			return math.ZeroInt(), nil
		}
		return math.ZeroInt(), err
	}

	if pool.TotalStaked.IsZero() {
		return math.ZeroInt(), nil
	}

	// MasterChef: accumulate reward per share unit
	rewardPerShare := revenueShare.ToLegacyDec().Quo(pool.TotalStaked.ToLegacyDec())
	pool.AccRewardPerShare = pool.AccRewardPerShare.Add(rewardPerShare)
	pool.PendingRevenue = pool.PendingRevenue.Add(revenueShare)
	pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	if err := k.MemberStakePool.Set(ctx, memberAddr.String(), pool); err != nil {
		return math.ZeroInt(), err
	}
	return revenueShare, nil
}

// AccumulateTagStakeRevenue adds revenue to tag stake pools and returns the
// DREAM actually accrued across them (zero for tags with no pool or no stake —
// the same real-accrual contract as AccumulateMemberStakeRevenue).
func (k Keeper) AccumulateTagStakeRevenue(ctx context.Context, tags []string, totalRevenue math.Int) (math.Int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}

	// Split the total tag revenue share evenly across all tags.
	// Total tag staker revenue stays at TagStakeRevenueShare regardless of tag count.
	// E.g., 3 tags with 2% share → each tag pool gets 0.66% instead of 2% each.
	perTagShare := totalRevenue.ToLegacyDec().Mul(params.TagStakeRevenueShare).QuoInt64(int64(len(tags))).TruncateInt()

	accrued := math.ZeroInt()
	for _, tag := range tags {
		pool, err := k.TagStakePool.Get(ctx, tag)
		if err != nil {
			if errors.Is(err, collections.ErrNotFound) {
				continue // No stakers on this tag
			}
			continue // Skip on error
		}

		if pool.TotalStaked.IsZero() {
			continue
		}

		rewardPerShare := perTagShare.ToLegacyDec().Quo(pool.TotalStaked.ToLegacyDec())
		pool.AccRewardPerShare = pool.AccRewardPerShare.Add(rewardPerShare)
		pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
		if err := k.TagStakePool.Set(ctx, tag, pool); err != nil {
			continue
		}
		accrued = accrued.Add(perTagShare)
	}

	return accrued, nil
}

// DistributeInitiativeCompletionBonus distributes a conviction-based bonus to an
// initiative's *external* stakers.
//
// The bonus rewards vouching, so it is paid only to stakers at arm's length from
// the work — the same independence test the external-conviction floor uses, via
// InitiativeAffiliates and the invitation-graph hop in IsStakerExternal. It used
// to exclude the assignee and apprentice only, which made this the third and
// last place in the module that defined affiliation differently from the other
// two: the initiative's own author and the parent project's creator were paid as
// though they were independent backers, on top of the completer share the
// assignee already receives. Insiders staking on their own commission are not
// vouching for it, and paying them to complete it compounds the fact that the
// bonus is paid on completion and on no other outcome.
//
// Their principal is untouched — stakes are settled and returned by
// CompleteInitiative regardless. Only this extra mint is withheld, and the
// withheld share is never minted rather than being redistributed.
//
// Must be called while the stake records are still live: it recomputes each
// staker's time-weighted conviction from stake.created_at, so it has nothing to
// weight by once CompleteInitiative's payout loop has deleted them.
// InitiativeHasStakes reports whether an initiative has any stake at all.
//
// Used by the completion mint gate to skip projecting a staker bonus for an
// initiative that cannot pay one — over-projecting there would refuse
// completions near the season cap for a payout that was never going to happen.
func (k Keeper) InitiativeHasStakes(ctx context.Context, initiativeID uint64) (bool, error) {
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil {
		return false, err
	}
	return len(stakes) > 0, nil
}

// InitiativeCompletionBonusPool is the DREAM external stakers share on
// completion, as a fraction of the initiative budget. Tunable, and the mirror
// of the project-side project_completion_bonus_rate — it was a hardcoded 1/10
// divisor while the project equivalent was already a param.
//
// Factored out so CompleteInitiative can count it against the per-season mint
// cap before minting rather than after. It is an upper bound: the actual payout
// truncates per staker, is capped again at a multiple of the external stake by
// CappedInitiativeCompletionBonusPool, and is zero when no external staker
// holds conviction.
func (k Keeper) InitiativeCompletionBonusPool(ctx context.Context, totalBudget math.Int) math.Int {
	bonusRate := math.LegacyNewDecWithPrec(1, 1) // 0.1, the shipped default
	if params, pErr := k.Params.Get(ctx); pErr == nil && !params.InitiativeCompletionBonusRate.IsNil() {
		bonusRate = params.InitiativeCompletionBonusRate
	}
	if bonusRate.IsNil() || !bonusRate.IsPositive() {
		return math.ZeroInt()
	}
	return math.LegacyNewDecFromInt(totalBudget).Mul(bonusRate).TruncateInt()
}

// CappedInitiativeCompletionBonusPool is the bonus actually paid:
//
//	min(initiative_completion_bonus_rate * budget,
//	    max_completion_bonus_stake_multiple * external_stake)
//
// The budget term alone prices the bonus off the work, not off the risk taken
// to back it, and those two come apart as the staker count grows. Clearing the
// conviction gate costs conviction_per_dream^2 * budget / N in total across N
// stakers, because required_conviction scales with sqrt(budget) while each
// staker supplies sqrt(stake) — so the same fixed share of budget is a 7.5x
// return on stake at three stakers, 25x at ten, 62.5x at twenty-five, on any
// initiative that completes. Capping against capital at risk holds the return
// at max_completion_bonus_stake_multiple no matter how many stakers split it,
// and restores the ~4% stake-to-budget ratio the conviction formula was
// designed around: below roughly bonus_rate/multiple of the budget staked, the
// stake term binds and the bonus scales down with the capital behind it.
//
// A zero or unset multiple disables the bonus rather than uncapping it — a
// param that has never been written is not a decision to pay without limit.
func (k Keeper) CappedInitiativeCompletionBonusPool(
	ctx context.Context,
	params types.Params,
	totalBudget math.Int,
	externalStake math.Int,
) math.Int {
	pool := k.InitiativeCompletionBonusPool(ctx, totalBudget)
	if !pool.IsPositive() {
		return math.ZeroInt()
	}
	multiple := params.MaxCompletionBonusStakeMultiple
	if multiple.IsNil() || !multiple.IsPositive() {
		return math.ZeroInt()
	}
	if externalStake.IsNil() || !externalStake.IsPositive() {
		return math.ZeroInt()
	}
	if stakeCap := multiple.MulInt(externalStake).TruncateInt(); stakeCap.LT(pool) {
		return stakeCap
	}
	return pool
}

func (k Keeper) DistributeInitiativeCompletionBonus(ctx context.Context, initiativeID uint64, totalBudget math.Int) error {
	// Get initiative to check assignee and challenger
	initiative, err := k.GetInitiative(ctx, initiativeID)
	if err != nil {
		return err
	}

	// Get all stakes for this initiative
	stakes, err := k.GetInitiativeStakes(ctx, initiativeID)
	if err != nil || len(stakes) == 0 {
		return err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// The parent project is needed for the full affiliate set. A missing
	// project must not silently widen the payout back to everyone, so treat the
	// lookup failure as fatal rather than falling back to a narrower test.
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return fmt.Errorf("failed to load project %d for initiative %d completion bonus: %w",
			initiative.ProjectId, initiativeID, err)
	}
	affiliates := k.InvitationNeighborhoodOf(ctx, InitiativeAffiliates(initiative, project)...)

	// Accumulate RAW (pre-sqrt) conviction per staker, exactly as the
	// completion gate does in updateInitiativeConvictionWithStakes. Taking the
	// sqrt per stake record instead — which is what this function used to do —
	// pays a member who splits one position across k tranches sqrt(k) times as
	// much for the same capital: ten tranches of 0.049 DREAM weigh 2,214 where
	// one 0.49 DREAM stake weighs 700. The gate has guarded against exactly
	// that since it was written; the payout beside it did not, so the exploit
	// the gate refuses to reward was rewarded one function later.
	type stakerPosition struct {
		rawConviction math.LegacyDec
		principal     math.Int
	}
	positions := make(map[string]*stakerPosition, len(stakes))
	order := make([]string, 0, len(stakes)) // deterministic payout order

	// One staker can hold several stakes; the independence test is per address,
	// so cache it rather than re-walking the invitation graph for each.
	isExternal := make(map[string]bool, len(stakes))

	for _, stake := range stakes {
		external, seen := isExternal[stake.Staker]
		if !seen {
			external = k.IsStakerExternalTo(ctx, stake.Staker, affiliates)
			isExternal[stake.Staker] = external
		}
		if !external {
			continue
		}

		// Calculate raw conviction for this stake
		rawConviction, err := k.CalculateRawStakeConviction(ctx, stake, initiative.Tags)
		if err != nil {
			continue
		}

		pos, ok := positions[stake.Staker]
		if !ok {
			pos = &stakerPosition{rawConviction: math.LegacyZeroDec(), principal: math.ZeroInt()}
			positions[stake.Staker] = pos
			order = append(order, stake.Staker)
		}
		pos.rawConviction = pos.rawConviction.Add(rawConviction)
		if !stake.Amount.IsNil() && stake.Amount.IsPositive() {
			pos.principal = pos.principal.Add(stake.Amount)
		}
	}

	// Dampen and cap each staker's aggregate the way the gate does, so the
	// weight that earns the bonus is the same number that unlocked the budget.
	maxPerMember := DerefDec(initiative.RequiredConviction).Mul(params.MaxConvictionSharePerMember)

	type stakerShare struct {
		staker     string
		conviction math.LegacyDec
	}
	shares := make([]stakerShare, 0, len(order))
	externalConviction := math.LegacyZeroDec()
	externalStake := math.ZeroInt()

	for _, staker := range order {
		pos := positions[staker]
		dampened, err := pos.rawConviction.ApproxSqrt()
		if err != nil {
			continue
		}
		if maxPerMember.IsPositive() && dampened.GT(maxPerMember) {
			dampened = maxPerMember
		}
		if !dampened.IsPositive() {
			continue
		}
		shares = append(shares, stakerShare{staker: staker, conviction: dampened})
		externalConviction = externalConviction.Add(dampened)
		externalStake = externalStake.Add(pos.principal)
	}

	if externalConviction.IsZero() {
		return nil
	}

	bonusPool := k.CappedInitiativeCompletionBonusPool(ctx, params, totalBudget, externalStake)
	if !bonusPool.IsPositive() {
		return nil
	}

	// Split the bonus across stakers by conviction weight. Conviction is
	// time-weighted, so a stake placed moments before completion earns
	// approximately nothing here.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	totalMinted := math.ZeroInt()

	for _, sh := range shares {
		// Calculate this staker's share of the bonus pool based on conviction
		bonusShare := math.LegacyNewDecFromInt(bonusPool).
			Mul(sh.conviction).
			Quo(externalConviction).
			TruncateInt()

		if !bonusShare.IsPositive() {
			continue
		}

		stakerAddr, err := sdk.AccAddressFromBech32(sh.staker)
		if err != nil {
			return fmt.Errorf("invalid staker address %q on initiative %d: %w", sh.staker, initiativeID, err)
		}

		// Mint bonus to staker
		if err := k.MintDREAM(ctx, stakerAddr, bonusShare); err != nil {
			return fmt.Errorf("failed to mint completion bonus for staker %s: %w", sh.staker, err)
		}
		totalMinted = totalMinted.Add(bonusShare)

		// Emit event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"initiative_completion_bonus",
				sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
				sdk.NewAttribute("staker", sh.staker),
				sdk.NewAttribute("bonus", bonusShare.String()),
				sdk.NewAttribute("conviction", sh.conviction.String()),
				sdk.NewAttribute("stake", positions[sh.staker].principal.String()),
			),
		)
	}

	// The bonus is freshly minted DREAM sourced from the initiative budget, so
	// it belongs under the same per-season cap as the completer and treasury
	// shares that CompleteInitiative already tracks.
	if totalMinted.IsPositive() {
		if err := k.TrackInitiativeRewardMint(ctx, totalMinted); err != nil {
			return fmt.Errorf("failed to track initiative completion bonus mint: %w", err)
		}
	}

	return nil
}

// DistributeProjectCompletionBonus distributes the project completion bonus
// to the project's EXTERNAL stakers, and returns the DREAM actually minted so
// the caller can account it against the per-season cap.
//
// This is the project-side mirror of CappedInitiativeCompletionBonusPool plus
// DistributeInitiativeCompletionBonus, and it now carries the same three
// hardenings the initiative side already had — none of which it had before:
//
//  1. Stake-multiple cap. The bonus is bounded at
//     max_completion_bonus_stake_multiple x totalStaked. Priced off the
//     budget alone, a 0.001 DREAM dust stake on a PROPOSED project captured
//     the entire 5% of a completing project's spent budget — a ~500,000x
//     return on the min stake, exactly the exploit class the multiple was
//     introduced to kill on initiatives.
//  2. External filter. The project creator (and their invitation
//     neighborhood) is excluded: insiders staking on their own project are
//     not vouching for it, the same arm's-length test the initiative bonus
//     applies. The withheld share is never minted, not redistributed.
//  3. Season-cap accounting. The bonus is clamped to the headroom left under
//     max_initiative_rewards_per_season and the minted total is returned for
//     TrackInitiativeRewardMint — project completions used to mint entirely
//     outside the cap that initiative completions are gated by.
//
// Unlike the initiative gate (which refuses the completion), cap pressure here
// clamps the bonus rather than failing the terminal transition: CompleteProject
// settles stakes and flips the project COMPLETED, and a payout cap should not
// be able to wedge a terminal state transition.
//
// A zero or unset MaxCompletionBonusStakeMultiple disables the bonus rather
// than uncapping it, mirroring the initiative side.
func (k Keeper) DistributeProjectCompletionBonus(ctx context.Context, projectID uint64, finalBudget math.Int) (math.Int, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}

	project, err := k.GetProject(ctx, projectID)
	if err != nil {
		return math.ZeroInt(), fmt.Errorf("failed to load project %d for completion bonus: %w", projectID, err)
	}

	projectInfo, err := k.GetProjectStakeInfo(ctx, projectID)
	if err != nil {
		// No stakes on this project
		return math.ZeroInt(), nil
	}
	if projectInfo.TotalStaked.IsZero() {
		return math.ZeroInt(), nil
	}

	// Cap 1: rate on the budget actually spent.
	bonusPool := math.LegacyNewDecFromInt(finalBudget).
		Mul(params.ProjectCompletionBonusRate).
		TruncateInt()
	if !bonusPool.IsPositive() {
		return math.ZeroInt(), nil
	}

	// Cap 2: multiple of the DREAM actually staked behind the project.
	multiple := params.MaxCompletionBonusStakeMultiple
	if multiple.IsNil() || !multiple.IsPositive() {
		return math.ZeroInt(), nil
	}
	if stakeCap := multiple.MulInt(projectInfo.TotalStaked).TruncateInt(); stakeCap.LT(bonusPool) {
		bonusPool = stakeCap
	}
	if !bonusPool.IsPositive() {
		return math.ZeroInt(), nil
	}

	// Cap 3: headroom under the per-season initiative reward cap.
	seasonMinted, err := k.GetSeasonInitiativeRewardsMinted(ctx)
	if err != nil {
		return math.ZeroInt(), err
	}
	headroom := params.MaxInitiativeRewardsPerSeason.Sub(seasonMinted)
	if !headroom.IsPositive() {
		headroom = math.ZeroInt()
	}
	if headroom.LT(bonusPool) {
		bonusPool = headroom
	}
	if !bonusPool.IsPositive() {
		return math.ZeroInt(), nil
	}

	stakes, err := k.GetProjectStakes(ctx, projectID)
	if err != nil || len(stakes) == 0 {
		return math.ZeroInt(), err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_completion_bonus_allocated",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("bonus_pool", bonusPool.String()),
			sdk.NewAttribute("total_staked", projectInfo.TotalStaked.String()),
		),
	)

	// External stakers only, pro-rata by principal. No conviction weighting:
	// the project bonus prices breadth of backing, and the stake-multiple cap
	// above is what bounds the return on a late or tiny position.
	neighborhood := k.InvitationNeighborhoodOf(ctx, project.Creator)

	totalMinted := math.ZeroInt()
	for _, stake := range stakes {
		if stake.Amount.IsNil() || !stake.Amount.IsPositive() {
			continue
		}
		if !k.IsStakerExternalTo(ctx, stake.Staker, neighborhood) {
			continue
		}

		bonusShare := math.LegacyNewDecFromInt(bonusPool).
			Mul(math.LegacyNewDecFromInt(stake.Amount)).
			Quo(math.LegacyNewDecFromInt(projectInfo.TotalStaked)).
			TruncateInt()
		if !bonusShare.IsPositive() {
			continue
		}

		stakerAddr, err := sdk.AccAddressFromBech32(stake.Staker)
		if err != nil {
			continue
		}
		if err := k.MintDREAM(ctx, stakerAddr, bonusShare); err != nil {
			// Same containment as the old helper: one failed payout must not
			// void the others or the project's terminal transition.
			sdkCtx.Logger().Error("failed to mint project completion bonus",
				"project_id", projectID, "staker", stake.Staker, "error", err)
			continue
		}
		totalMinted = totalMinted.Add(bonusShare)

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"project_completion_bonus_distributed",
				sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
				sdk.NewAttribute("stake_id", fmt.Sprintf("%d", stake.Id)),
				sdk.NewAttribute("staker", stake.Staker),
				sdk.NewAttribute("bonus", bonusShare.String()),
				sdk.NewAttribute("stake_amount", stake.Amount.String()),
			),
		)
	}

	return totalMinted, nil
}
