//go:build testparams

package types

import (
	"cosmossdk.io/math"
)

// Testing values — adjusted for faster epoch cycles during integration tests.
// Build with: go build -tags testparams
func getShieldGenesisParams() Params {
	return Params{
		Enabled:                     true,
		MaxFundingPerDay:            math.NewInt(200_000_000), // 200 SPARK/day
		MinGasReserve:               math.NewInt(10_000_000),  // 10 SPARK (lower for testing)
		MaxGasPerExec:               500_000,
		MaxExecsPerIdentityPerEpoch: 50,
		EncryptedBatchEnabled:       false,
		ShieldEpochInterval:         10, // ~1 minute at 6s blocks (production: 50)
		MinBatchSize:                1,  // Execute even single ops (production: 3)
		MaxPendingEpochs:            6,
		MaxPendingQueueSize:         100, // Smaller for testing (production: 1000)
		MaxEncryptedPayloadSize:     16384,
		MaxOpsPerBatch:              100,
		TleMissWindow:               20, // Shorter window (production: 100)
		TleMissTolerance:            5,  // Lower tolerance (production: 10)
		TleJailDuration:             60, // 1 minute (production: 600)
		MinTleValidators:            3,  // Low threshold for testing (production: 5)
		DkgWindowBlocks:             20, // Short window for testing (production: 200)
		MaxValidatorSetDrift:        33,
	}
}
