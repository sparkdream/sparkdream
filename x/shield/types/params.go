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
	// DefaultMaxFeePerExec covers a max_gas_per_exec inner message plus the
	// outer tx overhead (~700k gas) at 0.025 uspark/gas with ~3x headroom.
	DefaultMaxFeePerExec = math.NewInt(50_000) // 0.05 SPARK
	// A completed DKG does not switch batch mode on by itself; governance
	// does, once it is satisfied with the ceremony.
	DefaultEnableBatchAfterDKG = false
	// DefaultMaxFeesPerEpoch caps the fees the module pays per shield epoch:
	// 0.65 SPARK x 288 epochs/day (50-block epochs at 6 s) = 187 SPARK/day,
	// below DefaultMaxFundingPerDay, so the reserve can't be drained faster
	// than it is funded.
	DefaultMaxFeesPerEpoch = math.NewInt(650_000)
	// DefaultBeaconEnabled: once a DKG has completed, every closed epoch's
	// TLE key is demanded and reconstructed (the randomness beacon), batch
	// mode or not. See docs/x-shield-spec.md "Randomness beacon".
	DefaultBeaconEnabled = true
)

// Shares of max_fees_per_epoch (compiled, not params: they bound how the one
// budget param is divided, see docs/x-shield-spec.md "Fee budget").
const (
	// IdentityFeeShareDivisor: one identity (rate-limit nullifier) may have
	// at most max_fees_per_epoch / IdentityFeeShareDivisor paid for it in a
	// shield epoch, and never less than one max_fee_per_exec. One member can
	// no longer take the whole budget; it takes IdentityFeeShareDivisor
	// members acting together.
	IdentityFeeShareDivisor = 10
	// GroupTreeFeeReserveDivisor: max_fees_per_epoch / GroupTreeFeeReserveDivisor
	// is reserved for PROOF_DOMAIN_GROUP_TREE execs (secret-ballot votes and
	// polls). Other execs stop at the rest, so trust-tree traffic cannot
	// starve secret-ballot governance.
	GroupTreeFeeReserveDivisor = 5
)

// IdentityEpochFeeCap is the most the module pays for one identity's execs in
// one shield epoch.
func IdentityEpochFeeCap(p Params) math.Int {
	share := p.MaxFeesPerEpoch.QuoRaw(IdentityFeeShareDivisor)
	if !p.MaxFeePerExec.IsNil() && share.LT(p.MaxFeePerExec) {
		share = p.MaxFeePerExec
	}
	return share
}

// EpochFeeLimit is how far an exec may take the epoch's paid fees: the whole
// budget for a group-tree exec, the budget less the group-tree reserve for
// any other (never below one max_fee_per_exec, capped at the budget).
func EpochFeeLimit(p Params, groupTree bool) math.Int {
	budget := p.MaxFeesPerEpoch
	if groupTree {
		return budget
	}
	limit := budget.Sub(budget.QuoRaw(GroupTreeFeeReserveDivisor))
	if !p.MaxFeePerExec.IsNil() && limit.LT(p.MaxFeePerExec) {
		limit = math.MinInt(p.MaxFeePerExec, budget)
	}
	return limit
}

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
		MaxFeePerExec:               DefaultMaxFeePerExec,
		EnableBatchAfterDkg:         DefaultEnableBatchAfterDKG,
		MaxFeesPerEpoch:             DefaultMaxFeesPerEpoch,
		BeaconEnabled:               DefaultBeaconEnabled,
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
	if p.MaxFeePerExec.IsNil() || p.MaxFeePerExec.IsNegative() {
		return fmt.Errorf("max_fee_per_exec must be non-negative: %s", p.MaxFeePerExec)
	}
	if p.MaxFeesPerEpoch.IsNil() || p.MaxFeesPerEpoch.IsNegative() {
		return fmt.Errorf("max_fees_per_epoch must be set and non-negative")
	}
	if p.MaxFeesPerEpoch.LT(p.MaxFeePerExec) {
		return fmt.Errorf("max_fees_per_epoch (%s) must be at least max_fee_per_exec (%s)", p.MaxFeesPerEpoch, p.MaxFeePerExec)
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
	if p.DkgWindowBlocks == 0 {
		return fmt.Errorf("dkg_window_blocks must be positive")
	}
	if p.TleMissTolerance > p.TleMissWindow {
		return fmt.Errorf("tle_miss_tolerance (%d) must not exceed tle_miss_window (%d)", p.TleMissTolerance, p.TleMissWindow)
	}
	if p.TleJailDuration < 0 {
		return fmt.Errorf("tle_jail_duration must be non-negative: %d", p.TleJailDuration)
	}
	return nil
}
