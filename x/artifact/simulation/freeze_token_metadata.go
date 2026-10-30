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

// SimulateMsgFreezeTokenMetadata freezes a random unfrozen unique token's metadata (signed by its holder).
func SimulateMsgFreezeTokenMetadata(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFreezeTokenMetadata{}
		t, holder, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return !c.IsEditions() && !t.MetadataFrozen
		})
		if !ok {
			return noop(msg, "no unfrozen token")
		}
		msg = &types.MsgFreezeTokenMetadata{Signer: holder.Address.String(), Refs: []types.TokenRef{{ClassId: t.ClassId, TokenId: t.Id}}}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}
