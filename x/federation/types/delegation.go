package types

import sdk "github.com/cosmos/cosmos-sdk/types"

// NonDelegableMsgTypeURLs lists the x/federation messages a session key may
// never sign (x/session ForbiddenAllowlistMsgs is built from it).
//
// MsgAttestIdentityLink stakes the operator's SPARK bond on a claim that a
// person controls a remote identity, and a verified link is what
// ResolveRemoteIdentity answers with. Unlike MsgSubmitFederatedContent,
// which a verifier re-checks before anyone relies on it, nothing re-checks
// an attestation before it takes effect, so a leaked daemon key could hand
// out remote identities at will. Attestations are signed by the operator
// key itself.
func NonDelegableMsgTypeURLs() []string {
	return []string{
		sdk.MsgTypeURL(&MsgAttestIdentityLink{}),
	}
}
