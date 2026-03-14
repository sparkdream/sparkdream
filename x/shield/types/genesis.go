package types

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:        DefaultParams(),
		RegisteredOps: defaultShieldedOps(),
	}
}

// defaultShieldedOps returns the default set of shielded operations registered
// at genesis. These cover all existing anonymous functionality across modules.
// See docs/x-shield-spec.md "Default Operations (Genesis)" for rationale.
func defaultShieldedOps() []ShieldedOpRegistration {
	return []ShieldedOpRegistration{
		// --- x/blog ---
		{
			MessageTypeUrl:     "/sparkdream.blog.v1.MsgCreatePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1, // anon_min_trust
			NullifierDomain:    1,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.blog.v1.MsgCreateReply",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    2,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.blog.v1.MsgReact",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    8,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		// --- x/forum ---
		{
			MessageTypeUrl:     "/sparkdream.forum.v1.MsgCreatePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    11,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.forum.v1.MsgUpvotePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    12,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.forum.v1.MsgDownvotePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    13,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		// --- x/collect ---
		{
			MessageTypeUrl:     "/sparkdream.collect.v1.MsgCreateCollection",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    21,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.collect.v1.MsgUpvoteContent",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    22,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "target_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.collect.v1.MsgDownvoteContent",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      1,
			NullifierDomain:    23,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "target_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		// --- x/rep ---
		{
			MessageTypeUrl:     "/sparkdream.rep.v1.MsgCreateChallenge",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    41,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY,
		},
		// --- x/commons (anonymous governance) ---
		// BatchMode is EITHER so these work in both immediate and encrypted batch modes.
		// Immediate mode is needed while TLE/DKG is not yet active (encrypted_batch_enabled=false).
		// Once TLE is production-ready, governance can change these to ENCRYPTED_ONLY
		// for maximum sender unlinkability.
		{
			MessageTypeUrl:     "/sparkdream.commons.v1.MsgSubmitAnonymousProposal",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    31,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.commons.v1.MsgAnonymousVoteProposal",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    32,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "proposal_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	return gs.Params.Validate()
}
