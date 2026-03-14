package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "shield"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	GovModuleName = "gov"

	// ContextKeyFeePaid is the context key set by ShieldGasDecorator
	// to signal that fees have already been paid by the shield module.
	ContextKeyFeePaid = "shield_fee_paid"
)

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

	// Pending shielded operations (key: op_id)
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
)
