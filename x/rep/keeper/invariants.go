package keeper

import (
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// RegisterInvariants wires x/rep's consistency checks into x/crisis.
//
// x/rep keeps its own token ledger — DREAM balances on member records, four
// reward accumulators, a module treasury — none of which x/bank or any other
// module can cross-check. Nothing was registered here at all, so every failure
// class the module can suffer (a denominator drifting from the stakes backing
// it, an aggregate exceeding the balance it is a subset of, a treasury going
// negative) was undetectable on-chain.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "seasonal-pool-denominator", SeasonalPoolDenominatorInvariant(k))
	ir.RegisterRoute(types.ModuleName, "member-staked-within-balance", MemberStakedWithinBalanceInvariant(k))
	ir.RegisterRoute(types.ModuleName, "treasury-non-negative", TreasuryNonNegativeInvariant(k))
	ir.RegisterRoute(types.ModuleName, "season-caps-not-exceeded", SeasonCapsNotExceededInvariant(k))
}

// SeasonalPoolDenominatorInvariant: SeasonalPoolTotalStaked must equal the sum
// of live INITIATIVE and PROJECT stake amounts.
//
// This is the MasterChef denominator every seasonal payout divides by. If it
// drifts above the real total, every staker is under-paid; below it, the pool
// over-pays and can exceed its budget. Every mutation is supposed to route
// through updateStakePoolTotals — this is what proves it did.
func SeasonalPoolDenominatorInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		want := math.ZeroInt()
		if err := k.Stake.Walk(ctx, nil, func(_ uint64, stake types.Stake) (bool, error) {
			switch stake.TargetType {
			case types.StakeTargetType_STAKE_TARGET_INITIATIVE,
				types.StakeTargetType_STAKE_TARGET_PROJECT:
				if !stake.Amount.IsNil() && stake.Amount.IsPositive() {
					want = want.Add(stake.Amount)
				}
			}
			return false, nil
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "seasonal-pool-denominator",
				fmt.Sprintf("failed to walk stakes: %v", err)), true
		}

		got, err := k.GetSeasonalPoolTotalStaked(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "seasonal-pool-denominator",
				fmt.Sprintf("failed to read total_staked: %v", err)), true
		}
		if !got.Equal(want) {
			return sdk.FormatInvariant(types.ModuleName, "seasonal-pool-denominator",
				fmt.Sprintf("seasonal pool total_staked is %s but live initiative+project stakes sum to %s",
					got, want)), true
		}
		return "", false
	}
}

// MemberStakedWithinBalanceInvariant: staked_dream must never exceed
// dream_balance, and neither may be negative.
//
// staked_dream is a SUBSET of dream_balance (LockDREAM adds to the former
// without reducing the latter), so `unlocked = dream_balance - staked_dream` is
// read all over the module and goes wrong silently if the relation inverts.
// Both the old ZeroMember double-count and the pre-decayStakes design that
// eroded the aggregate while its obligations kept face value could push a
// member here.
func MemberStakedWithinBalanceInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := false
		if err := k.Member.Walk(ctx, nil, func(addr string, m types.Member) (bool, error) {
			bal := DerefInt(m.DreamBalance)
			staked := DerefInt(m.StakedDream)
			switch {
			case bal.IsNegative():
				msg = fmt.Sprintf("member %s has negative dream_balance %s", addr, bal)
			case staked.IsNegative():
				msg = fmt.Sprintf("member %s has negative staked_dream %s", addr, staked)
			case staked.GT(bal):
				msg = fmt.Sprintf("member %s has staked_dream %s exceeding dream_balance %s",
					addr, staked, bal)
			default:
				return false, nil
			}
			broken = true
			return true, nil
		}); err != nil {
			return sdk.FormatInvariant(types.ModuleName, "member-staked-within-balance",
				fmt.Sprintf("failed to walk members: %v", err)), true
		}
		if broken {
			return sdk.FormatInvariant(types.ModuleName, "member-staked-within-balance", msg), true
		}
		return "", false
	}
}

// TreasuryNonNegativeInvariant: the module treasury ledger must never go
// negative. Spend paths are supposed to clamp; this proves they did.
func TreasuryNonNegativeInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		bal, err := k.GetTreasuryBalance(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "treasury-non-negative",
				fmt.Sprintf("failed to read treasury balance: %v", err)), true
		}
		if bal.IsNegative() {
			return sdk.FormatInvariant(types.ModuleName, "treasury-non-negative",
				fmt.Sprintf("treasury balance is negative: %s", bal)), true
		}
		return "", false
	}
}

// SeasonCapsNotExceededInvariant: the per-season emission counters must stay
// within the caps that gate them.
//
// The caps are checked before minting, so a counter above its cap means a
// payout path minted without charging the gate — which is how the interim path
// behaved before it had a cap at all. Warning-grade rather than halting: a
// governance change that lowers a cap mid-season legitimately leaves the
// counter above it, and halting the chain for that would be worse than
// reporting it.
func SeasonCapsNotExceededInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		params, err := k.Params.Get(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "season-caps-not-exceeded",
				fmt.Sprintf("failed to read params: %v", err)), false
		}

		for _, c := range []struct {
			name string
			get  func(sdk.Context) (math.Int, error)
			cap  math.Int
		}{
			{"initiative", func(c sdk.Context) (math.Int, error) { return k.GetSeasonInitiativeRewardsMinted(c) },
				params.MaxInitiativeRewardsPerSeason},
			{"interim", func(c sdk.Context) (math.Int, error) { return k.GetSeasonInterimRewardsMinted(c) },
				params.MaxInterimRewardsPerSeason},
			{"staking", func(c sdk.Context) (math.Int, error) { return k.GetSeasonStakingRewardsMinted(c) },
				params.MaxStakingRewardsPerSeason},
		} {
			if c.cap.IsNil() || !c.cap.IsPositive() {
				continue
			}
			minted, err := c.get(ctx)
			if err != nil {
				return sdk.FormatInvariant(types.ModuleName, "season-caps-not-exceeded",
					fmt.Sprintf("failed to read %s season counter: %v", c.name, err)), false
			}
			if minted.GT(c.cap) {
				return sdk.FormatInvariant(types.ModuleName, "season-caps-not-exceeded",
					fmt.Sprintf("%s rewards minted this season (%s) exceed the cap (%s)",
						c.name, minted, c.cap)), false
			}
		}
		return "", false
	}
}
