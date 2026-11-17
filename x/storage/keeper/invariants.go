package keeper

import (
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

// RegisterInvariants registers the module invariants (spec section 13). They
// are evaluated by `go test ./app -run TestInvariants`; x/crisis is not wired.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "escrow", EscrowInvariant(k))
	ir.RegisterRoute(types.ModuleName, "indexes", IndexInvariant(k))
	ir.RegisterRoute(types.ModuleName, "flags", FlagInvariant(k))
}

// AllInvariants runs every invariant and returns the first failure.
func AllInvariants(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		for _, inv := range []sdk.Invariant{EscrowInvariant(k), IndexInvariant(k), FlagInvariant(k)} {
			if msg, broken := inv(ctx); broken {
				return msg, true
			}
		}
		return "", false
	}
}

// paramsOrDefault reads the params, or the defaults on a chain whose genesis
// was written before the module existed (nothing is stored there yet).
func (k Keeper) paramsOrDefault(ctx sdk.Context) (types.Params, error) {
	p, err := k.Params.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return types.DefaultParams(), nil
	}
	return p, err
}

func broken(route, format string, args ...any) (string, bool) {
	return sdk.FormatInvariant(types.ModuleName, route, fmt.Sprintf(format, args...)), true
}

// EscrowInvariant: the module balance covers every request's copy_fee, and
// only a request that still holds its fee has one. "Covers", never
// "equals": a stray coin must not break it.
func EscrowInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		total := math.ZeroInt()
		var bad string
		err := k.Requests.Walk(ctx, nil, func(id uint64, r types.Request) (bool, error) {
			if r.CopyFee.IsNil() || r.CopyFee.IsNegative() {
				bad = fmt.Sprintf("request %d has an invalid copy_fee", id)
				return true, nil
			}
			holds := r.Status.IsOpen() || (r.Status == types.RequestStatus_REQUEST_STATUS_REJECTED && r.ForfeitAt != 0)
			if !holds && !r.CopyFee.IsZero() {
				bad = fmt.Sprintf("request %d (%s) holds %s but should hold nothing", id, r.Status, r.CopyFee)
				return true, nil
			}
			total = total.Add(r.CopyFee)
			return false, nil
		})
		if err != nil {
			return broken("escrow", "walk failed: %v", err)
		}
		if bad != "" {
			return broken("escrow", "%s", bad)
		}
		if k.late.identity == nil {
			return "", false
		}
		have := k.bankKeeper.GetBalance(ctx, k.moduleAddress(), k.BondDenom(ctx)).Amount
		if have.LT(total) {
			return broken("escrow", "module holds %s, requests need %s", have, total)
		}
		return "", false
	}
}

