package types

// Federation module events
const (
	// Peer lifecycle
	EventTypePeerRegistered        = "peer_registered"
	EventTypePeerActivated         = "peer_activated"
	EventTypePeerSuspended         = "peer_suspended"
	EventTypePeerResumed           = "peer_resumed"
	EventTypePeerRemoved           = "peer_removed"
	EventTypePeerCleanupComplete   = "peer_cleanup_complete"
	EventTypePeerPolicyUpdated     = "peer_policy_updated"
	EventTypePeerInfoUpdated       = "peer_info_updated"
	EventTypePeerPolicyAutoUpdated = "peer_policy_auto_updated"

	// Bridge operators. The bond-lifecycle events this block used to carry
	// (revoked, auto_revoked, slashed, self_unbonded, stake_topped_up,
	// unbonding_complete, stake_insufficient) went away with the Phase 4
	// federation→service migration: x/service owns the bond and emits those.
	// Federation emits only what it still owns — registration, endpoint
	// updates, and the binding-lifecycle reactions below.
	EventTypeBridgeRegistered      = "bridge_registered"
	EventTypeBridgeUpdated         = "bridge_updated"
	EventTypeBridgeInactiveWarning = "bridge_inactive_warning"

	// Bridge operator compensation (see keeper/operator_reward.go).
	EventTypeOperatorRewardPoolFunded         = "operator_reward_pool_funded"
	EventTypeOperatorRewardPaid               = "operator_reward_paid"
	EventTypeOperatorRewardEpochSkipped       = "operator_reward_epoch_skipped"
	EventTypeOperatorRewardPoolOverflowBurned = "operator_reward_pool_overflow_burned"

	// Federation→service migration events (Phase 5).
	EventTypeBridgeUnbound         = "bridge_unbound"
	EventTypeBridgeSuspended       = "bridge_suspended"
	EventTypeBridgeResumed         = "bridge_resumed"
	EventTypeFederationHookFailure = "federation_hook_failure"

	// Content federation
	EventTypeFederatedContentReceived  = "federated_content_received"
	EventTypeFederatedContentModerated = "federated_content_moderated"
	EventTypeContentFederated          = "content_federated"
	EventTypeOutboundAttested          = "outbound_attested"

	// IBC packet send failures (best-effort path: tx still succeeds, but the
	// failure is surfaced as an event + log entry so that silent packet drops
	// are observable instead of being swallowed by `_, _ =`).
	EventTypeFederationPacketSendFailed = "federation_packet_send_failed"

	// Identity
	EventTypeIdentityLinked              = "identity_linked"
	EventTypeIdentityVerified            = "identity_verified"
	EventTypeIdentityUnlinked            = "identity_unlinked"
	EventTypeIdentityLinkRevoked         = "identity_link_revoked"
	EventTypeIdentityLinkExpired         = "identity_link_expired"
	EventTypeIdentityVerificationTimeout = "identity_verification_timeout"
	EventTypeIdentityVerificationFailed  = "identity_verification_failed"
	EventTypeIdentityChallengeReceived   = "identity_challenge_received"
	EventTypeIdentityChallengeConfirmed  = "identity_challenge_confirmed"
	EventTypeIdentityChallengeExpired    = "identity_challenge_expired"
	EventTypeIdentityConfirmationTimeout = "identity_confirmation_timeout"
	// Bridge identity attestation: an operator verified a link to a bridge
	// peer, or a slashed operator's attestation was withdrawn.
	EventTypeIdentityLinkAttested       = "identity_link_attested"
	EventTypeIdentityAttestationRevoked = "identity_attestation_revoked"

	// Reputation
	EventTypeReputationAttested     = "reputation_attested"
	EventTypeReputationExpired      = "reputation_expired"
	EventTypeReputationQueryTimeout = "reputation_query_timeout"
	EventTypeContentSendTimeout     = "content_send_timeout"

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
	EventTypeVerifierCommitmentReleased = "verifier_commitment_released"
	// EventTypeVerificationRejected: a PENDING verification settled as
	// OVERTURNED without accuracy credit because its content was REJECTED
	// by the committee (FED-202).
	EventTypeVerificationRejected = "verification_rejected"
	EventTypeContentUnresolved    = "content_unresolved"
	EventTypeContentSuperseded    = "content_superseded"
	// The verifier reward events (verifier_spark_reward_paid,
	// verifier_dream_reward_paid, verifier_dream_reward_auto_bonded,
	// verifier_bond_restored) are emitted by x/rep, which owns the
	// distribution -- see x/rep/keeper/verifier_reward_distribution.go.

	// Arbiter resolution
	EventTypeArbiterHashSubmitted          = "arbiter_hash_submitted"
	EventTypeArbiterQuorumReached          = "arbiter_quorum_reached"
	EventTypeChallengeAutoResolved         = "challenge_auto_resolved"
	EventTypeChallengeEscalated            = "challenge_escalated"
	EventTypeArbiterResolutionExpired      = "arbiter_resolution_expired"
	EventTypeChallengeCancelledPeerRemoved = "challenge_cancelled_peer_removed"
	// settlement_failure records an EndBlocker settlement whose side effect
	// (bank send, burn, x/rep slash / mint / outcome / release) failed. The
	// settlement ran in a cache context, so none of it committed (the escrow
	// stays on the record), and it is retried. Attributes: content_id,
	// step, attempt, retry_at, error.
	EventTypeSettlementFailure = "settlement_failure"
	// settlement_abandoned: the last retry of an EndBlocker settlement
	// failed. The record keeps its escrow (content still in dispute) until
	// peer removal or a committee verdict settles it. Attributes:
	// content_id, step, attempt, error.
	EventTypeSettlementAbandoned = "settlement_abandoned"
	// challenge_report_filed / challenge_report_deferred track the x/service
	// system report federation owes against a submitting bridge operator.
	// deferred means x/service refused it for now (rate limit); the
	// EndBlocker retries with the same dedupe key.
	EventTypeChallengeReportFiled    = "challenge_report_filed"
	EventTypeChallengeReportDeferred = "challenge_report_deferred"
	// content_awaiting_review: content landed HIDDEN because its peer's
	// policy has require_review set; an Operations Committee restore
	// (MsgModerateContent to ACTIVE) publishes it.
	EventTypeContentAwaitingReview = "content_awaiting_review"

	// Phase 2 (jury) resolution
	EventTypeJuryVerdictApplied    = "jury_verdict_applied"
	EventTypeEscalationFeeRefunded = "escalation_fee_refunded"
	EventTypeEscalationFeeBurned   = "escalation_fee_burned"

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
	AttributeKeySupersededBy    = "superseded_by"
	AttributeKeyContentType     = "content_type"
	AttributeKeyCreatorIdentity = "creator_identity"
	AttributeKeyLocalAddress    = "local_address"
	AttributeKeyRemoteIdentity  = "remote_identity"
	AttributeKeyEvidenceURI     = "evidence_uri"
	AttributeKeyLinkProof       = "link_proof"
	AttributeKeyVerifier        = "verifier"
	AttributeKeyChallenger      = "challenger"
	AttributeKeyNewStatus       = "new_status"
	AttributeKeyUpdatedBy       = "updated_by"
	AttributeKeyBondStatus      = "bond_status"
	AttributeKeySlashAmount     = "slash_amount"
	AttributeKeyRemainingBond   = "remaining_bond"
	AttributeKeyLocalContentID  = "local_content_id"
	AttributeKeyCreator         = "creator"
	AttributeKeyLicense         = "license"
	AttributeKeyError           = "error"
	AttributeKeyPacketKind      = "packet_kind"
	AttributeKeyServiceType     = "service_type"

	// AttributeKeyChallengerRefunded is the challenge fee returned to the
	// challenger when peer removal cancels an active dispute ("0" if none).
	AttributeKeyChallengerRefunded = "challenger_refunded"
)

// AttributeKeyCounterpartyChainID names the chain behind a peer's
// federation channel on peer_resumed (FED-106).
const AttributeKeyCounterpartyChainID = "counterparty_chain_id"
