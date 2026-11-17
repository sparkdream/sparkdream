//go:build mainnet || testnet || devnet

package types

// Network windows, in blocks: 17280 a day at 5s blocks, the figure x/service
// uses for the unbonding period these windows must stay inside.
const blocksPerDay int64 = 17280

func defaultRequestTTLBlocks() int64        { return 7 * blocksPerDay }
func defaultClaimWindowBlocks() int64       { return blocksPerDay / 12 }
func defaultSettleWindowBlocks() int64      { return blocksPerDay }
func defaultDisputeWindowBlocks() int64     { return 14 * blocksPerDay }
func defaultFlagHoldBlocks() int64          { return 14 * blocksPerDay }
func defaultMinUnreachableBlocks() int64    { return blocksPerDay / 24 }
func defaultTerminalRetentionBlocks() int64 { return 30 * blocksPerDay }
func defaultCounterWindowBlocks() int64     { return blocksPerDay }
func defaultDeadlineRetryBlocks() int64     { return blocksPerDay / 24 }
