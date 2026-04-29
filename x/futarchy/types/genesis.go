package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:    DefaultParams(),
		MarketMap: []Market{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	marketIndexMap := make(map[string]struct{})

	for _, elem := range gs.MarketMap {
		index := fmt.Sprint(elem.Index)
		if _, ok := marketIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for market")
		}
		marketIndexMap[index] = struct{}{}

		// All LMSR pointer fields must be populated — a nil here would panic on resolve.
		if elem.BValue == nil {
			return fmt.Errorf("market %d: b_value is nil", elem.Index)
		}
		if elem.PoolYes == nil {
			return fmt.Errorf("market %d: pool_yes is nil", elem.Index)
		}
		if elem.PoolNo == nil {
			return fmt.Errorf("market %d: pool_no is nil", elem.Index)
		}
		if elem.MinTick == nil {
			return fmt.Errorf("market %d: min_tick is nil", elem.Index)
		}
		if elem.InitialLiquidity == nil {
			return fmt.Errorf("market %d: initial_liquidity is nil", elem.Index)
		}
		if elem.LiquidityWithdrawn == nil {
			return fmt.Errorf("market %d: liquidity_withdrawn is nil", elem.Index)
		}

		if !elem.BValue.IsPositive() {
			return fmt.Errorf("market %d: b_value must be positive", elem.Index)
		}
		if elem.PoolYes.IsNegative() {
			return fmt.Errorf("market %d: pool_yes must be non-negative", elem.Index)
		}
		if elem.PoolNo.IsNegative() {
			return fmt.Errorf("market %d: pool_no must be non-negative", elem.Index)
		}
		if elem.InitialLiquidity.IsNegative() {
			return fmt.Errorf("market %d: initial_liquidity must be non-negative", elem.Index)
		}
		if elem.LiquidityWithdrawn.IsNegative() {
			return fmt.Errorf("market %d: liquidity_withdrawn must be non-negative", elem.Index)
		}
		if elem.LiquidityWithdrawn.GT(*elem.InitialLiquidity) {
			return fmt.Errorf("market %d: liquidity_withdrawn exceeds initial_liquidity", elem.Index)
		}
	}

	return gs.Params.Validate()
}
