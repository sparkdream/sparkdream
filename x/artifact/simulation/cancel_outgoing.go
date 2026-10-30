package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgCancelOutgoing cancels a random item in a sim account's outbox.
func SimulateMsgCancelOutgoing(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelOutgoing{}
		r.Shuffle(len(accs), func(i, j int) { accs[i], accs[j] = accs[j], accs[i] })
		for _, acc := range accs {
			var refs []types.TokenRef
			_ = k.OutboxBySender.Walk(ctx, collections.NewPrefixedTripleRange[string, uint64, uint64](acc.Address.String()),
				func(key collections.Triple[string, uint64, uint64], _ uint64) (bool, error) {
					refs = append(refs, types.TokenRef{ClassId: key.K2(), TokenId: key.K3()})
					return true, nil
				})
			if len(refs) > 0 {
				msg = &types.MsgCancelOutgoing{Signer: acc.Address.String(), Refs: refs}
				return deliver(r, app, ctx, ak, bk, txGen, acc, msg, math.ZeroInt())
			}
		}
		return noop(msg, "no outgoing item")
	}
}
