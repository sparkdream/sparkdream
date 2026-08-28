package types

import "cosmossdk.io/collections"

const (
	// Service-type strings seeded at x/service genesis. Federation
	// derives the service_type for each bridge from the peer's
	// PeerType. Sync with the genesis seed in x/service genesis_vals
	// (or wherever the ServiceTypeConfig entries are bootstrapped).
	ServiceTypeFederationBridgeActivityPub = "federation-bridge-activitypub"
	ServiceTypeFederationBridgeATProto     = "federation-bridge-atproto"
	ServiceTypeFederationBridgeNOSTR       = "federation-bridge-nostr"
	ServiceTypeFederationBridgeLENS        = "federation-bridge-lens"

	// ModuleName defines the module name
	ModuleName = "federation"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	GovModuleName = "gov"

	// Version defines the current version the IBC module supports
	Version = "federation-1"

	// PortID is the default port id that module binds to
	PortID = "federation"
)

var (
	// PortKey defines the key to store the port ID in store
	PortKey = collections.NewPrefix("federation-port-")

	// ParamsKey is the prefix for module parameters
	ParamsKey = collections.NewPrefix("p_federation")

	// --- Primary Collections ---

	PeersKey        = collections.NewPrefix("fed/peers/")
	PeerPoliciesKey = collections.NewPrefix("fed/policies/")
	// BridgeBindingsKey: (operator_address, peer_id) → BridgeBinding.
	// Renamed from BridgeOperatorsKey in the federation→service
	// migration; storage prefix string kept stable.
	BridgeBindingsKey = collections.NewPrefix("fed/bridges/")
	// VerifierActivityKey: address -> VerifierActivity (federation-specific
	// per-verifier counters). Generic bond state lives in x/rep as
	// BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr).
	VerifierActivityKey = collections.NewPrefix("fed/verifier_activity/")
	// OperatorRewardDayFundingKey: utc_day -> math.Int string. Ledgers the
	// community-pool draw for the bridge-operator reward pool so the daily
	// allowance survives restarts and cannot be re-drawn within the same day.
	OperatorRewardDayFundingKey = collections.NewPrefix("fed/operator_reward_day/")
	VerificationRecsKey         = collections.NewPrefix("fed/verifyrecs/")
	ArbiterSubmissionsKey       = collections.NewPrefix("fed/arbiters/")
	ContentKey                  = collections.NewPrefix("fed/content/")
	IdentityLinksKey            = collections.NewPrefix("fed/idlinks/")
	PendingIdChallengesKey      = collections.NewPrefix("fed/idchallenges/")
	RepAttestationsKey          = collections.NewPrefix("fed/repattest/")
	OutboundAttestationsKey     = collections.NewPrefix("fed/outbound/")
	PeerRemovalQueueKey         = collections.NewPrefix("fed/peerremoval/")

	// --- Sequences ---

	ContentSeqKey             = collections.NewPrefix("fed/seq/content")
	OutboundAttestationSeqKey = collections.NewPrefix("fed/seq/outbound")
	ArbiterAnonSubSeqKey      = collections.NewPrefix("fed/seq/arbiter_anon")

	// --- Secondary Indexes ---

	// Content indexes
	ContentByPeerKey     = collections.NewPrefix("fed/idx/content_peer/")
	ContentByTypeKey     = collections.NewPrefix("fed/idx/content_type/")
	ContentByCreatorKey  = collections.NewPrefix("fed/idx/content_creator/")
	ContentByHashKey     = collections.NewPrefix("fed/idx/content_hash/")
	ContentExpirationKey = collections.NewPrefix("fed/idx/content_exp/")

	// Bridge indexes
	BridgesByPeerKey = collections.NewPrefix("fed/idx/bridges_peer/")

	// BindingsByOperatorKey: (service_type, address, peer_id) reverse
	// index for hook-handler lookups. Multi-valued (Phase 1 of the
	// federation→service migration).
	BindingsByOperatorKey = collections.NewPrefix("fed/idx/bindings_operator/")

	// Identity indexes
	IdentityLinksByRemoteKey = collections.NewPrefix("fed/idx/idlinks_remote/")
	IdentityLinkCountKey     = collections.NewPrefix("fed/idx/idlink_count/")
	UnverifiedLinkExpKey     = collections.NewPrefix("fed/idx/unverified_exp/")

	// Reputation indexes
	AttestationExpKey = collections.NewPrefix("fed/idx/attest_exp/")

	// Verification indexes
	VerificationWindowKey = collections.NewPrefix("fed/idx/verify_window/")
	ChallengeWindowKey    = collections.NewPrefix("fed/idx/challenge_window/")

	// Arbiter resolution indexes
	ArbiterHashCountsKey      = collections.NewPrefix("fed/idx/arbiter_counts/")
	ArbiterResolutionQueueKey = collections.NewPrefix("fed/idx/arbiter_res/")
	ArbiterEscalationQueueKey = collections.NewPrefix("fed/idx/arbiter_esc/")

	// Phase 2 (jury) escalation lifecycle. EscalatedChallengesKey is
	// keyed by content_id; EscalatedChallengeDeadlineKey is a
	// (deadline, content_id) KeySet driven by the EndBlocker timeout
	// sweep.
	EscalatedChallengesKey        = collections.NewPrefix("fed/escalated/")
	EscalatedChallengeDeadlineKey = collections.NewPrefix("fed/escalated_deadline/")

	// Bridge unbonding queue removed in Phase 4 of the federation→
	// service migration. x/service owns operator unbonding now via
	// UnderfundedQueue + per-type unbonding_period_blocks.

	// Rate limiting
	InboundRateLimitKey  = collections.NewPrefix("fed/rate/inbound/")
	OutboundRateLimitKey = collections.NewPrefix("fed/rate/outbound/")
	// Global per-block caps. Keyed by block height; entries are
	// pruned by EndBlocker phase 13.
	InboundPerBlockKey  = collections.NewPrefix("fed/rate/inbound_block/")
	OutboundPerBlockKey = collections.NewPrefix("fed/rate/outbound_block/")
)
