//go:build devnet

package types

import (
	"cosmossdk.io/math"
)

// Devnet values — faster epochs with realistic operational thresholds.
// Build with: go build -tags devnet
func getShieldGenesisParams() Params {
	return Params{
		Enabled:                     true,
		MaxFundingPerDay:            math.NewInt(200_000_000), // 200 SPARK/day
		MinGasReserve:               math.NewInt(50_000_000),  // 50 SPARK (production: 100)
		MaxGasPerExec:               500_000,
		MaxExecsPerIdentityPerEpoch: 50,
		EncryptedBatchEnabled:       false,
		ShieldEpochInterval:         25, // ~2.5 min at 6s blocks (production: 50)
		MinBatchSize:                2,  // production: 3
		MaxPendingEpochs:            6,
		MaxPendingQueueSize:         500, // production: 1000
		MaxEncryptedPayloadSize:     16384,
		MaxOpsPerBatch:              100,
		TleMissWindow:               50,  // production: 100
		TleMissTolerance:            7,   // production: 10
		TleJailDuration:             300, // 5 min (production: 600)
		MinTleValidators:            4,   // production: 5
		DkgWindowBlocks:             100, // ~10 min (production: 200)
		MaxValidatorSetDrift:        33,
	}
}
