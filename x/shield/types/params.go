package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// Default parameter values per the spec.
var (
	DefaultEnabled                      = true
	DefaultMaxFundingPerDay             = math.NewInt(200_000_000) // 200 SPARK/day
	DefaultMinGasReserve                = math.NewInt(100_000_000) // 100 SPARK
	DefaultMaxGasPerExec         uint64 = 500_000
	DefaultMaxExecsPerIdentity   uint64 = 50
	DefaultEncryptedBatchEnabled        = false // requires DKG ceremony
	DefaultShieldEpochInterval   uint64 = 50    // ~5 minutes at 6s blocks
	DefaultMinBatchSize          uint32 = 3
	DefaultMaxPendingEpochs      uint32 = 6 // ~30 min max wait
	DefaultMaxPendingQueueSize   uint32 = 1000
	DefaultMaxEncryptedPayload   uint32 = 16384 // 16 KB
	DefaultMaxOpsPerBatch        uint32 = 100
	DefaultTLEMissWindow         uint64 = 100
	DefaultTLEMissTolerance      uint64 = 10
	DefaultTLEJailDuration       int64  = 600 // 10 minutes
	DefaultMinTLEValidators      uint32 = 5
	DefaultDKGWindowBlocks       uint64 = 200 // ~20 minutes at 6s blocks
	DefaultMaxValidatorSetDrift  uint32 = 33  // 33% drift triggers re-keying
)

// NewParams creates a new Params instance.
func NewParams() Params {
	return Params{
		Enabled:                     DefaultEnabled,
		MaxFundingPerDay:            DefaultMaxFundingPerDay,
		MinGasReserve:               DefaultMinGasReserve,
		MaxGasPerExec:               DefaultMaxGasPerExec,
		MaxExecsPerIdentityPerEpoch: DefaultMaxExecsPerIdentity,
		EncryptedBatchEnabled:       DefaultEncryptedBatchEnabled,
		ShieldEpochInterval:         DefaultShieldEpochInterval,
		MinBatchSize:                DefaultMinBatchSize,
		MaxPendingEpochs:            DefaultMaxPendingEpochs,
		MaxPendingQueueSize:         DefaultMaxPendingQueueSize,
		MaxEncryptedPayloadSize:     DefaultMaxEncryptedPayload,
		MaxOpsPerBatch:              DefaultMaxOpsPerBatch,
		TleMissWindow:               DefaultTLEMissWindow,
		TleMissTolerance:            DefaultTLEMissTolerance,
		TleJailDuration:             DefaultTLEJailDuration,
		MinTleValidators:            DefaultMinTLEValidators,
		DkgWindowBlocks:             DefaultDKGWindowBlocks,
		MaxValidatorSetDrift:        DefaultMaxValidatorSetDrift,
	}
}

// DefaultParams returns a default set of parameters.
// Uses genesis_vals.go values which may differ between test and production modes.
func DefaultParams() Params {
	return getShieldGenesisParams()
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.MaxFundingPerDay.IsNegative() {
		return fmt.Errorf("max_funding_per_day must be non-negative: %s", p.MaxFundingPerDay)
	}
	if p.MinGasReserve.IsNegative() {
		return fmt.Errorf("min_gas_reserve must be non-negative: %s", p.MinGasReserve)
	}
	if p.MaxGasPerExec == 0 {
		return fmt.Errorf("max_gas_per_exec must be positive")
	}
	if p.MaxExecsPerIdentityPerEpoch == 0 {
		return fmt.Errorf("max_execs_per_identity_per_epoch must be positive")
	}
	if p.ShieldEpochInterval == 0 {
		return fmt.Errorf("shield_epoch_interval must be positive")
	}
	if p.MinBatchSize == 0 {
		return fmt.Errorf("min_batch_size must be positive")
	}
	if p.MaxPendingEpochs == 0 {
		return fmt.Errorf("max_pending_epochs must be positive")
	}
	if p.MaxPendingQueueSize == 0 {
		return fmt.Errorf("max_pending_queue_size must be positive")
	}
	if p.MaxEncryptedPayloadSize == 0 {
		return fmt.Errorf("max_encrypted_payload_size must be positive")
	}
	if p.MaxOpsPerBatch == 0 {
		return fmt.Errorf("max_ops_per_batch must be positive")
	}
	if p.TleMissTolerance > p.TleMissWindow {
		return fmt.Errorf("tle_miss_tolerance (%d) must not exceed tle_miss_window (%d)", p.TleMissTolerance, p.TleMissWindow)
	}
	if p.TleJailDuration < 0 {
		return fmt.Errorf("tle_jail_duration must be non-negative: %d", p.TleJailDuration)
	}
	return nil
}