// IndexInvariant: every request is in exactly the indexes its status calls
// for, has a timer of each kind it needs, and keeps its lists inside the caps.
func IndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		p, err := k.paramsOrDefault(ctx)
		if err != nil {
			return broken("indexes", "params: %v", err)
		}
		timers := map[[2]uint64]bool{}
		if err := k.Deadlines.Walk(ctx, nil, func(key collections.Triple[int64, uint64, uint32]) (bool, error) {
			timers[[2]uint64{key.K2(), uint64(key.K3())}] = true
			return false, nil
		}); err != nil {
			return broken("indexes", "deadlines walk failed: %v", err)
		}
		live := map[string]uint64{}
		var bad string
		err = k.Requests.Walk(ctx, nil, func(id uint64, r types.Request) (bool, error) {
			fail := func(format string, args ...any) (bool, error) {
				bad = fmt.Sprintf("request %d: ", id) + fmt.Sprintf(format, args...)
				return true, nil
			}
			if has, _ := k.ByStatus.Has(ctx, collections.Join(statusKey(r.Backend, r.Status), id)); !has {
				return fail("missing from ByStatus")
			}
			if has, _ := k.ByRequester.Has(ctx, collections.Join(r.Requester, id)); !has {
				return fail("missing from ByRequester")
			}
			if has, _ := k.OpenByRequester.Has(ctx, collections.Join(r.Requester, id)); has != r.Status.IsOpen() {
				return fail("OpenByRequester is %v for status %s", has, r.Status)
			}
			if r.Status.IsLive() {
				key := r.Backend + "/" + r.Sha256
				if other, dup := live[key]; dup {
					return fail("shares its file with live request %d", other)
				}
				live[key] = id
				if got, err := k.LiveByHash.Get(ctx, collections.Join(r.Backend, r.Sha256)); err != nil || got != id {
					return fail("LiveByHash does not point at it")
				}
			}
			claimed := r.Status == types.RequestStatus_REQUEST_STATUS_CLAIMED
			held := claimed || r.Status == types.RequestStatus_REQUEST_STATUS_COMPLETED
			if r.Operator != "" {
				if has, _ := k.ClaimsByOperator.Has(ctx, collections.Join(r.Operator, id)); has != claimed {
					return fail("ClaimsByOperator is %v for status %s", has, r.Status)
				}
				if has, _ := k.ByOperator.Has(ctx, collections.Join(r.Operator, id)); has != held {
					return fail("ByOperator is %v for status %s", has, r.Status)
				}
			} else if held {
				return fail("status %s without an operator", r.Status)
			}
			for _, t := range timersOf(r, p) {
				if !timers[[2]uint64{id, uint64(t.kind)}] {
					return fail("no timer of kind %d", t.kind)
				}
			}
			if uint64(len(r.Barred)) > uint64(p.MaxBarredPerRequest) || uint64(len(r.DeclinedBy)) > uint64(p.MaxBarredPerRequest) ||
				uint64(len(r.Sources)) > uint64(p.MaxSourcesPerRequest) || uint64(len(r.Attestations)) > uint64(p.MaxAttestationsPerCompletion) {
				return fail("a list is above its cap")
			}
			return false, nil
		})
		if err != nil {
			return broken("indexes", "walk failed: %v", err)
		}
		if bad != "" {
			return broken("indexes", "%s", bad)
		}
		// LiveByHash holds nothing else.
		n := 0
		if err := k.LiveByHash.Walk(ctx, nil, func(collections.Pair[string, string], uint64) (bool, error) {
			n++
			return false, nil
		}); err != nil {
			return broken("indexes", "LiveByHash walk failed: %v", err)
		}
		if n != len(live) {
			return broken("indexes", "LiveByHash has %d entries for %d live requests", n, len(live))
		}
		return "", false
	}
}

// FlagInvariant: no open request's hash is flagged; a flag record's held
// list names exactly the flagged requests whose fee is still held.
func FlagInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		p, err := k.paramsOrDefault(ctx)
		if err != nil {
			return broken("flags", "params: %v", err)
		}
		held := map[uint64]bool{}
		var bad string
		err = k.RemovedHashes.Walk(ctx, nil, func(hash string, rec types.RemovedHash) (bool, error) {
			if len(rec.Flags) == 0 || uint64(len(rec.Flags)) > uint64(p.MaxFlagsPerHash) {
				bad = fmt.Sprintf("hash %s has %d flags", hash, len(rec.Flags))
				return true, nil
			}
			seen := map[string]bool{}
			for _, f := range rec.Flags {
				if seen[f.Scanner] {
					bad = fmt.Sprintf("hash %s is flagged twice by %s", hash, f.Scanner)
					return true, nil
				}
				seen[f.Scanner] = true
			}
			for _, id := range rec.Held {
				r, err := k.Requests.Get(ctx, id)
				if err != nil || r.Sha256 != hash || r.Status != types.RequestStatus_REQUEST_STATUS_REJECTED ||
					r.ClosedReason != types.ClosedReason_CLOSED_REASON_FLAGGED || r.ForfeitAt == 0 {
					bad = fmt.Sprintf("hash %s holds request %d, which is not a flagged request with a held fee", hash, id)
					return true, nil
				}
				held[id] = true
			}
			return false, nil
		})
		if err != nil {
			return broken("flags", "walk failed: %v", err)
		}
		if bad != "" {
			return broken("flags", "%s", bad)
		}
		err = k.Requests.Walk(ctx, nil, func(id uint64, r types.Request) (bool, error) {
			if r.Status.IsOpen() {
				if flagged, _ := k.RemovedHashes.Has(ctx, r.Sha256); flagged {
					bad = fmt.Sprintf("open request %d has a flagged hash", id)
					return true, nil
				}
			}
			if r.ForfeitAt != 0 && !held[id] {
				bad = fmt.Sprintf("request %d holds a flagged fee but is on no held list", id)
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			return broken("flags", "requests walk failed: %v", err)
		}
		if bad != "" {
			return broken("flags", "%s", bad)
		}
		return "", false
	}
}
