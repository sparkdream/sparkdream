package types

import "cosmossdk.io/collections"

var (
	TagBudgetKey      = collections.NewPrefix("tagbudget/value/")
	TagBudgetCountKey = collections.NewPrefix("tagbudget/count/")

	TagBudgetAwardKey      = collections.NewPrefix("tagbudgetaward/value/")
	TagBudgetAwardCountKey = collections.NewPrefix("tagbudgetaward/count/")

	// TagBudgetAwardByPostKey maps (budget_id, post_id) -> block height of the most recent
	// award, used to enforce a per-(budget, post) cooldown against drain attacks.
	TagBudgetAwardByPostKey = collections.NewPrefix("tagbudgetaward/bypost/")

	// TagBudgetAwarderEpochKey maps (budget_id, canonical awarder, epoch) ->
	// amount that awarder has awarded from the budget in that epoch, for the
	// per-awarder epoch cap (tag_budget_awarder_epoch_cap_bps). At most one
	// entry per (budget, awarder): older epochs are dropped on the next award.
	TagBudgetAwarderEpochKey = collections.NewPrefix("tagbudgetaward/byawarder/")
)

// DefaultTagBudgetAwarderEpochCapBps is the per-awarder epoch cap used when
// tag_budget_awarder_epoch_cap_bps is 0: 25% of the pool.
const DefaultTagBudgetAwarderEpochCapBps uint64 = 2500

// TagBudgetAwardCooldownBlocks is the minimum number of blocks that must elapse between
// awards to the same post from the same budget. Hardcoded to avoid a new governance param.
const TagBudgetAwardCooldownBlocks int64 = 100
