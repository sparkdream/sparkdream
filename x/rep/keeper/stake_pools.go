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

	// Content conviction and author bond stakes have no pool accounting.
	return nil
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

	return nil
}

// AccumulateMemberStakeRevenue adds revenue to a member's stake pool
func (k Keeper) AccumulateMemberStakeRevenue(ctx context.Context, memberAddr sdk.AccAddress, amount math.Int) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	revenueShare := amount.ToLegacyDec().Mul(params.MemberStakeRevenueShare).TruncateInt()

	pool, err := k.MemberStakePool.Get(ctx, memberAddr.String())
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// No stakers on this member, skip
			return nil
		}
		return err
	}

	if pool.TotalStaked.IsZero() {
		return nil
	}

	// MasterChef: accumulate reward per share unit
	rewardPerShare := revenueShare.ToLegacyDec().Quo(pool.TotalStaked.ToLegacyDec())
	pool.AccRewardPerShare = pool.AccRewardPerShare.Add(rewardPerShare)
	pool.PendingRevenue = pool.PendingRevenue.Add(revenueShare)
	pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	return k.MemberStakePool.Set(ctx, memberAddr.String(), pool)
}

// AccumulateTagStakeRevenue adds revenue to tag stake pools
func (k Keeper) AccumulateTagStakeRevenue(ctx context.Context, tags []string, totalRevenue math.Int) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Split the total tag revenue share evenly across all tags.
	// Total tag staker revenue stays at TagStakeRevenueShare regardless of tag count.
	// E.g., 3 tags with 2% share → each tag pool gets 0.66% instead of 2% each.
	perTagShare := totalRevenue.ToLegacyDec().Mul(params.TagStakeRevenueShare).QuoInt64(int64(len(tags))).TruncateInt()

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
		_ = k.TagStakePool.Set(ctx, tag, pool)
	}

	return nil
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
// truncates per staker, and is zero when no external staker holds conviction.
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

	// The parent project is needed for the full affiliate set. A missing
	// project must not silently widen the payout back to everyone, so treat the
	// lookup failure as fatal rather than falling back to a narrower test.
	project, err := k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return fmt.Errorf("failed to load project %d for initiative %d completion bonus: %w",
			initiative.ProjectId, initiativeID, err)
	}
	affiliates := k.InvitationNeighborhoodOf(ctx, InitiativeAffiliates(initiative, project)...)

	// Sum conviction over the stakers eligible for the bonus.
	externalConviction := math.LegacyZeroDec()

	type stakeConviction struct {
		stake      types.Stake
		conviction math.LegacyDec
	}
	stakeConvictions := make([]stakeConviction, 0, len(stakes))

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

		// Calculate conviction for this stake
		conviction, err := k.CalculateStakeConviction(ctx, stake, initiative.Tags)
		if err != nil {
			continue
		}

		stakeConvictions = append(stakeConvictions, stakeConviction{
			stake:      stake,
			conviction: conviction,
		})

		externalConviction = externalConviction.Add(conviction)
	}

	if externalConviction.IsZero() {
		return nil
	}

	bonusPool := k.InitiativeCompletionBonusPool(ctx, totalBudget)
	if bonusPool.IsZero() {
		return nil
	}

	// Split the bonus across stakers by conviction weight. Conviction is
	// time-weighted, so a stake placed moments before completion earns
	// approximately nothing here.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	totalMinted := math.ZeroInt()

	for _, sc := range stakeConvictions {
		if sc.conviction.IsZero() {
			continue
		}

		// Calculate this staker's share of the bonus pool based on conviction
		bonusShare := math.LegacyNewDecFromInt(bonusPool).
			Mul(sc.conviction).
			Quo(externalConviction).
			TruncateInt()

		if !bonusShare.IsPositive() {
			continue
		}

		stakerAddr, err := sdk.AccAddressFromBech32(sc.stake.Staker)
		if err != nil {
			return fmt.Errorf("invalid staker address %q on stake %d: %w", sc.stake.Staker, sc.stake.Id, err)
		}

		// Mint bonus to staker
		if err := k.MintDREAM(ctx, stakerAddr, bonusShare); err != nil {
			return fmt.Errorf("failed to mint completion bonus for staker %s: %w", sc.stake.Staker, err)
		}
		totalMinted = totalMinted.Add(bonusShare)

		// Emit event
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"initiative_completion_bonus",
				sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
				sdk.NewAttribute("stake_id", fmt.Sprintf("%d", sc.stake.Id)),
				sdk.NewAttribute("staker", sc.stake.Staker),
				sdk.NewAttribute("bonus", bonusShare.String()),
				sdk.NewAttribute("conviction", sc.conviction.String()),
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

// DistributeProjectCompletionBonus distributes 5% completion bonus to project stakers
func (k Keeper) DistributeProjectCompletionBonus(ctx context.Context, projectID uint64, finalBudget math.Int) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Calculate 5% bonus pool
	bonusPool := math.LegacyNewDecFromInt(finalBudget).
		Mul(params.ProjectCompletionBonusRate).
		TruncateInt()

	if bonusPool.IsZero() {
		return nil
	}

	// Get project stake info to get total staked
	projectInfo, err := k.GetProjectStakeInfo(ctx, projectID)
	if err != nil {
		// No stakes on this project
		return nil
	}

	if projectInfo.TotalStaked.IsZero() {
		return nil
	}

	// The bonus is paid out in full immediately, below — nothing is escrowed,
	// so nothing is accumulated into projectInfo.CompletionBonusPool. That field
	// was previously incremented here and never read back anywhere, which
	// misrepresented a deferred-claim liability that does not exist.
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_completion_bonus_allocated",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("bonus_pool", bonusPool.String()),
			sdk.NewAttribute("total_staked", projectInfo.TotalStaked.String()),
		),
	)

	// Distribute bonus to all project stakers immediately
	if err := k.distributeProjectBonusToStakers(ctx, projectID, bonusPool, projectInfo.TotalStaked); err != nil {
		return err
	}

	return nil
}

// distributeProjectBonusToStakers distributes project completion bonus to all stakers
func (k Keeper) distributeProjectBonusToStakers(ctx context.Context, projectID uint64, bonusPool math.Int, totalStaked math.Int) error {
	// Get all stakes for this project
	stakes, err := k.GetProjectStakes(ctx, projectID)
	if err != nil || len(stakes) == 0 {
		return err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	for _, stake := range stakes {
		if stake.Amount.IsZero() {
			continue
		}

		// Calculate this staker's share: (stake.Amount / totalStaked) * bonusPool
		bonusShare := math.LegacyNewDecFromInt(bonusPool).
			Mul(math.LegacyNewDecFromInt(stake.Amount)).
			Quo(math.LegacyNewDecFromInt(totalStaked)).
			TruncateInt()

		if bonusShare.GT(math.ZeroInt()) {
			stakerAddr, err := sdk.AccAddressFromBech32(stake.Staker)
			if err != nil {
				continue
			}

			// Mint bonus to staker
			if err := k.MintDREAM(ctx, stakerAddr, bonusShare); err != nil {
				continue
			}

			// Emit event
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
	}

	return nil
}
