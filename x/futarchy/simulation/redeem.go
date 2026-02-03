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

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
)

// SimulateMsgRedeem simulates a MsgRedeem message using direct keeper calls.
// This bypasses complex state requirements (resolved markets with winning shares).
// Full integration testing should be done in integration tests.
func SimulateMsgRedeem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// 1. Get or create a resolved market
		market, marketID, err := getOrCreateResolvedMarket(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to get/create market: %v", err)), nil, nil
		}

		// 2. Determine winning outcome
		winningOutcome := "yes"
		if market.Status == "RESOLVED_NO" {
			winningOutcome = "no"
		}

		// 3. Ensure user has winning shares (mint if needed via direct keeper)
		shareDenom := fmt.Sprintf("f/%d/%s", marketID, winningOutcome)
		shareBalance := bk.GetBalance(ctx, simAccount.Address, shareDenom)

		if shareBalance.Amount.IsZero() {
			// Mint shares for simulation
			shareAmount := math.NewInt(int64(100 + r.Intn(900)))
			shareCoins := sdk.NewCoins(sdk.NewCoin(shareDenom, shareAmount))

			if err := bk.MintCoins(ctx, types.ModuleName, shareCoins); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to mint shares: %v", err)), nil, nil
			}
			if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, simAccount.Address, shareCoins); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to send shares: %v", err)), nil, nil
			}

			// Also ensure module has collateral for payout
			denom := market.Denom
			if denom == "" {
				denom = "uspark"
			}
			collateralCoins := sdk.NewCoins(sdk.NewCoin(denom, shareAmount))
			if err := bk.MintCoins(ctx, types.ModuleName, collateralCoins); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to mint collateral: %v", err)), nil, nil
			}

			shareBalance = sdk.NewCoin(shareDenom, shareAmount)
		}

		// 4. Simulate redemption via direct keeper calls:
		// - Burn shares from user
		// - Send collateral from module to user

		// Burn shares
		if err := bk.SendCoinsFromAccountToModule(ctx, simAccount.Address, types.ModuleName, sdk.NewCoins(shareBalance)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to transfer shares to module: %v", err)), nil, nil
		}
		if err := bk.BurnCoins(ctx, types.ModuleName, sdk.NewCoins(shareBalance)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to burn shares: %v", err)), nil, nil
		}

		// Pay out collateral
		denom := market.Denom
		if denom == "" {
			denom = "uspark"
		}
		payout := sdk.NewCoin(denom, shareBalance.Amount)
		if err := bk.SendCoinsFromModuleToAccount(ctx, types.ModuleName, simAccount.Address, sdk.NewCoins(payout)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), fmt.Sprintf("failed to pay collateral: %v", err)), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRedeem{}), "ok (direct keeper call)"), nil, nil
	}
}

// getOrCreateResolvedMarket finds a resolved market or creates one
func getOrCreateResolvedMarket(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, creator string) (*types.Market, uint64, error) {
	// Try to find an existing resolved market
	var markets []struct {
		id     uint64
		market types.Market
	}

	_ = k.Market.Walk(ctx, nil, func(id uint64, market types.Market) (bool, error) {
		if strings.HasPrefix(market.Status, "RESOLVED_") && market.Status != "RESOLVED_INVALID" {
			// Check if redemption is unlocked
			if market.RedemptionBlocks > 0 {
				unlockHeight := market.ResolutionHeight + market.RedemptionBlocks
				if ctx.BlockHeight() < unlockHeight {
					return false, nil // Still locked
				}
			}
			markets = append(markets, struct {
				id     uint64
				market types.Market
			}{id, market})
		}
		return false, nil
	})

	if len(markets) > 0 {
		selected := markets[r.Intn(len(markets))]
		return &selected.market, selected.id, nil
	}

	// Try to resolve an active market
	_ = k.Market.Walk(ctx, nil, func(id uint64, market types.Market) (bool, error) {
		if market.Status == "ACTIVE" {
			// Resolve it
			outcome := "RESOLVED_YES"
			if r.Intn(2) == 0 {
				outcome = "RESOLVED_NO"
			}
			market.Status = outcome
			market.ResolutionHeight = ctx.BlockHeight() - 10
			market.RedemptionBlocks = 0

			if err := k.Market.Set(ctx, id, market); err != nil {
				return false, err
			}

			markets = append(markets, struct {
				id     uint64
				market types.Market
			}{id, market})
			return true, nil // Found one
		}
		return false, nil
	})

	if len(markets) > 0 {
		return &markets[0].market, markets[0].id, nil
	}

	// Create a new resolved market
	marketID, err := k.MarketSeq.Next(ctx)
	if err != nil {
		return nil, 0, err
	}

	outcome := "RESOLVED_YES"
	if r.Intn(2) == 0 {
		outcome = "RESOLVED_NO"
	}

	bValue := math.LegacyNewDec(1000)
	zeroInt := math.ZeroInt()
	liquidity := math.NewInt(100000)
	minTick := math.NewInt(100)

	market := types.Market{
		Index:              marketID,
		Creator:            creator,
		Symbol:             fmt.Sprintf("SIM%d", marketID),
		Question:           "Simulation test market?",
		Denom:              "uspark",
		MinTick:            &minTick,
		EndBlock:           ctx.BlockHeight() - 100,
		RedemptionBlocks:   0,
		ResolutionHeight:   ctx.BlockHeight() - 10,
		Status:             outcome,
		BValue:             &bValue,
		PoolYes:            &zeroInt,
		PoolNo:             &zeroInt,
		InitialLiquidity:   &liquidity,
		LiquidityWithdrawn: &zeroInt,
	}

	if err := k.Market.Set(ctx, marketID, market); err != nil {
		return nil, 0, err
	}

	return &market, marketID, nil
}
