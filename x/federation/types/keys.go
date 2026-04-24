package types

import "cosmossdk.io/collections"

const (
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

	PeersKey           = collections.NewPrefix("fed/peers/")
	PeerPoliciesKey    = collections.NewPrefix("fed/policies/")
	BridgeOperatorsKey = collections.NewPrefix("fed/bridges/")
	// VerifierActivityKey: address -> VerifierActivity (federation-specific
	// per-verifier counters). Generic bond state lives in x/rep as
	// BondedRole(ROLE_TYPE_FEDERATION_VERIFIER, addr).
	VerifierActivityKey = collections.NewPrefix("fed/verifier_activity/")
	VerificationRecsKey = collections.NewPrefix("fed/verifyrecs/")
	ArbiterSubmissionsKey = collections.NewPrefix("fed/arbiters/")
	ContentKey         = collections.NewPrefix("fed/content/")
	IdentityLinksKey   = collections.NewPrefix("fed/idlinks/")
	PendingIdChallengesKey = collections.NewPrefix("fed/idchallenges/")
	RepAttestationsKey = collections.NewPrefix("fed/repattest/")
	OutboundAttestationsKey = collections.NewPrefix("fed/outbound/")
	PeerRemovalQueueKey = collections.NewPrefix("fed/peerremoval/")

	// --- Sequences ---

	ContentSeqKey          = collections.NewPrefix("fed/seq/content")
	OutboundAttestationSeqKey = collections.NewPrefix("fed/seq/outbound")

	// --- Secondary Indexes ---

	// Content indexes
	ContentByPeerKey       = collections.NewPrefix("fed/idx/content_peer/")
	ContentByTypeKey       = collections.NewPrefix("fed/idx/content_type/")
	ContentByCreatorKey    = collections.NewPrefix("fed/idx/content_creator/")
	ContentByHashKey       = collections.NewPrefix("fed/idx/content_hash/")
	ContentExpirationKey   = collections.NewPrefix("fed/idx/content_exp/")

	// Bridge indexes
	BridgesByPeerKey = collections.NewPrefix("fed/idx/bridges_peer/")

	// Identity indexes
	IdentityLinksByRemoteKey = collections.NewPrefix("fed/idx/idlinks_remote/")
	IdentityLinkCountKey     = collections.NewPrefix("fed/idx/idlink_count/")
	UnverifiedLinkExpKey     = collections.NewPrefix("fed/idx/unverified_exp/")

	// Reputation indexes
	AttestationExpKey = collections.NewPrefix("fed/idx/attest_exp/")

	// Verification indexes
	VerificationWindowKey  = collections.NewPrefix("fed/idx/verify_window/")
	ChallengeWindowKey     = collections.NewPrefix("fed/idx/challenge_window/")

	// Arbiter resolution indexes
	ArbiterHashCountsKey      = collections.NewPrefix("fed/idx/arbiter_counts/")
	ArbiterResolutionQueueKey = collections.NewPrefix("fed/idx/arbiter_res/")
	ArbiterEscalationQueueKey = collections.NewPrefix("fed/idx/arbiter_esc/")

	// Bridge unbonding
	BridgeUnbondingQueueKey = collections.NewPrefix("fed/idx/bridge_unbond/")

	// Rate limiting
	InboundRateLimitKey  = collections.NewPrefix("fed/rate/inbound/")
	OutboundRateLimitKey = collections.NewPrefix("fed/rate/outbound/")
)
