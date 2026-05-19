package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/federation/types"
)

// RegisterInvariants registers all x/federation module invariants.
// Added in Phase 5b of the federation→service migration plan. The
// crisis module consults these via
// `simd q crisis invariant federation/<name>`; simulations also
// exercise them.
//
// Currently registers a single invariant — the orphan-binding check —
// because that's the consistency property the federation→service
// hand-off can plausibly violate (a fail-soft hook recovers from a
// panic by logging instead of rolling back, leaving a BridgeBinding
// without a live service.Operator).
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "orphan-bindings", OrphanBindingsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "bindings-by-operator-index", BindingsByOperatorIndexInvariant(k))
}

// OrphanBindingsInvariant fails when any BridgeBinding references an
// operator address whose service.Operator (at the binding's derived
// service_type) is not live in x/service. Orphans accumulate when the
// fail-soft hook pattern swallows a panic during dissolution / retire
// and leaves the federation-side cleanup undone. Recovery: invoke
// MsgPruneOrphanBindings (gov or Operations Committee) on the affected
// peer; orphans get pruned without a chain upgrade.
//
// In standalone mode (service keeper not wired) this invariant is a
// no-op — it has no way to query operator status.
func OrphanBindingsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		sk := k.serviceKeeper()
		if sk == nil {
			return "", false
		}

		var orphans []string
		err := k.BridgeBindings.Walk(ctx, nil, func(key collections.Pair[string, string], binding types.BridgeBinding) (bool, error) {
			operatorAddr := key.K1()
			peerID := key.K2()

			peer, perr := k.Peers.Get(ctx, peerID)
			if perr != nil {
				orphans = append(orphans, fmt.Sprintf("binding(%s, %s): peer not found", operatorAddr, peerID))
				return false, nil
			}
			serviceType, terr := serviceTypeForPeer(peer.Type)
			if terr != nil {
				// SPARK_DREAM peer with a binding — shouldn't happen,
				// log as orphan.
				orphans = append(orphans, fmt.Sprintf("binding(%s, %s): peer type %s has no service_type mapping", operatorAddr, peerID, peer.Type))
				return false, nil
			}
			opBytes, cerr := k.addressCodec.StringToBytes(operatorAddr)
			if cerr != nil {
				orphans = append(orphans, fmt.Sprintf("binding(%s, %s): operator address codec failure", operatorAddr, peerID))
				return false, nil
			}
			if _, exists := sk.GetOperator(ctx, opBytes, serviceType); !exists {
				orphans = append(orphans, fmt.Sprintf("binding(%s, %s, service_type=%s): no live service.Operator", operatorAddr, peerID, serviceType))
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "orphan-bindings",
				fmt.Sprintf("walk error: %v", err)), true
		}
		if len(orphans) > 0 {
			return sdk.FormatInvariant(types.ModuleName, "orphan-bindings",
				fmt.Sprintf("%d orphan binding(s):\n  %s", len(orphans), joinFirstN(orphans, 10))), true
		}
		return "", false
	}
}

// BindingsByOperatorIndexInvariant fails when the BindingsByOperator
// reverse index disagrees with the primary BridgeBindings map (in
// either direction). Catches index corruption from hook bugs.
func BindingsByOperatorIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var mismatches []string

		// 1. Every primary binding has a corresponding reverse-index entry.
		err := k.BridgeBindings.Walk(ctx, nil, func(key collections.Pair[string, string], binding types.BridgeBinding) (bool, error) {
			operatorAddr := key.K1()
			peerID := key.K2()
			peer, perr := k.Peers.Get(ctx, peerID)
			if perr != nil {
				return false, nil // OrphanBindingsInvariant catches this
			}
			serviceType, terr := serviceTypeForPeer(peer.Type)
			if terr != nil {
				return false, nil
			}
			ok, herr := k.BindingsByOperator.Has(ctx, collections.Join3(serviceType, operatorAddr, peerID))
			if herr != nil {
				return false, nil
			}
			if !ok {
				mismatches = append(mismatches, fmt.Sprintf("binding(%s, %s) missing reverse-index entry at (service_type=%s)", operatorAddr, peerID, serviceType))
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "bindings-by-operator-index",
				fmt.Sprintf("primary walk error: %v", err)), true
		}

		// 2. Every reverse-index entry points at an existing primary binding.
		err = k.BindingsByOperator.Walk(ctx, nil, func(key collections.Triple[string, string, string]) (bool, error) {
			serviceType := key.K1()
			operatorAddr := key.K2()
			peerID := key.K3()
			ok, gerr := k.BridgeBindings.Has(ctx, collections.Join(operatorAddr, peerID))
			if gerr != nil {
				return false, nil
			}
			if !ok {
				mismatches = append(mismatches, fmt.Sprintf("reverse-index entry (service_type=%s, %s, %s) has no primary binding", serviceType, operatorAddr, peerID))
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "bindings-by-operator-index",
				fmt.Sprintf("reverse walk error: %v", err)), true
		}

		if len(mismatches) > 0 {
			return sdk.FormatInvariant(types.ModuleName, "bindings-by-operator-index",
				fmt.Sprintf("%d mismatch(es):\n  %s", len(mismatches), joinFirstN(mismatches, 10))), true
		}
		return "", false
	}
}

// joinFirstN joins at most n strings with newline+indent for invariant
// messages. Keeps panic messages bounded if a huge mismatch set hits.
func joinFirstN(items []string, n int) string {
	cap := n
	if len(items) < cap {
		cap = len(items)
	}
	out := ""
	for i := 0; i < cap; i++ {
		if i > 0 {
			out += "\n  "
		}
		out += items[i]
	}
	if len(items) > n {
		out += fmt.Sprintf("\n  ... and %d more", len(items)-n)
	}
	return out
}
