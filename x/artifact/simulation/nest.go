package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgNest nests one unlocked token of a holder into another of
// theirs (transferable tokens only, so the soulbound rule never bites).
func SimulateMsgNest(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgNest{}
		child, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Flags.Transferable && !c.IsHarberger() && t.Lock == types.TokenLock_TOKEN_LOCK_NONE && t.Descendants == 0
		})
		if !ok {
			return noop(msg, "no nestable child")
		}
		var parent types.Token
		found := false
		_ = k.TokensByOwner.Walk(ctx, nil, func(key keeper.AddrTokenKey) (bool, error) {
			if key.K1() != child.Owner || (key.K2() == child.ClassId && key.K3() == child.Id) {
				return false, nil
			}
			t, err := k.Tokens.Get(ctx, keeper.U64Pair(collectionsJoin(key.K2(), key.K3())))
			if err != nil || t.IsNested() || t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
				return false, nil
			}
			if c, err := k.Classes.Get(ctx, t.ClassId); err != nil || c.IsHarberger() {
				return false, nil
			}
			parent, found = t, true
			return true, nil
		})
		if !found {
			return noop(msg, "no parent for the child")
		}
		msg = &types.MsgNest{Owner: child.Owner, Child: types.TokenRef{ClassId: child.ClassId, TokenId: child.Id},
			Parent: types.TokenRef{ClassId: parent.ClassId, TokenId: parent.Id}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
