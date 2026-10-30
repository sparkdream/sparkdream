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

// SimulateMsgUpdateToken rewrites a random mutable token's metadata (signed by the class owner).
func SimulateMsgUpdateToken(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateToken{}
		t, owner, ok := randomTokenOfClassOwner(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return !c.IsEditions() && c.Flags.TokenMetadataMutable && !c.MetadataFrozen &&
				c.Status != types.ContentStatus_CONTENT_STATUS_HIDDEN && !t.MetadataFrozen &&
				t.Status != types.ContentStatus_CONTENT_STATUS_HIDDEN && !t.Unrevealed
		})
		if !ok {
			return noop(msg, "no mutable token")
		}
		msg = &types.MsgUpdateToken{Owner: owner.Address.String(), ClassId: t.ClassId, TokenId: t.Id, Metadata: randomMetadata(r)}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
