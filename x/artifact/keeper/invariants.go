package keeper

import (
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// RegisterInvariants registers all x/artifact invariants
// (docs/x-artifact-spec.md §13).
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "owner-index", OwnerIndexInvariant(k))
	ir.RegisterRoute(types.ModuleName, "supply", SupplyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "locks", LockInvariant(k))
	ir.RegisterRoute(types.ModuleName, "escrow", EscrowInvariant(k))
	ir.RegisterRoute(types.ModuleName, "flags", FlagsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "inbox-counters", InboxCounterInvariant(k))
	ir.RegisterRoute(types.ModuleName, "hide-consistency", HideConsistencyInvariant(k))
}

// AllInvariants runs every invariant (tests and simulation).
func AllInvariants(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		for _, inv := range []sdk.Invariant{
			OwnerIndexInvariant(k), SupplyInvariant(k), LockInvariant(k), EscrowInvariant(k),
			FlagsInvariant(k), InboxCounterInvariant(k), HideConsistencyInvariant(k),
		} {
			if msg, broken := inv(ctx); broken {
				return msg, true
			}
		}
		return "", false
	}
}

func report(name string, problems []string) (string, bool) {
	return sdk.FormatInvariant(types.ModuleName, name,
		fmt.Sprintf("found %d violations\n%s", len(problems), strings.Join(problems, "\n"))), len(problems) > 0
}

// OwnerIndexInvariant: every token has its TokensByOwner entry and every
// entry points at a token with that owner.
func OwnerIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		_ = k.Tokens.Walk(ctx, nil, func(key U64Pair, t types.Token) (bool, error) {
			if has, _ := k.TokensByOwner.Has(ctx, collections.Join3(t.Owner, t.ClassId, t.Id)); !has {
				problems = append(problems, fmt.Sprintf("token %d/%d missing owner index", t.ClassId, t.Id))
			}
			return false, nil
		})
		_ = k.TokensByOwner.Walk(ctx, nil, func(key AddrTokenKey) (bool, error) {
			t, err := k.Tokens.Get(ctx, collections.Join(key.K2(), key.K3()))
			if err != nil || t.Owner != key.K1() {
				problems = append(problems, fmt.Sprintf("stale owner index %s %d/%d", key.K1(), key.K2(), key.K3()))
			}
			return false, nil
		})
		return report("owner-index", problems)
	}
}

// SupplyInvariant: class counters match the stores, the cap holds, ids are
// below next_token_id, and no token shares an id with a pending mint.
func SupplyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		seq, _ := k.ClassSeq.Peek(ctx)
		_ = k.Classes.Walk(ctx, nil, func(id uint64, c types.Class) (bool, error) {
			if c.Id > seq {
				problems = append(problems, fmt.Sprintf("class %d above class_seq %d", c.Id, seq))
			}
			var live, reserved uint64
			_ = k.Tokens.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](id), func(key U64Pair, t types.Token) (bool, error) {
				live++
				if t.Id >= c.NextTokenId {
					problems = append(problems, fmt.Sprintf("token %d/%d >= next_token_id %d", id, t.Id, c.NextTokenId))
				}
				return false, nil
			})
			_ = k.PendingMints.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](id), func(key U64Pair, pm types.PendingMint) (bool, error) {
				reserved++
				if has, _ := k.Tokens.Has(ctx, key); has {
					problems = append(problems, fmt.Sprintf("pending mint %d/%d collides with a token", id, pm.TokenId))
				}
				return false, nil
			})
			if live != c.Supply {
				problems = append(problems, fmt.Sprintf("class %d supply %d != %d tokens", id, c.Supply, live))
			}
			if reserved != c.ReservedSupply {
				problems = append(problems, fmt.Sprintf("class %d reserved %d != %d pending mints", id, c.ReservedSupply, reserved))
			}
			if c.MaxSupply > 0 && issued(c) > c.MaxSupply {
				problems = append(problems, fmt.Sprintf("class %d issued %d > max %d", id, issued(c), c.MaxSupply))
			}
			return false, nil
		})
		return report("supply", problems)
	}
}

