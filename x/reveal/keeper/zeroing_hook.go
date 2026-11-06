package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

// ZeroingHook adapts the keeper to x/rep's MemberZeroingHook.
type ZeroingHook struct{ k *Keeper }

// NewZeroingHook returns the hook app.go registers with x/rep.
func NewZeroingHook(k *Keeper) ZeroingHook { return ZeroingHook{k: k} }

// AfterMemberZeroed drops every DREAM lock record x/reveal holds for a
// zeroed member. x/rep burned the DREAM behind them with the rest of the
// member's balance. A record kept past that cannot be settled safely: while
// the zeroing stands its unlock fails, and once an overturned zeroing gives
// the member a balance back (x/rep re-mints the burn as liquid DREAM) the
// same unlock succeeds against whatever the member locked since, releasing
// an unrelated lock and paying the stake twice (REV-101). So nothing is left
// for a later unlock to find:
//
//   - The member's stakes go. On a tranche still collecting (STAKING) the
//     stake also leaves the tranche total, so DREAM that no longer exists
//     cannot back it. On a tranche already BACKED or later the total keeps
//     the stake's weight: pulling it out would change a tally in flight.
//     A vote the member already cast keeps its recorded weight.
//   - The member's own PROPOSED and IN_PROGRESS contributions are cancelled
//     (REV-102): the other stakers get their stakes back, the bond is
//     treated as burned, and the unpaid holdback is forfeited. Kept, an
//     approved contribution went on minting tranche payouts to the zeroed
//     account and MsgReject failed on the bond unlock.
//
// Each contribution is cancelled in its own cache context, so one that
// cannot be unwound (another staker's unlock fails) is logged and left for
// the council instead of blocking the rest.
func (h ZeroingHook) AfterMemberZeroed(ctx context.Context, member string, addresses []string) error {
	k := h.k
	dropped, err := k.dropZeroedStakes(ctx, addresses)
	if err != nil {
		return err
	}
	cancelled := k.cancelZeroedContributions(ctx, member, addresses)

	if dropped > 0 || cancelled > 0 {
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
			"reveal_zeroed_stakes_dropped",
			sdk.NewAttribute("member", member),
			sdk.NewAttribute("stakes_dropped", fmt.Sprintf("%d", dropped)),
			sdk.NewAttribute("contributions_cancelled", fmt.Sprintf("%d", cancelled)),
		))
	}
	return nil
}

// dropZeroedStakes removes every stake held under addresses without an
// unlock. Returns the count dropped.
func (k Keeper) dropZeroedStakes(ctx context.Context, addresses []string) (int, error) {
	type held struct {
		id    uint64
		stake types.RevealStake
	}
	var stakes []held
	for _, addr := range addresses {
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		if err := k.StakesByStaker.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			stake, err := k.RevealStake.Get(ctx, key.K2())
			if err != nil {
				// Dangling index entry.
				stake = types.RevealStake{Id: key.K2(), Staker: addr}
			}
			stakes = append(stakes, held{id: key.K2(), stake: stake})
			return false, nil
		}); err != nil {
			return 0, err
		}
	}

	for _, st := range stakes {
		if err := k.StakesByStaker.Remove(ctx, collections.Join(st.stake.Staker, st.id)); err != nil {
			return 0, err
		}
		has, err := k.RevealStake.Has(ctx, st.id)
		if err != nil {
			return 0, err
		}
		if !has {
			continue
		}
		if err := k.RevealStake.Remove(ctx, st.id); err != nil {
			return 0, err
		}
		trancheKey := TrancheKey(st.stake.ContributionId, st.stake.TrancheId)
		if err := k.StakesByTranche.Remove(ctx, collections.Join(trancheKey, st.id)); err != nil {
			return 0, err
		}
		contrib, err := k.Contribution.Get(ctx, st.stake.ContributionId)
		if err == nil {
			if tranche, tErr := GetTranche(&contrib, st.stake.TrancheId); tErr == nil &&
				tranche.Status == types.TrancheStatus_TRANCHE_STATUS_STAKING {
				tranche.DreamStaked = tranche.DreamStaked.Sub(st.stake.Amount)
				if tranche.DreamStaked.IsNegative() {
					tranche.DreamStaked = math.ZeroInt()
				}
				if err := k.saveContribution(ctx, &contrib); err != nil {
					return 0, err
				}
			}
		}
	}
	return len(stakes), nil
}

// cancelZeroedContributions cancels the PROPOSED and IN_PROGRESS
// contributions whose contributor is one of addresses. Returns the count
// cancelled.
func (k Keeper) cancelZeroedContributions(ctx context.Context, member string, addresses []string) int {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var ids []uint64
	for _, addr := range addresses {
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		if err := k.ContributionsByContributor.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			ids = append(ids, key.K2())
			return false, nil
		}); err != nil {
			sdkCtx.Logger().Error("reveal zeroing: failed to walk contributions", "member", member, "error", err)
			return 0
		}
	}
	cancelled := 0
	for _, id := range ids {
		contrib, err := k.Contribution.Get(ctx, id)
		if err != nil || (contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED &&
			contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS) {
			continue
		}
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.cancelContributionOfZeroed(cacheCtx, &contrib); err != nil {
			sdkCtx.Logger().Error("reveal zeroing: failed to cancel contribution", "contribution_id", id, "member", member, "error", err)
			continue
		}
		write()
		cancelled++
	}
	return cancelled
}

// cancelContributionOfZeroed is MsgCancel for a contribution whose
// contributor was zeroed: stakes on open tranches are returned, the bond is
// not unlocked (it was burned with the balance), and the accrued holdback,
// which was never minted, is forfeited.
func (k Keeper) cancelContributionOfZeroed(ctx context.Context, contrib *types.Contribution) error {
	if err := k.returnAllStakes(ctx, contrib); err != nil {
		return err
	}
	bond, holdback := contrib.BondRemaining, contrib.HoldbackAmount
	contrib.BondRemaining = math.ZeroInt()
	contrib.HoldbackAmount = math.ZeroInt()

	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status != types.TrancheStatus_TRANCHE_STATUS_VERIFIED {
			if err := k.deleteTrancheVotes(ctx, contrib.Id, contrib.Tranches[i].Id); err != nil {
				return err
			}
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
	if err := k.saveContribution(ctx, contrib); err != nil {
		return err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"contribution_cancelled",
		sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
		sdk.NewAttribute("cancelled_by", types.ModuleName),
		sdk.NewAttribute("reason", "contributor zeroed"),
		sdk.NewAttribute("bond_forfeited", bond.String()),
		sdk.NewAttribute("holdback_forfeited", holdback.String()),
	))
	return nil
}
