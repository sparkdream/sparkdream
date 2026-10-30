package keeper

import "context"

// SlidingWindowAtLimitForTest exposes the integer sliding-window check to the
// external test package.
func SlidingWindowAtLimitForTest(cur, prev uint64, elapsed, window int64, limit uint64) bool {
	return slidingWindowAtLimit(cur, prev, elapsed, window, limit)
}

// IndexHideExpiryForTest files a HideRecord written directly by a test under
// the expiry index, as MsgHidePost does.
func (k Keeper) IndexHideExpiryForTest(ctx context.Context, postID uint64) error {
	hr, err := k.HideRecord.Get(ctx, postID)
	if err != nil {
		return err
	}
	return k.indexHideExpiry(ctx, postID, hr)
}

// ReconcileThreadBountyForTest runs bounty moderation reconciliation for a
// thread, as every hide/lock settlement path does.
func (k Keeper) ReconcileThreadBountyForTest(ctx context.Context, threadID uint64) error {
	return k.reconcileThreadBounty(ctx, threadID)
}