// LockInvariant: a lock exists iff its record exists, the record names the
// owner, and the listing nonce equals the token's listing_seq.
func LockInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		_ = k.Tokens.Walk(ctx, nil, func(key U64Pair, t types.Token) (bool, error) {
			l, lErr := k.Listings.Get(ctx, key)
			pt, pErr := k.PendingTransfers.Get(ctx, key)
			switch t.Lock {
			case types.TokenLock_TOKEN_LOCK_LISTED:
				if lErr != nil {
					problems = append(problems, fmt.Sprintf("token %d/%d LISTED without listing", t.ClassId, t.Id))
				} else if l.Seller != t.Owner || l.Nonce != t.ListingSeq {
					problems = append(problems, fmt.Sprintf("listing %d/%d seller/nonce mismatch", t.ClassId, t.Id))
				}
			case types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER:
				if pErr != nil {
					problems = append(problems, fmt.Sprintf("token %d/%d PENDING without record", t.ClassId, t.Id))
				} else if pt.From != t.Owner {
					problems = append(problems, fmt.Sprintf("pending transfer %d/%d from != owner", t.ClassId, t.Id))
				}
			}
			if t.Lock != types.TokenLock_TOKEN_LOCK_LISTED && lErr == nil {
				problems = append(problems, fmt.Sprintf("listing %d/%d on unlocked token", t.ClassId, t.Id))
			}
			if t.Lock != types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER && pErr == nil {
				problems = append(problems, fmt.Sprintf("pending transfer %d/%d on unlocked token", t.ClassId, t.Id))
			}
			return false, nil
		})
		_ = k.Listings.Walk(ctx, nil, func(key U64Pair, l types.Listing) (bool, error) {
			if has, _ := k.Tokens.Has(ctx, key); !has {
				problems = append(problems, fmt.Sprintf("listing %d/%d without token", key.K1(), key.K2()))
			}
			return false, nil
		})
		return report("locks", problems)
	}
}

// EscrowInvariant: the module balance covers every recorded deposit.
// (Appeal bonds are held by x/rep, not here.)
func EscrowInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		if k.late.identity == nil {
			return "", false
		}
		need := math.ZeroInt()
		_ = k.Tokens.Walk(ctx, nil, func(_ U64Pair, t types.Token) (bool, error) {
			need = need.Add(t.Deposit)
			return false, nil
		})
		_ = k.PendingMints.Walk(ctx, nil, func(_ U64Pair, pm types.PendingMint) (bool, error) {
			need = need.Add(pm.Deposit)
			return false, nil
		})
		have := k.bankKeeper.GetBalance(ctx, k.ModuleAddress(), k.BondDenom(ctx)).Amount
		var problems []string
		if have.LT(need) {
			problems = append(problems, fmt.Sprintf("module holds %s < escrowed %s", have, need))
		}
		return report("escrow", problems)
	}
}

// FlagsInvariant: no class the issuer can burn is transferable; every class
// has a valid royalty split; soulbound classes carry no royalty, mint price,
// listing or pending transfer.
func FlagsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		_ = k.Classes.Walk(ctx, nil, func(id uint64, c types.Class) (bool, error) {
			if err := c.Flags.Validate(); err != nil {
				problems = append(problems, fmt.Sprintf("class %d: %v", id, err))
			}
			if err := types.ValidateRoyaltyShares(c.RoyaltyRecipients); err != nil {
				problems = append(problems, fmt.Sprintf("class %d: %v", id, err))
			}
			if !c.Flags.Transferable {
				if c.RoyaltyBps > 0 || (!c.MintPolicy.Price.Amount.IsNil() && c.MintPolicy.Price.Amount.IsPositive()) {
					problems = append(problems, fmt.Sprintf("soulbound class %d has royalty or price", id))
				}
				_ = k.Listings.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](id), func(key U64Pair, _ types.Listing) (bool, error) {
					problems = append(problems, fmt.Sprintf("soulbound token %d/%d listed", id, key.K2()))
					return false, nil
				})
				_ = k.PendingTransfers.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](id), func(key U64Pair, _ types.PendingTransfer) (bool, error) {
					problems = append(problems, fmt.Sprintf("soulbound token %d/%d pending transfer", id, key.K2()))
					return false, nil
				})
			}
			return false, nil
		})
		return report("flags", problems)
	}
}

