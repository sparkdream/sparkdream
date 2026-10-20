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
		now := ctx.BlockTime().Unix()
		var classes []types.Class
		_ = k.Classes.Walk(ctx, nil, func(_ uint64, c types.Class) (bool, error) {
			mp := c.MintPolicy
			if mp.PublicMintEnabled && !c.MintingClosed && !c.Flags.Mutual && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE &&
				(mp.StartTime == 0 || mp.StartTime <= now) && (mp.EndTime == 0 || now < mp.EndTime) &&
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
		// Price along the curve; max_price must cover the unit sold now.
		total, maxUnit := types.BatchPrice(c.MintPolicy, c.PublicMinted, 1, now)
		msg = &types.MsgPublicMint{Buyer: buyer.Address.String(), ClassId: c.Id, Quantity: 1, MaxPrice: sdk.NewCoin(c.MintPolicy.Price.Denom, maxUnit)}
		return deliver(r, app, ctx, ak, bk, txGen, buyer, msg, total.Add(k.GetParams(ctx).TokenDeposit))
	}
}
