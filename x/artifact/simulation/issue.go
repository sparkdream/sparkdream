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

// SimulateMsgIssue issues one badge metadata to 1-5 distinct random accounts
// from a random open class.
func SimulateMsgIssue(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgIssue{}
		c, owner, ok := randomOpenClass(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no open class owned by a sim account")
		}
		if c.IsEditions() {
			return noop(msg, "editions classes mint through MsgCreateEdition and MsgMintUnits")
		}
		if c.Reveal.Kind == types.RevealKind_REVEAL_KIND_BLIND_METADATA {
			return noop(msg, "blind classes mint empty metadata through MsgMint")
		}
		n := 1 + r.Intn(5)
		if c.MaxSupply > 0 && c.Supply+c.Burned+c.ReservedSupply+uint64(n) > c.MaxSupply {
			return noop(msg, "class supply exhausted")
		}
		recipients := make([]string, 0, n)
		for _, i := range r.Perm(len(accs)) {
			if len(recipients) == n {
				break
			}
			if c.Flags.Mutual && accs[i].Address.Equals(owner.Address) {
				continue
			}
			recipients = append(recipients, accs[i].Address.String())
		}
		msg = &types.MsgIssue{Minter: owner.Address.String(), ClassId: c.Id, Recipients: recipients,
			Metadata: types.TokenMetadata{Name: "attendance-" + simtypes.RandStringOfLength(r, 4)}, AcceptedContentLicense: types.ContentLicense}
		p := k.GetParams(ctx)
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, p.TokenDeposit.Add(p.InboxFee).MulRaw(int64(len(recipients))))
	}
}
