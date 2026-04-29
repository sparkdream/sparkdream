package types

import (
	"cosmossdk.io/errors"
)

// x/federation module sentinel errors
var (
	ErrInvalidSigner        = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrInvalidPacketTimeout = errors.Register(ModuleName, 1500, "invalid packet timeout")
	ErrInvalidVersion       = errors.Register(ModuleName, 1501, "invalid version")
	ErrInvalidRequest       = errors.Register(ModuleName, 1502, "invalid request")

	// Peer errors
	ErrPeerNotFound           = errors.Register(ModuleName, 2300, "peer ID does not exist")
	ErrPeerAlreadyExists      = errors.Register(ModuleName, 2301, "peer ID already registered")
	ErrPeerNotActive          = errors.Register(ModuleName, 2302, "peer is not in ACTIVE status")
	ErrPeerTypeMismatch       = errors.Register(ModuleName, 2303, "operation not valid for this peer type")
	ErrInvalidPeerID          = errors.Register(ModuleName, 2304, "peer ID format validation failed")
	ErrPeerCleanupInProgress  = errors.Register(ModuleName, 2332, "peer removal cleanup still in progress, cannot re-register")

	// Bridge errors
	ErrBridgeNotFound        = errors.Register(ModuleName, 2305, "bridge operator not registered for this peer")
	ErrBridgeAlreadyExists   = errors.Register(ModuleName, 2306, "bridge operator already registered for this peer")
	ErrBridgeNotActive       = errors.Register(ModuleName, 2307, "bridge is suspended or revoked")
	ErrInsufficientStake     = errors.Register(ModuleName, 2308, "below minimum bridge stake requirement")
	ErrMaxBridgesExceeded    = errors.Register(ModuleName, 2309, "peer has reached max bridge operators")
	ErrCooldownNotElapsed    = errors.Register(ModuleName, 2324, "bridge revocation cooldown has not elapsed")
	ErrBridgeNotOwnedBySigner = errors.Register(ModuleName, 2333, "signer is not the bridge operator")
	ErrInvalidStakeDenom     = errors.Register(ModuleName, 2334, "stake denomination must be uspark")
	ErrSlashExceedsStake     = errors.Register(ModuleName, 2320, "slash amount exceeds operator's remaining stake")

	// Content errors
	ErrContentTypeNotAllowed = errors.Register(ModuleName, 2310, "content type not in peer policy")
	ErrRateLimitExceeded     = errors.Register(ModuleName, 2311, "rate limit exceeded")
	ErrContentNotFound       = errors.Register(ModuleName, 2315, "federated content ID not found")
	ErrContentTooLarge       = errors.Register(ModuleName, 2321, "content body exceeds max_content_body_size")
	ErrContentUriTooLarge    = errors.Register(ModuleName, 2326, "content URI exceeds max_content_uri_size")
	ErrMetadataTooLarge      = errors.Register(ModuleName, 2327, "protocol metadata exceeds max_protocol_metadata_size")
	ErrDuplicateContent      = errors.Register(ModuleName, 2328, "content with same hash already exists for this peer")
	ErrContentHashRequired   = errors.Register(ModuleName, 2351, "content hash is required (must be SHA-256 of full source content)")
	ErrUnknownContentType    = errors.Register(ModuleName, 2331, "content type not in known_content_types registry")

	// Identity errors
	ErrIdentityBlocked            = errors.Register(ModuleName, 2312, "remote identity is blocked")
	ErrIdentityLinkExists         = errors.Register(ModuleName, 2313, "identity link already exists for this peer")
	ErrIdentityLinkNotFound       = errors.Register(ModuleName, 2314, "no identity link for this peer")
	ErrRemoteIdentityAlreadyClaimed = errors.Register(ModuleName, 2322, "another local address already claims this remote identity")
	ErrMaxIdentityLinksExceeded   = errors.Register(ModuleName, 2323, "local address has reached max_identity_links_per_user")
	ErrNoPendingChallenge         = errors.Register(ModuleName, 2329, "no pending identity challenge for this address/peer")
	ErrChallengeExpired           = errors.Register(ModuleName, 2330, "identity challenge has expired")

	// Reputation errors
	ErrAttestationNotFound    = errors.Register(ModuleName, 2316, "reputation attestation not found")
	ErrReputationNotSupported = errors.Register(ModuleName, 2317, "reputation queries not supported for this peer type")

	// IBC errors
	ErrIBCNotAvailable = errors.Register(ModuleName, 2360, "IBC channel keeper not available")

	// Authorization errors
	ErrNotAuthorized = errors.Register(ModuleName, 2318, "sender not authorized for this action")

	// Param errors
	ErrInvalidParamValue = errors.Register(ModuleName, 2325, "operational or governance param outside valid range")

	// Verifier errors
	ErrVerifierNotFound           = errors.Register(ModuleName, 2335, "address is not a registered verifier")
	ErrVerifierNotActive          = errors.Register(ModuleName, 2336, "verifier bond status is not NORMAL or RECOVERY")
	ErrInsufficientVerifierBond   = errors.Register(ModuleName, 2337, "verifier bond too low for this operation")
	ErrBondCommitted              = errors.Register(ModuleName, 2338, "cannot unbond — bond committed against pending challenges")
	ErrDemotionCooldown           = errors.Register(ModuleName, 2339, "demotion cooldown has not elapsed")
	ErrVerifierOverturnCooldown   = errors.Register(ModuleName, 2340, "verifier in overturn cooldown, cannot verify")
	ErrContentNotPendingVerification = errors.Register(ModuleName, 2341, "content is not in PENDING_VERIFICATION status")
	ErrVerificationWindowExpired  = errors.Register(ModuleName, 2342, "verification window has expired")
	ErrContentNotVerified         = errors.Register(ModuleName, 2343, "content is not in VERIFIED status (cannot challenge)")
	ErrChallengeWindowExpired     = errors.Register(ModuleName, 2344, "challenge window has expired")
	ErrSelfChallenge              = errors.Register(ModuleName, 2345, "challenger cannot be the verifier or submitting operator")
	ErrTrustLevelInsufficient     = errors.Register(ModuleName, 2346, "sender does not meet minimum trust level")
	ErrSelfArbiter                = errors.Register(ModuleName, 2347, "submitting operator cannot arbitrate their own content")
	ErrSelfVerification           = errors.Register(ModuleName, 2352, "verifier cannot verify content submitted by their own bridge operator address")
	ErrChallengeCooldownActive    = errors.Register(ModuleName, 2353, "challenge cooldown has not elapsed since last rejected challenge on this content")

	// Escalation errors
	ErrNotChallengeParty          = errors.Register(ModuleName, 2348, "escalation signer is not the challenger or verifier")
	ErrNoAutoResolutionToEscalate = errors.Register(ModuleName, 2349, "content has no pending auto-resolution to escalate")
	ErrEscalationWindowExpired    = errors.Register(ModuleName, 2350, "escalation window has passed")
)
