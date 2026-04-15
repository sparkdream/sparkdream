//go:build testnet

package types

import (
	"cosmossdk.io/math"
)

// Testnet values — 2x devnet epochs, near-production operational thresholds.
// Build with: go build -tags testnet
func getShieldGenesisParams() Params {
	return Params{
		Enabled:                     true,
		MaxFundingPerDay:            math.NewInt(200_000_000), // 200 SPARK/day
		MinGasReserve:               math.NewInt(100_000_000), // 100 SPARK (= production)
		MaxGasPerExec:               500_000,
		MaxExecsPerIdentityPerEpoch: 50,
		EncryptedBatchEnabled:       false,
		ShieldEpochInterval:         50, // ~5 min at 6s blocks (= production)
		MinBatchSize:                3,  // = production
		MaxPendingEpochs:            6,
		MaxPendingQueueSize:         1000, // = production
		MaxEncryptedPayloadSize:     16384,
		MaxOpsPerBatch:              100,
		TleMissWindow:               100, // = production
		TleMissTolerance:            10,  // = production (2x devnet's 7 ≈ 14, capped at production)
		TleJailDuration:             600, // 10 min (= production)
		MinTleValidators:            5,   // = production (2x devnet's 4 = 8, capped at production)
		DkgWindowBlocks:             200, // ~20 min (= production)
		MaxValidatorSetDrift:        33,
	}
}
