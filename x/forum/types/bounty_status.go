package types

// IsOpen reports whether a bounty still holds its escrow and can still
// settle: ACTIVE, or suspended while its root's moderation is pending
// (MODERATION_PENDING). An open bounty owns its thread's ActiveBountyByThread
// entry.
func (b Bounty) IsOpen() bool {
	return b.Status == BountyStatus_BOUNTY_STATUS_ACTIVE ||
		b.Status == BountyStatus_BOUNTY_STATUS_MODERATION_PENDING
}
