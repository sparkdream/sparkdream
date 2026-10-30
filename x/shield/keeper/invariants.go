package keeper

import (
	"encoding/hex"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// RegisterInvariants registers the shield module invariants. They run in
// app.TestInvariants against every e2e post-setup snapshot (x/crisis is not
// wired on chain).
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "pending-op-count", PendingOpCountInvariant(k))
	ir.RegisterRoute(types.ModuleName, "pending-nullifiers", PendingNullifiersInvariant(k))
	ir.RegisterRoute(types.ModuleName, "canonical-nullifiers", CanonicalNullifiersInvariant(k))
}

// PendingOpCountInvariant checks the maintained pending-op counter equals the
// number of stored pending ops (the queue-capacity check reads the counter).
func PendingOpCountInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var n uint64
		if err := k.IteratePendingOps(ctx, func(types.PendingShieldedOp) bool {
			n++
			return false
		}); err != nil {
			return fmt.Sprintf("k.IteratePendingOps failed: %v", err), true
		}
		stored := k.GetPendingOpCountVal(ctx)
		broken := stored != n
		return sdk.FormatInvariant(types.ModuleName, "pending-op-count",
			fmt.Sprintf("pending_op_count %d, pending ops stored %d", stored, n)), broken
	}
}

// PendingNullifiersInvariant checks every pending submission nullifier
// belongs to a pending op and every pending op holds its nullifier, so the
// batch dedup set can neither leak entries nor miss one.
func PendingNullifiersInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		ops := map[string]bool{}
		if err := k.IteratePendingOps(ctx, func(op types.PendingShieldedOp) bool {
			ops[hex.EncodeToString(op.SubmissionNullifier)] = true
			return false
		}); err != nil {
			return fmt.Sprintf("k.IteratePendingOps failed: %v", err), true
		}
		var msg string
		broken := 0
		seen := map[string]bool{}
		if err := k.IteratePendingNullifiers(ctx, func(n string) bool {
			seen[n] = true
			if !ops[n] {
				broken++
				msg += fmt.Sprintf("pending nullifier %s has no pending op\n", n)
			}
			return false
		}); err != nil {
			return fmt.Sprintf("k.IteratePendingNullifiers failed: %v", err), true
		}
		for n := range ops {
			if !seen[n] {
				broken++
				msg += fmt.Sprintf("pending op with nullifier %s is not in the pending set\n", n)
			}
		}
		return sdk.FormatInvariant(types.ModuleName, "pending-nullifiers",
			fmt.Sprintf("%d mismatches\n%s", broken, msg)), broken > 0
	}
}

// CanonicalNullifiersInvariant checks every used and pending nullifier is a
// canonical BN254 field element: a non-canonical one could only have been
// stored by skipping the public-input check (SHD-01), and would be an alias of
// another member action.
func CanonicalNullifiersInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		broken := 0
		if err := k.IterateUsedNullifiers(ctx, func(n types.UsedNullifier) bool {
			if !types.IsCanonicalNullifierHex(n.NullifierHex) {
				broken++
				msg += fmt.Sprintf("used nullifier (%d, %d, %s) is not canonical\n", n.Domain, n.Scope, n.NullifierHex)
			}
			return false
		}); err != nil {
			return fmt.Sprintf("k.IterateUsedNullifiers failed: %v", err), true
		}
		if err := k.IteratePendingNullifiers(ctx, func(n string) bool {
			if !types.IsCanonicalNullifierHex(n) {
				broken++
				msg += fmt.Sprintf("pending nullifier %s is not canonical\n", n)
			}
			return false
		}); err != nil {
			return fmt.Sprintf("k.IteratePendingNullifiers failed: %v", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "canonical-nullifiers",
			fmt.Sprintf("%d non-canonical nullifiers\n%s", broken, msg)), broken > 0
	}
}
