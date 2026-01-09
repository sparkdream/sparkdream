package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
)

func SimulateMsgTrade(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		var targetMarket types.Market
		found := false

		// 1. Find Active Market
		err := k.Market.Walk(ctx, nil, func(key uint64, market types.Market) (bool, error) {
			if market.Status == "ACTIVE" && ctx.BlockHeight() < market.EndBlock {
				// Safety Check: BValue must be sufficient to avoid LMSR numerical instability
				if market.BValue == nil {
					return false, nil
				}
				bVal := *market.BValue
				// Require B >= 1000 to ensure standard trades don't explode the cost function
				if bVal.LT(math.LegacyNewDec(1000)) {
					return false, nil
				}

				targetMarket = market
				found = true
				return true, nil // stop
			}
			return false, nil
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Error walking markets"), nil, err
		}

		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 2. Create if not found
		if !found {
			id, err := k.MarketSeq.Next(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Error generating ID"), nil, err
			}

			endBlock := ctx.BlockHeight() + 100
			bVal := math.LegacyMustNewDecFromStr("1000000")
			zeroInt := math.ZeroInt()
			minTick := math.NewInt(1000)

			targetMarket = types.Market{
				Index:              id,
				Creator:            simAccount.Address.String(),
				Symbol:             "SIMTRADE",
				Denom:              "stake", // Default sim denom
				Question:           "Will this trade succeed?",
				EndBlock:           endBlock,
				Status:             "ACTIVE",
				BValue:             &bVal,
				PoolYes:            &zeroInt,
				PoolNo:             &zeroInt,
				MinTick:            &minTick,
				InitialLiquidity:   &zeroInt,
				LiquidityWithdrawn: &zeroInt,
			}

			if err := k.Market.Set(ctx, id, targetMarket); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Error setting market"), nil, err
			}

			// Add to ActiveMarkets
			if err := k.ActiveMarkets.Set(ctx, collections.Join(endBlock, id)); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Error setting active market"), nil, err
			}

			found = true
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "No active market"), nil, nil
		}

		// 3. Ensure User has Funds
		balance := bk.GetBalance(ctx, simAccount.Address, targetMarket.Denom)
		if balance.Amount.LT(math.NewInt(2000)) {
			fundAmt := sdk.NewCoins(sdk.NewCoin(targetMarket.Denom, math.NewInt(1000000)))
			if err := bk.MintCoins(ctx, types.ModuleName, fundAmt); err == nil {
				bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, simAccount.Address, fundAmt)
			}
		}

		// Refresh balance
		balance = bk.GetBalance(ctx, simAccount.Address, targetMarket.Denom)
		if balance.Amount.LT(math.NewInt(1100)) {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Balance too low"), nil, nil
		}

		// 4. Construct MsgTrade
		// Cap trade amount to avoid numerical instability in LMSR (Exp function)
		// Safe limit: AmountIn < 10 * BValue
		if targetMarket.BValue == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Invalid market state"), nil, nil
		}
		bVal := *targetMarket.BValue

		safeCap := bVal.MulInt64(10).TruncateInt()

		available := balance.Amount
		maxAmt := available.Quo(math.NewInt(2))

		if maxAmt.GT(safeCap) {
			maxAmt = safeCap
		}

		// Ensure range is valid
		rangeDiff := maxAmt.Int64() - 1000
		if rangeDiff <= 0 {
			// If maxAmt is close to or less than 1000 (MinTick), we can't trade safely/validly
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgTrade{}), "Trade amount unsafe or too small"), nil, nil
		}

		randAmt := r.Int63n(rangeDiff) + 1100
		tradeAmount := math.NewInt(randAmt)
		tradeCoin := sdk.NewCoin(targetMarket.Denom, tradeAmount)

		msg := &types.MsgTrade{
			Creator:  simAccount.Address.String(),
			MarketId: targetMarket.Index,
			IsYes:    r.Intn(2) == 0,
			AmountIn: &tradeCoin.Amount,
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(tradeCoin),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
