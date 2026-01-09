package types

import (
	"cosmossdk.io/collections"
	"cosmossdk.io/math"
)

const (
	// ModuleName defines the module name
	ModuleName = "futarchy"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// Collections Prefixes
var (
	ParamsKey        = collections.NewPrefix("p_futarchy")
	MarketSeqKey     = collections.NewPrefix("marketseq/value/")
	ActiveMarketsKey = collections.NewPrefix("active_markets/")
)

// Parameter keys
var (
	KeyMinLiquidity       = []byte("MinLiquidity")
	KeyMaxDuration        = []byte("MaxDuration")
	KeyDefaultMinTick     = []byte("DefaultMinTick")
	KeyMaxRedemptionDelay = []byte("MaxRedemptionDelay")
	KeyTradingFeeBps      = []byte("TradingFeeBps")
	KeyMaxLmsrExponent    = []byte("MaxLmsrExponent")
)

// Default parameter values
var (
	DefaultMinLiquidity       = math.NewInt(100000) // Minimum 100,000 base units for market creation
	DefaultMaxDuration        = int64(5256000)      // ~1 year in blocks (assuming 6s blocks)
	DefaultMinTick            = math.NewInt(1000)   // Minimum trade size of 1000 base units
	DefaultMaxRedemptionDelay = int64(5256000)      // Maximum ~1 year redemption delay
	DefaultTradingFeeBps      = uint64(30)          // 0.3% trading fee (30 basis points)
	DefaultMaxLmsrExponent    = "20"                // Maximum exponent value for LMSR to prevent overflow
)
