package types

import (
	"fmt"

	"sparkdream/tools/tle"
)

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
//
// Content ops take MinTrustLevel 0: the proof only shows
// membership, so the anonymity set is every member and a rarer trust level
// never narrows it. A target that asks for more (a post's
// min_reply_trust_level, a curator or sponsor gate) makes the client prove
// that level for that action alone.
func defaultShieldedOps() []ShieldedOpRegistration {
	ops := []ShieldedOpRegistration{
		// --- x/blog ---
		{
			MessageTypeUrl:     "/sparkdream.blog.v1.MsgCreatePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    1,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			// One anonymous post per member per 12 epochs (~1 hour).
			EpochWindow: 12,
			Active:      true,
			BatchMode:   ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.blog.v1.MsgCreateReply",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    2,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl: "/sparkdream.blog.v1.MsgReact",
			ProofDomain:    ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:  0,
			// One anonymous reaction per member per post and per reply:
			// reply ids come from their own sequence, so a reply reaction is
			// scoped to the reply and doesn't spend the post's.
			NullifierDomain:    8,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "reply_id" + ScopeFieldFallback + "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		// --- x/forum ---
		{
			MessageTypeUrl:     "/sparkdream.forum.v1.MsgCreatePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    11,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			// One anonymous post or reply per member per 3 epochs (~15 min).
			EpochWindow: 3,
			Active:      true,
			BatchMode:   ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.forum.v1.MsgUpvotePost",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    12,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl: "/sparkdream.forum.v1.MsgDownvotePost",
			ProofDomain:    ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:  0,
			// Shares the upvote domain: one anonymous vote per member per
			// post, either way, like the module's own per-voter record.
			NullifierDomain:    12,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "post_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		// --- x/collect ---
		{
			MessageTypeUrl:     "/sparkdream.collect.v1.MsgCreateCollection",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    21,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_EPOCH,
			// One anonymous collection per member per 288 epochs (~1 day).
			EpochWindow: 288,
			Active:      true,
			BatchMode:   ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl:     "/sparkdream.collect.v1.MsgUpvoteContent",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      0,
			NullifierDomain:    22,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "target_type,target_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
		{
			MessageTypeUrl: "/sparkdream.collect.v1.MsgDownvoteContent",
			ProofDomain:    ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:  0,
			// Shares the upvote domain: one anonymous vote per member per
			// target, either way, like the module's own per-voter record.
			NullifierDomain:    22,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "target_type,target_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
	}
	// Managing an anonymous collection: ownership mode, so the proof must
	// reproduce the collection's owner tag (its creation nullifier) and binds
	// the collection's owner sequence. Nothing is recorded under domain 23; it
	// only labels these ops.
	for _, typeURL := range []string{
		"/sparkdream.collect.v1.MsgUpdateCollection",
		"/sparkdream.collect.v1.MsgDeleteCollection",
		"/sparkdream.collect.v1.MsgAddItem",
		"/sparkdream.collect.v1.MsgAddItems",
		"/sparkdream.collect.v1.MsgUpdateItem",
		"/sparkdream.collect.v1.MsgRemoveItem",
		"/sparkdream.collect.v1.MsgRemoveItems",
		"/sparkdream.collect.v1.MsgReorderItem",
	} {
		ops = append(ops, ShieldedOpRegistration{
			MessageTypeUrl:  typeURL,
			ProofDomain:     ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:   0,
			NullifierDomain: 23,
			NullifierMode:   NullifierMode_NULLIFIER_MODE_OWNERSHIP,
			Active:          true,
			BatchMode:       ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
		})
	}
	ops = append(ops, []ShieldedOpRegistration{
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
		// --- x/federation (anonymous arbiter quorum) ---
		// FEDERATION-S2-5: scope nullifier per content_id so a single identity
		// cannot cast multiple votes for the same federated content. Without
		// this scope, a single identity could drive ArbiterHashCounts to
		// quorum alone via cross-epoch nullifier rotation.
		{
			MessageTypeUrl:     "/sparkdream.federation.v1.MsgSubmitArbiterHash",
			ProofDomain:        ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      2, // ESTABLISHED+: same gate as identified bridge operators
			NullifierDomain:    51,
			NullifierScopeType: NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD,
			ScopeFieldPath:     "content_id",
			Active:             true,
			BatchMode:          ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		},
	}...)
	return ops
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	seenOps := make(map[string]bool, len(gs.RegisteredOps))
	for _, reg := range gs.RegisteredOps {
		if err := reg.Validate(); err != nil {
			return err
		}
		if seenOps[reg.MessageTypeUrl] {
			return fmt.Errorf("duplicate registered op %s", reg.MessageTypeUrl)
		}
		seenOps[reg.MessageTypeUrl] = true
	}
	if err := gs.TleKeySet.Validate(); err != nil {
		return err
	}
	return gs.Params.Validate()
}

// BN256 marshaled point sizes (kyber).
const (
	G1PointLen = 64
	G2PointLen = 128
)

// Validate checks a seeded key set is complete and self-consistent: a G1 and
// a G2 master key encoding the same scalar (clients encrypt to the G2 one)
// and, per validator, a unique 1-based share index and G1/G2 public shares
// encoding the same scalar (decryption shares are verified against the G2
// one). A G2 key that does not match its G1 twin would have clients encrypt
// to a key no reconstruction can open. An empty key set is valid (no DKG yet).
func (ks TLEKeySet) Validate() error {
	if len(ks.MasterPublicKey) == 0 && len(ks.ValidatorShares) == 0 && len(ks.MasterPublicKeyG2) == 0 {
		return nil
	}
	if len(ks.MasterPublicKey) != G1PointLen {
		return fmt.Errorf("tle_key_set.master_public_key must be a %d-byte G1 point", G1PointLen)
	}
	if len(ks.MasterPublicKeyG2) != G2PointLen {
		return fmt.Errorf("tle_key_set.master_public_key_g2 must be a %d-byte G2 point", G2PointLen)
	}
	if ks.ThresholdDenominator == 0 || ks.ThresholdNumerator == 0 || ks.ThresholdNumerator > ks.ThresholdDenominator {
		return fmt.Errorf("tle_key_set threshold %d/%d is invalid", ks.ThresholdNumerator, ks.ThresholdDenominator)
	}
	if err := tle.VerifyG1G2Pair(ks.MasterPublicKey, ks.MasterPublicKeyG2); err != nil {
		return fmt.Errorf("tle_key_set master keys: %w", err)
	}
	seen := map[uint32]bool{}
	for _, vs := range ks.ValidatorShares {
		if vs == nil {
			return fmt.Errorf("tle_key_set has a nil validator share")
		}
		if vs.ValidatorAddress == "" {
			return fmt.Errorf("tle_key_set validator share without address")
		}
		if vs.ShareIndex == 0 || seen[vs.ShareIndex] {
			return fmt.Errorf("tle_key_set share index %d for %s must be unique and 1-based", vs.ShareIndex, vs.ValidatorAddress)
		}
		seen[vs.ShareIndex] = true
		if len(vs.PublicShare) != G1PointLen {
			return fmt.Errorf("tle_key_set public_share for %s must be a %d-byte G1 point", vs.ValidatorAddress, G1PointLen)
		}
		if len(vs.PublicShareG2) != G2PointLen {
			return fmt.Errorf("tle_key_set public_share_g2 for %s must be a %d-byte G2 point", vs.ValidatorAddress, G2PointLen)
		}
		if err := tle.VerifyG1G2Pair(vs.PublicShare, vs.PublicShareG2); err != nil {
			return fmt.Errorf("tle_key_set public shares for %s: %w", vs.ValidatorAddress, err)
		}
	}
	return nil
}