// InboxCounterInvariant: the pair and recipient counters equal the counts
// derived from the pending stores.
func InboxCounterInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		pairs, totals := k.deriveInboxCounters(ctx)
		var problems []string
		_ = k.PendingPairCount.Walk(ctx, nil, func(key collections.Pair[string, string], v uint64) (bool, error) {
			pk := [2]string{key.K1(), key.K2()}
			if pairs[pk] != v {
				problems = append(problems, fmt.Sprintf("pair %s->%s count %d != %d", key.K1(), key.K2(), v, pairs[pk]))
			}
			delete(pairs, pk)
			return false, nil
		})
		for key, v := range pairs {
			problems = append(problems, fmt.Sprintf("pair %s->%s missing counter (%d items)", key[0], key[1], v))
		}
		_ = k.InboxCount.Walk(ctx, nil, func(addr string, v uint64) (bool, error) {
			if totals[addr] != v {
				problems = append(problems, fmt.Sprintf("inbox %s count %d != %d", addr, v, totals[addr]))
			}
			delete(totals, addr)
			return false, nil
		})
		for addr, v := range totals {
			problems = append(problems, fmt.Sprintf("inbox %s missing counter (%d items)", addr, v))
		}
		return report("inbox-counters", problems)
	}
}

// deriveInboxCounters recomputes the inbox counters from the pending stores,
// keyed by [origin, recipient] (collections.Pair holds pointers, so it
// cannot be a map key).
func (k Keeper) deriveInboxCounters(ctx sdk.Context) (map[[2]string]uint64, map[string]uint64) {
	pairs := map[[2]string]uint64{}
	totals := map[string]uint64{}
	_ = k.PendingTransfers.Walk(ctx, nil, func(_ U64Pair, pt types.PendingTransfer) (bool, error) {
		pairs[[2]string{pt.From, pt.To}]++
		totals[pt.To]++
		return false, nil
	})
	_ = k.PendingMints.Walk(ctx, nil, func(_ U64Pair, pm types.PendingMint) (bool, error) {
		pairs[[2]string{types.MintOrigin(pm.ClassId), pm.To}]++
		totals[pm.To]++
		return false, nil
	})
	return pairs, totals
}

// HideConsistencyInvariant: every hidden target has an open hide record and
// every open-record index points at a PENDING record.
func HideConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		_ = k.HideByTarget.Walk(ctx, nil, func(key U64Pair, id uint64) (bool, error) {
			hr, err := k.HideRecords.Get(ctx, id)
			if err != nil || hr.Outcome != types.HideOutcome_HIDE_OUTCOME_PENDING {
				problems = append(problems, fmt.Sprintf("hide index %d/%d -> non-pending record %d", key.K1(), key.K2(), id))
			}
			return false, nil
		})
		_ = k.Classes.Walk(ctx, nil, func(id uint64, c types.Class) (bool, error) {
			if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
				if has, _ := k.HideByTarget.Has(ctx, targetKey(id, 0)); !has && !hiddenByClosedRecord(ctx, k, id, 0) {
					problems = append(problems, fmt.Sprintf("class %d hidden without a hide record", id))
				}
			}
			return false, nil
		})
		_ = k.Tokens.Walk(ctx, nil, func(key U64Pair, t types.Token) (bool, error) {
			if t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
				if has, _ := k.HideByTarget.Has(ctx, key); !has && !hiddenByClosedRecord(ctx, k, t.ClassId, t.Id) {
					problems = append(problems, fmt.Sprintf("token %d/%d hidden without a hide record", t.ClassId, t.Id))
				}
			}
			return false, nil
		})
		return report("hide-consistency", problems)
	}
}

// hiddenByClosedRecord: a target stays HIDDEN after an EXPIRED or
// UPHELD outcome (scrubbed, not restored).
func hiddenByClosedRecord(ctx sdk.Context, k Keeper, classID, tokenID uint64) bool {
	found := false
	_ = k.HidesByTargetAll.Walk(ctx, collections.NewSuperPrefixedTripleRange[uint64, uint64, uint64](classID, tokenID),
		func(key collections.Triple[uint64, uint64, uint64]) (bool, error) {
			hr, err := k.HideRecords.Get(ctx, key.K3())
			if err == nil && (hr.Outcome == types.HideOutcome_HIDE_OUTCOME_EXPIRED || hr.Outcome == types.HideOutcome_HIDE_OUTCOME_UPHELD) {
				found = true
				return true, nil
			}
			return false, nil
		})
	return found
}
