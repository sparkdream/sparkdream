package types

// Federation module events
const (
	// Peer lifecycle
	EventTypePeerRegistered     = "peer_registered"
	EventTypePeerActivated      = "peer_activated"
	EventTypePeerSuspended      = "peer_suspended"
	EventTypePeerResumed        = "peer_resumed"
	EventTypePeerRemoved        = "peer_removed"
	EventTypePeerCleanupComplete = "peer_cleanup_complete"
	EventTypePeerPolicyUpdated  = "peer_policy_updated"
	EventTypePeerPolicyAutoUpdated = "peer_policy_auto_updated"

	// Bridge operators
	EventTypeBridgeRegistered      = "bridge_registered"
	EventTypeBridgeRevoked         = "bridge_revoked"
	EventTypeBridgeAutoRevoked     = "bridge_auto_revoked"
	EventTypeBridgeSlashed         = "bridge_slashed"
	EventTypeBridgeUpdated         = "bridge_updated"
	EventTypeBridgeSelfUnbonded    = "bridge_self_unbonded"
	EventTypeBridgeStakeToppedUp   = "bridge_stake_topped_up"
	EventTypeBridgeUnbondingComplete = "bridge_unbonding_complete"
	EventTypeBridgeInactiveWarning = "bridge_inactive_warning"
	EventTypeBridgeStakeInsufficient = "bridge_stake_insufficient"

	// Content federation
	EventTypeFederatedContentReceived = "federated_content_received"
	EventTypeFederatedContentModerated = "federated_content_moderated"
	EventTypeContentFederated         = "content_federated"
	EventTypeOutboundAttested         = "outbound_attested"

	// Identity
	EventTypeIdentityLinked            = "identity_linked"
	EventTypeIdentityVerified          = "identity_verified"
	EventTypeIdentityUnlinked          = "identity_unlinked"
	EventTypeIdentityLinkRevoked       = "identity_link_revoked"
	EventTypeIdentityLinkExpired       = "identity_link_expired"
	EventTypeIdentityVerificationTimeout = "identity_verification_timeout"
	EventTypeIdentityVerificationFailed = "identity_verification_failed"
	EventTypeIdentityChallengeReceived = "identity_challenge_received"
	EventTypeIdentityChallengeConfirmed = "identity_challenge_confirmed"
	EventTypeIdentityChallengeExpired  = "identity_challenge_expired"
	EventTypeIdentityConfirmationTimeout = "identity_confirmation_timeout"

	// Reputation
	EventTypeReputationAttested    = "reputation_attested"
	EventTypeReputationExpired     = "reputation_expired"
	EventTypeReputationQueryTimeout = "reputation_query_timeout"
	EventTypeContentSendTimeout    = "content_send_timeout"

	// Params
	EventTypeOperationalParamsUpdated = "operational_params_updated"

	// Verification
	EventTypeVerifierBonded             = "verifier_bonded"
	EventTypeVerifierUnbonded           = "verifier_unbonded"
	EventTypeVerifierDemoted            = "verifier_demoted"
	EventTypeContentVerified            = "content_verified"
	EventTypeContentDisputed            = "content_disputed"
	EventTypeContentVerificationExpired = "content_verification_expired"
	EventTypeVerificationChallenged     = "verification_challenged"
	EventTypeChallengeUpheld            = "challenge_upheld"
	EventTypeChallengeRejected          = "challenge_rejected"
	EventTypeChallengeTimeout           = "challenge_timeout"
	EventTypeVerifierSlashed            = "verifier_slashed"
	EventTypeVerifierCooldownApplied    = "verifier_cooldown_applied"
	EventTypeVerifierDreamRewardPaid    = "verifier_dream_reward_paid"
	EventTypeVerifierDreamRewardAutoBonded = "verifier_dream_reward_auto_bonded"
	EventTypeVerifierBondRestored       = "verifier_bond_restored"

	// Arbiter resolution
	EventTypeArbiterHashSubmitted      = "arbiter_hash_submitted"
	EventTypeArbiterQuorumReached      = "arbiter_quorum_reached"
	EventTypeChallengeAutoResolved     = "challenge_auto_resolved"
	EventTypeChallengeEscalated        = "challenge_escalated"
	EventTypeArbiterResolutionExpired  = "arbiter_resolution_expired"
	EventTypeChallengeCancelledPeerRemoved = "challenge_cancelled_peer_removed"

	// Common attribute keys
	AttributeKeyPeerID          = "peer_id"
	AttributeKeyPeerType        = "type"
	AttributeKeyDisplayName     = "display_name"
	AttributeKeyRegisteredBy    = "registered_by"
	AttributeKeyReason          = "reason"
	AttributeKeyOperator        = "operator"
	AttributeKeyProtocol        = "protocol"
	AttributeKeyAmount          = "amount"
	AttributeKeyContentID       = "content_id"
	AttributeKeyContentType     = "content_type"
	AttributeKeyCreatorIdentity = "creator_identity"
	AttributeKeyLocalAddress    = "local_address"
	AttributeKeyRemoteIdentity  = "remote_identity"
	AttributeKeyVerifier        = "verifier"
	AttributeKeyChallenger      = "challenger"
	AttributeKeyNewStatus       = "new_status"
	AttributeKeyUpdatedBy       = "updated_by"
	AttributeKeyBondStatus      = "bond_status"
	AttributeKeySlashAmount     = "slash_amount"
	AttributeKeyRemainingBond   = "remaining_bond"
	AttributeKeyLocalContentID  = "local_content_id"
	AttributeKeyCreator         = "creator"
)
