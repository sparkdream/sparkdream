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

// SimulateMsgCreateEdition adds an edition (a third of them impact
// certificates) to a random open EDITIONS class.
func SimulateMsgCreateEdition(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCreateEdition{}
		var c types.Class
		var owner simtypes.Account
		found := false
		_ = k.Classes.Walk(ctx, nil, func(_ uint64, cl types.Class) (bool, error) {
			if cl.IsEditions() && cl.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && !cl.MintingClosed &&
				(cl.MaxSupply == 0 || cl.Supply < cl.MaxSupply) {
				if acc, ok := findAccount(accs, cl.Owner); ok && r.Intn(2) == 0 {
					c, owner, found = cl, acc, true
				}
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no open editions class")
		}
		msg = &types.MsgCreateEdition{Minter: owner.Address.String(), ClassId: c.Id, Metadata: randomMetadata(r),
			MaxSupply: uint64(r.Intn(3)) * 500, AcceptedContentLicense: types.ContentLicense}
		if r.Intn(3) == 0 {
			msg.Claim = types.ImpactClaim{Scope: "simulated work " + simtypes.RandStringOfLength(r, 6),
				Contributors: []string{owner.Address.String()}, WorkStart: 1, WorkEnd: 2}
		}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, k.GetParams(ctx).TokenDeposit)
	}
}
