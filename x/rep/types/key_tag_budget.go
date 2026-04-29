package types

import "cosmossdk.io/collections"

// TagBudgetFeeDenom is the denom used for tag-budget pool escrow and payouts.
// Tag budgets hold SPARK; DREAM awards flow through x/rep's DREAM primitives
// separately and are not used here.
const TagBudgetFeeDenom = "uspark"

var (
	TagBudgetKey      = collections.NewPrefix("tagbudget/value/")
	TagBudgetCountKey = collections.NewPrefix("tagbudget/count/")

	TagBudgetAwardKey      = collections.NewPrefix("tagbudgetaward/value/")
	TagBudgetAwardCountKey = collections.NewPrefix("tagbudgetaward/count/")

	// TagBudgetAwardByPostKey maps (budget_id, post_id) -> block height of the most recent
	// award, used to enforce a per-(budget, post) cooldown against drain attacks.
	TagBudgetAwardByPostKey = collections.NewPrefix("tagbudgetaward/bypost/")
)

// TagBudgetAwardCooldownBlocks is the minimum number of blocks that must elapse between
// awards to the same post from the same budget. Hardcoded to avoid a new governance param.
const TagBudgetAwardCooldownBlocks int64 = 100
