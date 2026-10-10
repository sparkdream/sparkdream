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

// SimulateMsgMint mints 1-3 tokens of a random open class to random accounts.
func SimulateMsgMint(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMint{}
		c, owner, ok := randomOpenClass(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no open class owned by a sim account")
		}
		n := 1 + r.Intn(3)
		if c.MaxSupply > 0 && c.Supply+c.Burned+c.ReservedSupply+uint64(n) > c.MaxSupply {
			return noop(msg, "class supply exhausted")
		}
		entries := make([]types.MintEntry, n)
		for i := range entries {
			to, _ := simtypes.RandomAcc(r, accs)
			entries[i] = types.MintEntry{Recipient: to.Address.String(), Metadata: randomMetadata(r)}
		}
		msg = &types.MsgMint{Minter: owner.Address.String(), ClassId: c.Id, Entries: entries, AcceptedContentLicense: types.ContentLicense}
		p := k.GetParams(ctx)
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, p.TokenDeposit.Add(p.InboxFee).MulRaw(int64(n)))
	}
}
