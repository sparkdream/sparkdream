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
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgSetDisplayedMedia switches the displayed version of a random
// token that has accepted media versions.
func SimulateMsgSetDisplayedMedia(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetDisplayedMedia{}
		t, holder, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return len(t.MediaVersions) > 0 && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no token with media versions")
		}
		msg = &types.MsgSetDisplayedMedia{Owner: holder.Address.String(), ClassId: t.ClassId, TokenId: t.Id, Version: commontypes.Saturate[uint32](r.Intn(len(t.MediaVersions) + 1))}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}
