package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// randomEdition picks a random edition matching pred whose class owner is
// a sim account.
func randomEdition(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account,
	pred func(types.Class, types.Edition) bool,
) (types.Class, types.Edition, simtypes.Account, bool) {
	type pick struct {
		c types.Class
		e types.Edition
	}
	var candidates []pick
	_ = k.Editions.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], e types.Edition) (bool, error) {
		c, err := k.Classes.Get(ctx, e.ClassId)
		if err == nil && pred(c, e) {
			candidates = append(candidates, pick{c, e})
		}
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, p := range candidates {
		if acc, ok := findAccount(accs, p.c.Owner); ok {
			return p.c, p.e, acc, true
		}
	}
	return types.Class{}, types.Edition{}, simtypes.Account{}, false
}

// randomBalance picks a random balance matching pred held by a sim account.
func randomBalance(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account,
	pred func(types.Class, types.Balance) bool,
) (types.Class, types.Balance, simtypes.Account, bool) {
	type pick struct {
		c types.Class
		b types.Balance
	}
	var candidates []pick
	_ = k.Balances.Walk(ctx, nil, func(_ keeper.BalanceKey, b types.Balance) (bool, error) {
		c, err := k.Classes.Get(ctx, b.ClassId)
		if err == nil && pred(c, b) {
			candidates = append(candidates, pick{c, b})
		}
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, p := range candidates {
		if acc, ok := findAccount(accs, p.b.Owner); ok {
			return p.c, p.b, acc, true
		}
	}
	return types.Class{}, types.Balance{}, simtypes.Account{}, false
}

func otherAccount(r *rand.Rand, accs []simtypes.Account, not sdk.AccAddress) (simtypes.Account, bool) {
	to, _ := simtypes.RandomAcc(r, accs)
	return to, !to.Address.Equals(not)
}

func collectionsJoin(a, b uint64) collections.Pair[uint64, uint64] { return collections.Join(a, b) }

type collectionsStringPair = collections.Pair[string, uint64]

func collectionsTriple(a, b uint64, c string) collections.Triple[uint64, uint64, string] {
	return collections.Join3(a, b, c)
}

func collectionsStringPrefix(s string) *collections.PairRange[string, uint64] {
	return collections.NewPrefixedPairRange[string, uint64](s)
}

func collectionsClassRange(classID uint64) *collections.PairRange[uint64, uint64] {
	return collections.NewPrefixedPairRange[uint64, uint64](classID)
}
