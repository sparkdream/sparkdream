package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgPublicMint buys one token from a random class with public mint on.
func SimulateMsgPublicMint(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgPublicMint{}
		var classes []types.Class
		_ = k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
			if c.MintPolicy.PublicMintEnabled && !c.MintingClosed && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE &&
				(c.MaxSupply == 0 || c.Supply+c.Burned+c.ReservedSupply < c.MaxSupply) {
				classes = append(classes, c)
			}
			return false, nil
		})
		if len(classes) == 0 {
			return noop(msg, "no class open for public mint")
		}
		c := classes[r.Intn(len(classes))]
		buyer, _ := simtypes.RandomAcc(r, accs)
		if c.MintPolicy.PerAddressLimit > 0 {
			used, _ := k.PublicMintCount.Get(ctx, collections.Join(c.Id, buyer.Address.String()))
			if used >= uint64(c.MintPolicy.PerAddressLimit) {
				return noop(msg, "per-address limit reached")
			}
		}
		msg = &types.MsgPublicMint{Buyer: buyer.Address.String(), ClassId: c.Id, Quantity: 1, MaxPrice: c.MintPolicy.Price}
		return deliver(r, app, ctx, ak, bk, txGen, buyer, msg, c.MintPolicy.Price.Amount.Add(k.GetParams(ctx).TokenDeposit))
	}
}
