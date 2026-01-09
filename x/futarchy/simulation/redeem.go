package simulation

import (
	"fmt"
	"math/rand"
	"strings"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
)

func SimulateMsgRedeem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		var targetMarket types.Market
		found := false

		// 1. Walk markets to find a redeemable one
		err := k.Market.Walk(ctx, nil, func(key uint64, market types.Market) (bool, error) {
			// Check if resolved
			if !strings.HasPrefix(market.Status, "RESOLVED_") {
				return false, nil
			}

			// Check redemption delay
			unlockHeight := market.ResolutionHeight + market.RedemptionBlocks
			if ctx.BlockHeight() < unlockHeight {
				return false, nil
			}

			targetMarket = market
			found = true
			return true, nil // Stop iteration
		})
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Error walking markets"), nil, err
		}

		if !found {
			// Create a redeemable market if one isn't found (for testing purposes)
			simAccount, _ := simtypes.RandomAcc(r, accs)

			// Get next ID
			id, err := k.MarketSeq.Next(ctx)
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Error generating market ID"), nil, err
			}

			bVal := math.LegacyMustNewDecFromStr("1000")
			zeroInt := math.ZeroInt()
			minTick := math.NewInt(1000)

			// Create Market
			targetMarket = types.Market{
				Index:              id,
				Denom:              "stake", // Default sim denom
				Creator:            simAccount.Address.String(),
				Symbol:             "SIM",
				Question:           "Simulation Question",
				EndBlock:           ctx.BlockHeight(),
				RedemptionBlocks:   0,
				ResolutionHeight:   ctx.BlockHeight(),
				Status:             "RESOLVED_YES",
				BValue:             &bVal,
				PoolYes:            &zeroInt,
				PoolNo:             &zeroInt,
				MinTick:            &minTick,
				InitialLiquidity:   &zeroInt,
				LiquidityWithdrawn: &zeroInt,
			}

			if err := k.Market.Set(ctx, id, targetMarket); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Error setting market"), nil, err
			}

			// Mint Collateral to Module (so it can pay out)
			// We'll mint enough for the shares we are about to give
			collateral := sdk.NewCoins(sdk.NewCoin("stake", math.NewInt(1000000)))
			if err := bk.MintCoins(ctx, types.ModuleName, collateral); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Error minting collateral"), nil, err
			}

			// Mint Winning Shares to User
			shareDenom := fmt.Sprintf("f/%d/yes", id)
			shares := sdk.NewCoins(sdk.NewCoin(shareDenom, math.NewInt(1000000)))

			// Mint to module first then send to user
			if err := bk.MintCoins(ctx, types.ModuleName, shares); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Error minting shares"), nil, err
			}
			if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, simAccount.Address, shares); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Error sending shares"), nil, err
			}

			found = true
		}

		// 2. Determine winner and share denom
		winner := ""
		switch targetMarket.Status {
		case "RESOLVED_YES":
			winner = "yes"
		case "RESOLVED_NO":
			winner = "no"
		default:
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "Invalid resolved status"), nil, nil
		}

		shareDenom := fmt.Sprintf("f/%d/%s", targetMarket.Index, winner)

		// 3. Find an account that has winning shares
		var simAccount simtypes.Account
		var hasShares bool

		// Shuffle accounts to avoid bias
		r.Shuffle(len(accs), func(i, j int) { accs[i], accs[j] = accs[j], accs[i] })

		for _, acc := range accs {
			balance := bk.GetBalance(ctx, acc.Address, shareDenom)
			if balance.Amount.IsPositive() {
				simAccount = acc
				hasShares = true
				break
			}
		}

		if !hasShares {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "No account has winning shares"), nil, nil
		}

		// 4. Construct MsgRedeem
		msg := &types.MsgRedeem{
			Creator:  simAccount.Address.String(),
			MarketId: targetMarket.Index,
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}
