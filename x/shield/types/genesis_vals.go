//go:build !testparams

package types

import (
	"cosmossdk.io/math"
)

// Production values for shield genesis params.
// To use relaxed testing values, build with: go build -tags testparams
func getShieldGenesisParams() Params {
	return Params{
		Enabled:                     true,
		MaxFundingPerDay:            math.NewInt(200_000_000), // 200 SPARK/day
		MinGasReserve:               math.NewInt(100_000_000), // 100 SPARK
		MaxGasPerExec:               500_000,
		MaxExecsPerIdentityPerEpoch: 50,
		EncryptedBatchEnabled:       false,
		ShieldEpochInterval:         50, // ~5 minutes at 6s blocks
		MinBatchSize:                3,
		MaxPendingEpochs:            6, // ~30 min max wait
		MaxPendingQueueSize:         1000,
		MaxEncryptedPayloadSize:     16384, // 16 KB
		MaxOpsPerBatch:              100,
		TleMissWindow:               100,
		TleMissTolerance:            10,
		TleJailDuration:             600, // 10 minutes
		MinTleValidators:            5,
		DkgWindowBlocks:             200, // ~20 minutes at 6s blocks
		MaxValidatorSetDrift:        33,  // 33% drift triggers re-keying
	}
}
