package types

// RewardDenom is the bank-module denom used for the sentinel SPARK reward pool.
// The pool itself is simply the rep module account's balance of this denom, so
// SPARK vs DREAM are unambiguously separated within the same module account:
// uspark = sentinel reward pool, udream (and friends) = escrow/bonds/etc.
const RewardDenom = "uspark"
