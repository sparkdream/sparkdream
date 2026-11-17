//go:build !mainnet && !testnet && !devnet

package types

// Test-build windows: a few blocks each, so test/storage can watch a request
// expire, a claim lapse, a dispute hold and a flagged fee burn in one run.
func defaultRequestTTLBlocks() int64        { return 120 }
func defaultClaimWindowBlocks() int64       { return 15 }
func defaultSettleWindowBlocks() int64      { return 10 }
func defaultDisputeWindowBlocks() int64     { return 30 }
func defaultFlagHoldBlocks() int64          { return 30 }
func defaultMinUnreachableBlocks() int64    { return 3 }
func defaultTerminalRetentionBlocks() int64 { return 60 }
func defaultCounterWindowBlocks() int64     { return 400 }
func defaultDeadlineRetryBlocks() int64     { return 5 }
