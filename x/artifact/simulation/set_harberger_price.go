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

// SimulateMsgSetHarbergerPrice abandons (prices at zero) a random Harberger
// token. Positive prices need an x/session RecurringPull, which the
// artifact simulator does not create; unit and e2e tests cover them.
func SimulateMsgSetHarbergerPrice(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetHarbergerPrice{}
		t, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, _ types.Token) bool { return c.IsHarberger() })
		if !ok || r.Intn(4) != 0 {
			return noop(msg, "no harberger token picked")
		}
		msg = &types.MsgSetHarbergerPrice{Owner: t.Owner, ClassId: t.ClassId, TokenId: t.Id, Price: math.ZeroInt()}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
