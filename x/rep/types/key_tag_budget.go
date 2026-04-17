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
)
