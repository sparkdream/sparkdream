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

// SimulateMsgTransfer moves a random unlocked, transferable token.
func SimulateMsgTransfer(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgTransfer{}
		t, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Flags.Transferable && t.Lock == types.TokenLock_TOKEN_LOCK_NONE
		})
		if !ok {
			return noop(msg, "no transferable token")
		}
		to, _ := simtypes.RandomAcc(r, accs)
		if to.Address.Equals(owner.Address) {
			return noop(msg, "picked self as recipient")
		}
		msg = &types.MsgTransfer{Sender: owner.Address.String(), Entries: []types.TransferEntry{{ClassId: t.ClassId, TokenId: t.Id, Recipient: to.Address.String()}}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, k.GetParams(ctx).InboxFee)
	}
}

// SimulateMsgBurn burns a random unlocked token or one that is listed.
func SimulateMsgBurn(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgBurn{}
		t, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Flags.HolderMayBurn() && t.Lock != types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER
		})
		if !ok {
			return noop(msg, "no burnable token")
		}
		msg = &types.MsgBurn{Owner: owner.Address.String(), Refs: []types.TokenRef{{ClassId: t.ClassId, TokenId: t.Id}}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}
