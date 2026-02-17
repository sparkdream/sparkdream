package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Cancel(ctx context.Context, msg *types.MsgCancel) (*types.MsgCancelResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	// Get contribution
	contrib, err := k.Contribution.Get(ctx, msg.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound.Wrapf("contribution %d", msg.ContributionId)
	}

	isContributor := msg.Authority == contrib.Contributor

	// If contributor is cancelling, verify no tranche has reached BACKED or beyond
	if isContributor {
		if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED &&
			contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
			return nil, types.ErrUnauthorized.Wrapf("contribution is in %s status", contrib.Status)
		}
		if HasAnyTrancheReachedStatus(&contrib, types.TrancheStatus_TRANCHE_STATUS_BACKED) {
			return nil, types.ErrCannotCancelBacked
		}
	}
	// Operations Committee can cancel at any time — authority validation is handled
	// by the council proposal mechanism (msg.Authority is the group policy account)

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Return all stakes for active tranches
	if err := k.returnAllStakes(ctx, &contrib); err != nil {
		return nil, err
	}

	// Handle bond based on who cancelled
	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return nil, err
	}

	if contrib.BondRemaining.IsPositive() {
		if isContributor {
			// Contributor cancels before BACKED: bond returned
			if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
				return nil, err
			}
		} else {
			// Committee cancels: bond returned (not contributor's fault)
			if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
				return nil, err
			}
		}
	}

	// Return holdback if committee cancelled (not contributor's fault)
	if !isContributor && contrib.HoldbackAmount.IsPositive() {
		if err := k.repKeeper.MintDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.HoldbackAmount); err != nil {
			return nil, err
		}
	}
	// If contributor cancels, holdback doesn't exist yet (no tranche BACKED+)

	// Remove old status index
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return nil, err
	}

	// Cancel all tranches
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status != types.TrancheStatus_TRANCHE_STATUS_VERIFIED {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED

	// Save updated contribution
	if err := k.Contribution.Set(ctx, contrib.Id, contrib); err != nil {
		return nil, err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"contribution_cancelled",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("cancelled_by", msg.Authority),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgCancelResponse{}, nil
}

// returnAllStakes returns all active stakes for a contribution to their stakers.
func (k Keeper) returnAllStakes(ctx context.Context, contrib *types.Contribution) error {
	for _, tranche := range contrib.Tranches {
		if tranche.Status == types.TrancheStatus_TRANCHE_STATUS_VERIFIED ||
			tranche.Status == types.TrancheStatus_TRANCHE_STATUS_CANCELLED ||
			tranche.Status == types.TrancheStatus_TRANCHE_STATUS_FAILED {
			continue // stakes already returned
		}
		if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
			return err
		}
	}
	return nil
}

// returnTrancheStakes returns all stakes for a specific tranche to their stakers.
func (k Keeper) returnTrancheStakes(ctx context.Context, contributionID uint64, trancheID uint32) error {
	trancheKey := TrancheKey(contributionID, trancheID)

	var stakesToRemove []uint64
	err := k.StakesByTranche.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](trancheKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			stakeID := key.K2()
			stake, err := k.RevealStake.Get(ctx, stakeID)
			if err != nil {
				return true, err
			}

			// Unlock DREAM back to staker
			stakerAddr, err := k.addressCodec.StringToBytes(stake.Staker)
			if err != nil {
				return true, err
			}
			if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(stakerAddr), stake.Amount); err != nil {
				return true, err
			}

			stakesToRemove = append(stakesToRemove, stakeID)
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	// Remove stakes and indexes
	for _, stakeID := range stakesToRemove {
		stake, err := k.RevealStake.Get(ctx, stakeID)
		if err != nil {
			continue
		}
		if err := k.RevealStake.Remove(ctx, stakeID); err != nil {
			return err
		}
		if err := k.StakesByTranche.Remove(ctx, collections.Join(trancheKey, stakeID)); err != nil {
			return err
		}
		if err := k.StakesByStaker.Remove(ctx, collections.Join(stake.Staker, stakeID)); err != nil {
			return err
		}
	}

	return nil
}

// deleteTrancheVotes deletes all verification votes for a tranche (used on IMPROVE verdict).
func (k Keeper) deleteTrancheVotes(ctx context.Context, contributionID uint64, trancheID uint32) error {
	trancheKey := TrancheKey(contributionID, trancheID)

	var voteKeys []string
	err := k.VotesByTranche.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](trancheKey),
		func(key collections.Pair[string, string]) (bool, error) {
			voteKeys = append(voteKeys, key.K2())
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	for _, vk := range voteKeys {
		vote, err := k.Vote.Get(ctx, vk)
		if err != nil {
			continue
		}
		if err := k.Vote.Remove(ctx, vk); err != nil {
			return err
		}
		if err := k.VotesByTranche.Remove(ctx, collections.Join(trancheKey, vk)); err != nil {
			return err
		}
		if err := k.VotesByVoter.Remove(ctx, collections.Join(vote.Voter, vk)); err != nil {
			return err
		}
	}

	return nil
}
