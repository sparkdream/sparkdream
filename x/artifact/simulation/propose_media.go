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

// SimulateMsgProposeMedia offers a random revealed token a new media version
// as its class owner.
func SimulateMsgProposeMedia(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgProposeMedia{}
		t, _, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return !t.Unrevealed && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no token to propose media for")
		}
		c, _ := k.Classes.Get(ctx, t.ClassId)
		owner, found := findAccount(accs, c.Owner)
		if !found {
			return noop(msg, "class owner is not a sim account")
		}
		msg = &types.MsgProposeMedia{Owner: owner.Address.String(), ClassId: t.ClassId, TokenId: t.Id, Metadata: randomMetadata(r)}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
