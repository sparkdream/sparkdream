package types

import (
	"context"
)

// FutarchyHooks event hooks for the futarchy module.
type FutarchyHooks interface {
	// AfterMarketResolved is called when a market status changes to RESOLVED_YES or RESOLVED_NO
	AfterMarketResolved(ctx context.Context, marketId uint64, winner string) error
}

// MultiFutarchyHooks combines multiple hooks (standard boilerplate)
type MultiFutarchyHooks []FutarchyHooks

func NewMultiFutarchyHooks(hooks ...FutarchyHooks) MultiFutarchyHooks {
	return hooks
}

func (h MultiFutarchyHooks) AfterMarketResolved(ctx context.Context, marketId uint64, winner string) error {
	for i := range h {
		if err := h[i].AfterMarketResolved(ctx, marketId, winner); err != nil {
			return err
		}
	}
	return nil
}
