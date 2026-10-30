package types

import (
	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
)

const (
	// ModuleName defines the module name
	ModuleName = "shield"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	GovModuleName = "gov"
)

// ContextKeyFeePaid is the context key set by ShieldGasDecorator to signal
// that fees have already been paid by the shield module. It is the typed key
// shared with x/session's SessionFeeDecorator (x/common/types).
var ContextKeyFeePaid = commontypes.ContextKeyFeePaid

// Collection prefixes for all shield state.
var (
	ParamsKey = collections.NewPrefix("p_shield")

	// Registered shielded operations (key: message_type_url)
	ShieldedOpsKey = collections.NewPrefix("shield/ops/")

	// Used nullifiers (key: domain + scope + nullifier_hex)
	UsedNullifiersKey = collections.NewPrefix("shield/nullifiers/")

	// Pending nullifiers for encrypted batch dedup (key: nullifier_hex)
	PendingNullifiersKey = collections.NewPrefix("shield/pending_nullifiers/")

	// Day funding ledger (key: day number)
	DayFundingsKey = collections.NewPrefix("shield/day_fundings/")

	// Per-identity rate limits (key: epoch + rate_limit_nullifier_hex)
	IdentityRateLimitsKey = collections.NewPrefix("shield/rate_limits/")

	// ZK verification keys (key: circuit_id)
	VerificationKeysKey = collections.NewPrefix("shield/vk/")

	// TLE key set (singleton)
	TLEKeySetKey = collections.NewPrefix("shield/tle_keyset")

	// TLE miss counters (key: validator_address)
	TLEMissCountersKey = collections.NewPrefix("shield/tle_miss/")

	// Pending shielded operations (key: target_epoch + op_id, so one epoch's
	// batch is a prefix range)
	PendingOpsKey = collections.NewPrefix("shield/pending_ops/")

	// Pending op ID sequence
	NextPendingOpIdKey = collections.NewPrefix("shield/pending_ops_seq")

	// Shield epoch state (singleton)
	ShieldEpochStateKey = collections.NewPrefix("shield/epoch_state")

	// Shield epoch decryption keys (key: epoch)
	ShieldDecryptionKeysKey = collections.NewPrefix("shield/dec_keys/")

	// Shield decryption shares (key: epoch + validator)
	ShieldDecryptionSharesKey = collections.NewPrefix("shield/dec_shares/")

	// DKG state (singleton)
	DKGStateKey = collections.NewPrefix("shield/dkg_state")

	// DKG contributions (key: validator_address)
	DKGContributionsKey = collections.NewPrefix("shield/dkg_contrib/")

	// DKG registrations — pub keys registered during REGISTERING phase (key: validator_operator_address)
	DKGRegistrationsKey = collections.NewPrefix("shield/dkg_reg/")

	// Pending op counter (singleton — avoids full iteration to count pending ops)
	PendingOpCountKey = collections.NewPrefix("shield/pending_op_count")

	// TLE epoch participation tracking for sliding window liveness.
	// Key: (validator_address, epoch_mod_window) → bool (participated)
	TLEEpochParticipationKey = collections.NewPrefix("shield/tle_part/")

	// Decryption demands: closed epochs whose shares validators were asked for
	// (key: epoch → height the demand was recorded at)
	DecryptionDemandsKey = collections.NewPrefix("shield/dec_demand/")

	// DKG complaints for the current round (key: dealer + complainer)
	DKGComplaintsKey = collections.NewPrefix("shield/dkg_complaint/")

	// Number of demanded epochs TLE liveness has judged under the current
	// key set; its value mod tle_miss_window is the participation ring slot.
	TLEJudgedEpochsKey = collections.NewPrefix("shield/tle_judged")

	// EpochFeeLedgerKey is the fees the module paid in the current shield
	// epoch (params.max_fees_per_epoch).
	EpochFeeLedgerKey = collections.NewPrefix("shield/fee_ledger")

	// RetiredDomainsKey maps a nullifier domain no live op uses to the
	// registration of the last op that used it.
	RetiredDomainsKey = collections.NewPrefix("shield/retired_domain/")
)
