package keeper

import (
	"fmt"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// CreateMarketInternal allows other modules to create markets programmatically.
func (k Keeper) CreateMarketInternal(ctx sdk.Context, creator sdk.AccAddress, symbol string, question string, durationBlocks int64, redemptionBlocks int64, liquidity sdk.Coin) (uint64, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return 0, err
	}

	// Validate liquidity meets minimum
	if liquidity.Amount.LT(params.MinLiquidity) {
		return 0, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"liquidity %s is below minimum %s", liquidity.Amount.String(), params.MinLiquidity.String())
	}

	// Validate duration
	if durationBlocks <= 0 {
		return 0, types.ErrInvalidDuration
	}
	if durationBlocks > params.MaxDuration {
		return 0, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"duration %d exceeds maximum %d", durationBlocks, params.MaxDuration)
	}

	// Validate redemption delay
	if redemptionBlocks < 0 {
		return 0, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "redemption_blocks must be non-negative")
	}
	if redemptionBlocks > params.MaxRedemptionDelay {
		return 0, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest,
			"redemption delay %d exceeds maximum %d", redemptionBlocks, params.MaxRedemptionDelay)
	}

	// 1. Transfer Subsidy (Creator -> Module)
	// For x/commons, 'creator' will be the x/commons Module Account
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, types.ModuleName, sdk.NewCoins(liquidity)); err != nil {
		return 0, err
	}

	// 2. Calculate b (LMSR Constant)
	ln2 := math.LegacyMustNewDecFromStr("0.69314718")
	amountDec := math.LegacyNewDecFromInt(liquidity.Amount)
	bValue := amountDec.Quo(ln2)

	// 3. Generate ID
	id, err := k.MarketSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	// 4. Initialize Market
	endBlock := ctx.BlockHeight() + durationBlocks

	market := types.Market{
		Index:              id,
		Denom:              liquidity.Denom,
		Creator:            creator.String(),
		Symbol:             symbol,
		Question:           question,
		EndBlock:           endBlock,
		RedemptionBlocks:   redemptionBlocks,
		ResolutionHeight:   0,
		BValue:             bValue.String(),
		PoolYes:            "0",
		PoolNo:             "0",
		MinTick:            params.DefaultMinTick.String(),
		Status:             "ACTIVE",
		InitialLiquidity:   liquidity.Amount.String(),
		LiquidityWithdrawn: "0",
	}

	// 5. Save Market
	if err := k.Market.Set(ctx, id, market); err != nil {
		return 0, err
	}

	// 6. Add to Active Index (So it resolves automatically)
	if err := k.ActiveMarkets.Set(ctx, collections.Join(endBlock, id)); err != nil {
		return 0, err
	}

	// 7. Emit Event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"market_created",
			sdk.NewAttribute("market_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("creator", creator.String()),
			sdk.NewAttribute("symbol", symbol),
			sdk.NewAttribute("liquidity", liquidity.String()),
		),
	)

	return id, nil
}
