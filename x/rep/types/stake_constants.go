package types

// Staking constants that shape reward accounting and bound per-block work.
// These are deliberately compile-time constants rather than governance params:
// they are structural limits whose safe range depends on keeper implementation
// details (per-block iteration cost, MasterChef debt semantics), so changing
// them belongs to a chain upgrade alongside the code that reads them.
const (
	// MaxStakeTranchesPerTarget caps how many separate stake records one member
	// may hold on a single target.
	//
	// Stakes are deliberately never merged: each carries its own created_at
	// maturity clock and its own reward_debt baseline, and averaging two joins
	// made at different times or at different accumulator values would break
	// both. But nothing about that design needs an *unbounded* record count,
	// and two costs scale with it — the EndBlocker conviction sweep touches
	// every stake of every active initiative on every block, and CreateStake's
	// per-member cap check is O(existing stakes on the target), making n
	// tranches cost O(n^2) to accumulate. Since the staked DREAM is fully
	// refundable via Unstake, an uncapped count lets a member impose permanent
	// per-block work on every validator for close to free.
	MaxStakeTranchesPerTarget = 10
)
