package keeper

import (
	"context"
	"sort"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	"cosmossdk.io/math"

	"sparkdream/x/rep/types"
)

// MemberIndexes are the secondary indexes of the Member IndexedMap.
type MemberIndexes struct {
	// MaxRep: (integer part of the member's highest tag reputation, address).
	// Jury selection walks it in descending order from the top down to
	// MinJurorReputation, so the candidate pool is read without touching
	// members that cannot qualify.
	MaxRep *indexes.Multi[uint64, string, types.Member]
}

func (i MemberIndexes) IndexesList() []collections.Index[string, types.Member] {
	return []collections.Index[string, types.Member]{i.MaxRep}
}

func newMemberIndexes(sb *collections.SchemaBuilder) MemberIndexes {
	return MemberIndexes{
		MaxRep: indexes.NewMulti(sb, types.MemberByMaxRepKey, "member_by_max_rep",
			collections.Uint64Key, collections.StringKey,
			func(_ string, m types.Member) (uint64, error) {
				return memberMaxRepBucket(m), nil
			}),
	}
}

// memberMaxRepBucket is the integer part of the member's highest stored tag
// reputation (unparsable or negative scores count as zero).
func memberMaxRepBucket(m types.Member) uint64 {
	best := math.LegacyZeroDec()
	for _, s := range m.ReputationScores {
		d, err := math.LegacyNewDecFromStr(s)
		if err != nil || d.IsNegative() {
			continue
		}
		if d.GT(best) {
			best = d
		}
	}
	ti := best.TruncateInt()
	if !ti.IsUint64() {
		return ^uint64(0)
	}
	return ti.Uint64()
}

// maxJurorCandidates caps how many members a jury draw reads. Candidates are
// read from the MaxRep index in descending reputation order, so the cap keeps
// the most reputable members and never depends on the total member count.
const maxJurorCandidates = 1000

// walkJurorCandidates calls fn for up to maxJurorCandidates members whose
// highest tag reputation is at least minRep (by the index's integer bucket;
// fn applies the exact eligibility rule), in address order so the weighted
// draw is independent of the index layout.
func (k Keeper) walkJurorCandidates(ctx context.Context, minRep math.LegacyDec, fn func(addr string, member types.Member) error) error {
	floor := uint64(0)
	if minRep.IsPositive() {
		ti := minRep.TruncateInt()
		if ti.IsUint64() {
			floor = ti.Uint64()
		} else {
			floor = ^uint64(0)
		}
	}
	rng := new(collections.Range[collections.Pair[uint64, string]]).
		StartInclusive(collections.Join(floor, "")).
		Descending()
	var addrs []string
	if err := k.Member.Indexes.MaxRep.Walk(ctx, rng, func(_ uint64, addr string) (bool, error) {
		addrs = append(addrs, addr)
		return len(addrs) >= maxJurorCandidates, nil
	}); err != nil {
		return err
	}
	sort.Strings(addrs)
	for _, addr := range addrs {
		member, err := k.Member.Get(ctx, addr)
		if err != nil {
			continue
		}
		if err := fn(addr, member); err != nil {
			return err
		}
	}
	return nil
}
