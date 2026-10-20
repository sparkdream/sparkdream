package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgRenew renews a random expiring token, as its holder (paying
// the class renewal price) or as the class owner (free).
func SimulateMsgRenew(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRenew{}
		t, holder, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Expiry.Expires() && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no expiring token owned by a sim account")
		}
		c, _ := k.Classes.Get(ctx, t.ClassId)
		signer, spend := holder, c.Expiry.RenewalPrice.Amount
		if owner, found := findAccount(accs, c.Owner); found && r.Intn(2) == 0 {
			signer, spend = owner, spend.SubRaw(spend.Int64())
		}
		msg = &types.MsgRenew{Signer: signer.Address.String(), ClassId: t.ClassId, TokenId: t.Id, MaxPrice: c.Expiry.RenewalPrice}
		return deliver(r, app, ctx, ak, bk, txGen, signer, msg, spend)
	}
}
