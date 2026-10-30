package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// simFee is an explicit fee high enough for the ante handler.
var simFee = math.NewInt(5_000_000)

// deliver signs and delivers msg from acc with an explicit fee, after
// checking the account can pay fee + spend.
func deliver(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, ak types.AuthKeeper, bk types.BankKeeper,
	txGen client.TxConfig, acc simtypes.Account, msg sdk.Msg, spend math.Int,
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	denom := sdk.DefaultBondDenom
	if bk.SpendableCoins(ctx, acc.Address).AmountOf(denom).LT(simFee.Add(spend)) {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "insufficient funds"), nil, nil
	}
	// Final gate: run the msg through its real handler on a throwaway cache
	// so a precheck that drifts from the handler is a NoOp, never a delivered
	// tx the handler rejects (which fails the whole simulation).
	if reason := dryRun(app, ctx, msg); reason != "" {
		return noop(msg, "handler would reject: "+reason)
	}
	op := simulation.OperationInput{
		R:             r,
		App:           app,
		TxGen:         txGen,
		Msg:           msg,
		Context:       ctx,
		SimAccount:    acc,
		AccountKeeper: ak,
		Bankkeeper:    bk,
		ModuleName:    types.ModuleName,
	}
	return simulation.GenAndDeliverTx(op, sdk.NewCoins(sdk.NewCoin(denom, simFee)))
}

// dryRun executes msg through the app's msg router on a cache context and
// returns the handler's error text ("" when it would succeed).
func dryRun(app *baseapp.BaseApp, ctx sdk.Context, msg sdk.Msg) (reason string) {
	handler := app.MsgServiceRouter().Handler(msg)
	if handler == nil {
		return "no handler"
	}
	cacheCtx, _ := ctx.CacheContext()
	defer func() {
		if p := recover(); p != nil {
			reason = fmt.Sprintf("panic: %v", p)
		}
	}()
	if _, err := handler(cacheCtx, msg); err != nil {
		return err.Error()
	}
	return ""
}

func noop(msg sdk.Msg, reason string) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), reason), nil, nil
}

func findAccount(accs []simtypes.Account, addr string) (simtypes.Account, bool) {
	for _, a := range accs {
		if a.Address.String() == addr {
			return a, true
		}
	}
	return simtypes.Account{}, false
}

// randomToken picks a random token matching pred whose owner is a sim account.
func randomToken(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account,
	pred func(types.Class, types.Token) bool,
) (types.Token, simtypes.Account, bool) {
	var candidates []types.Token
	_ = k.Tokens.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], t types.Token) (bool, error) {
		c, err := k.Classes.Get(ctx, t.ClassId)
		if err == nil && pred(c, t) {
			candidates = append(candidates, t)
		}
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, t := range candidates {
		if acc, ok := findAccount(accs, t.Owner); ok {
			return t, acc, true
		}
	}
	return types.Token{}, simtypes.Account{}, false
}

// randomClassOwnedBy picks a random ACTIVE, open class owned by a sim account.
func randomOpenClass(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account) (types.Class, simtypes.Account, bool) {
	var candidates []types.Class
	_ = k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
		if c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && !c.MintingClosed {
			candidates = append(candidates, c)
		}
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, c := range candidates {
		if acc, ok := findAccount(accs, c.Owner); ok {
			return c, acc, true
		}
	}
	return types.Class{}, simtypes.Account{}, false
}

func randomMetadata(r *rand.Rand) types.TokenMetadata {
	return types.TokenMetadata{
		Name:       "aurora-" + simtypes.RandStringOfLength(r, 6),
		Uri:        fmt.Sprintf("ipfs://bafy%s", simtypes.RandStringOfLength(r, 20)),
		Attributes: []types.Attribute{{Key: "seed", Value: simtypes.RandStringOfLength(r, 4)}},
	}
}

// randomClassWhere picks a random class matching pred whose owner is a sim
// account.
func randomClassWhere(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account,
	pred func(types.Class) bool,
) (types.Class, simtypes.Account, bool) {
	var candidates []types.Class
	_ = k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
		if pred(c) {
			candidates = append(candidates, c)
		}
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, c := range candidates {
		if acc, ok := findAccount(accs, c.Owner); ok {
			return c, acc, true
		}
	}
	return types.Class{}, simtypes.Account{}, false
}

// randomTokenOfClassOwner picks a random token matching pred whose class
// owner is a sim account, and returns that owner.
func randomTokenOfClassOwner(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account,
	pred func(types.Class, types.Token) bool,
) (types.Token, simtypes.Account, bool) {
	type pick struct {
		t     types.Token
		owner string
	}
	var candidates []pick
	_ = k.Tokens.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], t types.Token) (bool, error) {
		c, err := k.Classes.Get(ctx, t.ClassId)
		if err == nil && pred(c, t) {
			candidates = append(candidates, pick{t, c.Owner})
		}
		return len(candidates) >= 200, nil
	})
	r.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	for _, p := range candidates {
		if acc, ok := findAccount(accs, p.owner); ok {
			return p.t, acc, true
		}
	}
	return types.Token{}, simtypes.Account{}, false
}

// randomPendingOwner picks a random pending class-owner proposal matching
// pred.
func randomPendingOwner(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, pred func(types.PendingClassOwner) bool) (types.PendingClassOwner, bool) {
	var candidates []types.PendingClassOwner
	_ = k.PendingClassOwners.Walk(ctx, nil, func(_ uint64, po types.PendingClassOwner) (bool, error) {
		if pred(po) {
			candidates = append(candidates, po)
		}
		return len(candidates) >= 200, nil
	})
	if len(candidates) == 0 {
		return types.PendingClassOwner{}, false
	}
	return candidates[r.Intn(len(candidates))], true
}
