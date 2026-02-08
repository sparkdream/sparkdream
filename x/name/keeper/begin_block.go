package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker processes begin block logic for the x/name module.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	return k.processExpiredDisputes(ctx)
}

// processExpiredDisputes auto-resolves uncontested disputes that have passed the timeout period.
// Uncontested disputes that expire are upheld: name transfers to claimant, claimant's stake is returned.
// Contested disputes are NOT auto-resolved — they wait for jury verdict via ResolveDispute.
func (k Keeper) processExpiredDisputes(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()
	params := k.GetParams(ctx)

	var toResolve []string // names to resolve

	iter, err := k.Disputes.Iterate(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to iterate disputes: %w", err)
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		dispute, err := iter.Value()
		if err != nil {
			continue
		}

		// Skip inactive disputes
		if !dispute.Active {
			continue
		}

		// Skip contested disputes (jury handles them)
		if dispute.ContestChallengeId != "" {
			continue
		}

		// Check if timeout has passed
		deadline := dispute.FiledAt + int64(params.DisputeTimeoutBlocks)
		if currentHeight > deadline {
			toResolve = append(toResolve, dispute.Name)
		}
	}

	// Resolve expired disputes outside the iterator to avoid concurrent modification
	for _, name := range toResolve {
		dispute, found := k.GetDispute(ctx, name)
		if !found || !dispute.Active {
			continue
		}

		// Auto-resolve: uncontested timeout = dispute upheld, name transfers
		if err := k.resolveDisputeInternal(ctx, dispute, true); err != nil {
			// Log error but don't halt the chain
			sdkCtx.Logger().Error("failed to auto-resolve expired dispute",
				"name", name, "error", err)
			continue
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"name_dispute_expired_upheld",
				sdk.NewAttribute("name", name),
				sdk.NewAttribute("claimant", dispute.Claimant),
				sdk.NewAttribute("expired_at_block", fmt.Sprintf("%d", currentHeight)),
			),
		)
	}

	return nil
}
