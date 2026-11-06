package keeper

import (
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

// RegisterInvariants registers the x/reveal invariants (run by app's
// TestInvariants; x/crisis is not wired).
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "contribution-indexes", ContributionIndexesInvariant(k))
	ir.RegisterRoute(types.ModuleName, "deadline-index", DeadlineIndexInvariant(k))
}

// ContributionIndexesInvariant: ContributionsByStatus and
// ContributionsByContributor agree with the Contribution map in both
// directions.
func ContributionIndexesInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		contributions := 0
		if err := k.Contribution.Walk(ctx, nil, func(id uint64, c types.Contribution) (bool, error) {
			contributions++
			if has, _ := k.ContributionsByStatus.Has(ctx, collections.Join(int32(c.Status), id)); !has {
				problems = append(problems, fmt.Sprintf("contribution %d (%s) missing from ContributionsByStatus", id, c.Status))
			}
			if has, _ := k.ContributionsByContributor.Has(ctx, collections.Join(c.Contributor, id)); !has {
				problems = append(problems, fmt.Sprintf("contribution %d missing from ContributionsByContributor", id))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Contribution.Walk failed: %v", err), true
		}
		byStatus := 0
		if err := k.ContributionsByStatus.Walk(ctx, nil, func(key collections.Pair[int32, uint64]) (bool, error) {
			byStatus++
			if c, err := k.Contribution.Get(ctx, key.K2()); err != nil || int32(c.Status) != key.K1() {
				problems = append(problems, fmt.Sprintf("ContributionsByStatus (%d,%d) dangling", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ContributionsByStatus.Walk failed: %v", err), true
		}
		byContributor := 0
		if err := k.ContributionsByContributor.Walk(ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
			byContributor++
			if c, err := k.Contribution.Get(ctx, key.K2()); err != nil || c.Contributor != key.K1() {
				problems = append(problems, fmt.Sprintf("ContributionsByContributor (%s,%d) dangling", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ContributionsByContributor.Walk failed: %v", err), true
		}
		if byStatus != contributions {
			problems = append(problems, fmt.Sprintf("ContributionsByStatus has %d entries, Contribution has %d", byStatus, contributions))
		}
		if byContributor != contributions {
			problems = append(problems, fmt.Sprintf("ContributionsByContributor has %d entries, Contribution has %d", byContributor, contributions))
		}
		return sdk.FormatInvariant(types.ModuleName, "contribution-indexes", strings.Join(problems, "\n")), len(problems) > 0
	}
}

// DeadlineIndexInvariant: ContributionDeadlines and ContributionNextDeadline
// are the same set, and every entry points at an IN_PROGRESS contribution.
func DeadlineIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		next := 0
		if err := k.ContributionNextDeadline.Walk(ctx, nil, func(id uint64, at int64) (bool, error) {
			next++
			if has, _ := k.ContributionDeadlines.Has(ctx, collections.Join(at, id)); !has {
				problems = append(problems, fmt.Sprintf("contribution %d next deadline %d missing from ContributionDeadlines", id, at))
			}
			c, err := k.Contribution.Get(ctx, id)
			if err != nil || c.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
				problems = append(problems, fmt.Sprintf("deadline index entry for contribution %d that is missing or not IN_PROGRESS", id))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ContributionNextDeadline.Walk failed: %v", err), true
		}
		queued := 0
		if err := k.ContributionDeadlines.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			queued++
			if at, err := k.ContributionNextDeadline.Get(ctx, key.K2()); err != nil || at != key.K1() {
				problems = append(problems, fmt.Sprintf("ContributionDeadlines (%d,%d) does not match ContributionNextDeadline", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ContributionDeadlines.Walk failed: %v", err), true
		}
		if queued != next {
			problems = append(problems, fmt.Sprintf("ContributionDeadlines has %d entries, ContributionNextDeadline has %d", queued, next))
		}
		return sdk.FormatInvariant(types.ModuleName, "deadline-index", strings.Join(problems, "\n")), len(problems) > 0
	}
}
