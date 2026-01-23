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

// updateMemberStakePoolOnStake updates member pool when stake is added
func (k Keeper) updateMemberStakePoolOnStake(ctx context.Context, memberAddr string, amount math.Int) error {
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

	pool.TotalStaked = pool.TotalStaked.Add(amount)
	pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	return k.MemberStakePool.Set(ctx, memberAddr, pool)
}

// updateTagStakePoolOnStake updates tag pool when stake is added
func (k Keeper) updateTagStakePoolOnStake(ctx context.Context, tag string, amount math.Int) error {
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

	pool.TotalStaked = pool.TotalStaked.Add(amount)
	pool.LastUpdated = sdk.UnwrapSDKContext(ctx).BlockTime().Unix()

	return k.TagStakePool.Set(ctx, tag, pool)
}

// updateProjectStakeInfoOnStake updates project info when stake is added
func (k Keeper) updateProjectStakeInfoOnStake(ctx context.Context, projectID uint64, amount math.Int) error {
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

	info.TotalStaked = info.TotalStaked.Add(amount)

	return k.ProjectStakeInfo.Set(ctx, projectID, info)
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

	perTagShare := totalRevenue.ToLegacyDec().Mul(params.TagStakeRevenueShare).TruncateInt()

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

// DistributeInitiativeCompletionBonus distributes conviction-based bonus to initiative stakers
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

	// Calculate total conviction and separate external vs internal stakers
	totalConviction := math.LegacyZeroDec()
	externalConviction := math.LegacyZeroDec()
	internalConviction := math.LegacyZeroDec()

	type stakeConviction struct {
		stake      types.Stake
		conviction math.LegacyDec
		isExternal bool
	}
	stakeConvictions := make([]stakeConviction, 0, len(stakes))

	for _, stake := range stakes {
		// Calculate conviction for this stake
		conviction, err := k.CalculateStakeConviction(ctx, stake, initiative.Tags)
		if err != nil {
			continue
		}

		// External stakers are those who are not the assignee or apprentice
		isExternal := stake.Staker != initiative.Assignee && stake.Staker != initiative.Apprentice

		stakeConvictions = append(stakeConvictions, stakeConviction{
			stake:      stake,
			conviction: conviction,
			isExternal: isExternal,
		})

		totalConviction = totalConviction.Add(conviction)
		if isExternal {
			externalConviction = externalConviction.Add(conviction)
		} else {
			internalConviction = internalConviction.Add(conviction)
		}
	}

	if totalConviction.IsZero() {
		return nil
	}

	// Calculate bonus pool (10% of budget)
	bonusPool := math.LegacyNewDecFromInt(totalBudget).QuoInt64(10).TruncateInt()

	if bonusPool.IsZero() {
		return nil
	}

	// Split bonus: external stakers get their proportional share
	// If external conviction >= 50%, they get full proportional distribution
	// Otherwise, distribute based on conviction weight
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	for _, sc := range stakeConvictions {
		if sc.conviction.IsZero() {
			continue
		}

		// Calculate this staker's share of the bonus pool based on conviction
		bonusShare := math.LegacyNewDecFromInt(bonusPool).
			Mul(sc.conviction).
			Quo(totalConviction).
			TruncateInt()

		if bonusShare.GT(math.ZeroInt()) {
			stakerAddr, err := sdk.AccAddressFromBech32(sc.stake.Staker)
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
					"initiative_completion_bonus",
					sdk.NewAttribute("initiative_id", fmt.Sprintf("%d", initiativeID)),
					sdk.NewAttribute("stake_id", fmt.Sprintf("%d", sc.stake.Id)),
					sdk.NewAttribute("staker", sc.stake.Staker),
					sdk.NewAttribute("bonus", bonusShare.String()),
					sdk.NewAttribute("conviction", sc.conviction.String()),
					sdk.NewAttribute("is_external", fmt.Sprintf("%t", sc.isExternal)),
				),
			)
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

	// Add bonus to completion bonus pool
	projectInfo.CompletionBonusPool = projectInfo.CompletionBonusPool.Add(bonusPool)

	// Update AccRewardPerShare for the project pool (similar to MasterChef)
	// This allows stakers to claim their share proportionally
	bonusPerShare := math.LegacyNewDecFromInt(bonusPool).
		Quo(math.LegacyNewDecFromInt(projectInfo.TotalStaked))

	// We'll track this in the completion bonus pool
	// Individual stakers will get their share when they unstake or claim

	if err := k.ProjectStakeInfo.Set(ctx, projectID, projectInfo); err != nil {
		return err
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"project_completion_bonus_allocated",
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", projectID)),
			sdk.NewAttribute("bonus_pool", bonusPool.String()),
			sdk.NewAttribute("total_staked", projectInfo.TotalStaked.String()),
			sdk.NewAttribute("bonus_per_share", bonusPerShare.String()),
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
