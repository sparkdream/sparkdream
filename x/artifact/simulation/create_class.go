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

// SimulateMsgCreateClass creates a class from a random sim account that is
// an active x/rep member (class creation is member-gated).
func SimulateMsgCreateClass(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCreateClass{}
		rk := k.GetRepKeeper()
		if rk == nil {
			return noop(msg, "rep keeper not wired")
		}
		p := k.GetParams(ctx)
		var acc simtypes.Account
		found := false
		for _, i := range r.Perm(len(accs)) {
			if !rk.IsActiveMember(ctx, accs[i].Address) {
				continue
			}
			if lvl, err := rk.GetTrustLevel(ctx, accs[i].Address); err == nil && uint32(lvl) >= p.MinTrustLevelCreateClass {
				acc, found = accs[i], true
				break
			}
		}
		if !found {
			return noop(msg, "no sim account is a member at the required trust level")
		}
		if count, _ := k.ClassCountByCreator.Get(ctx, acc.Address.String()); count >= uint64(p.MaxClassesPerCreator) {
			return noop(msg, "creator class cap reached")
		}
		soulbound := r.Intn(4) == 0
		burnAuth := types.BurnAuthorization_BURN_AUTHORIZATION_HOLDER
		if soulbound {
			burnAuth = types.BurnAuthorization(r.Intn(3)) // any mode is valid on a soulbound class
		}
		msg = &types.MsgCreateClass{
			Creator:                acc.Address.String(),
			Name:                   "Phoenix " + simtypes.RandStringOfLength(r, 6),
			Symbol:                 "PHX",
			Description:            "simulated class",
			Uri:                    "ipfs://bafy" + simtypes.RandStringOfLength(r, 16),
			Flags:                  types.ClassFlags{Transferable: !soulbound, BurnAuthorization: burnAuth, TokenMetadataMutable: r.Intn(2) == 0},
			MaxSupply:              uint64(r.Intn(3)) * 50,
			AcceptedContentLicense: types.ContentLicense,
		}
		if !soulbound {
			msg.RoyaltyBps = uint32(r.Intn(int(p.MaxRoyaltyBps) + 1))
			msg.MintPolicy = types.MintPolicy{PublicMintEnabled: true, Price: sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(r.Intn(3))*1_000_000)}
		}
		return deliver(r, app, ctx, ak, bk, txGen, acc, msg, p.ClassCreationFee)
	}
}
