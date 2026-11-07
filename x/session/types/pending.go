package types

// Grantee consent for access-class grants (SES-102).
//
// A grant is created by its granter, so without consent anyone with a funded
// address could push grants onto any grantee and fill the per-class grantee
// cap (MaxGrantsPerGrantee) that bounds every walk of a grantee's grants: a
// few dozen throwaway granters could block a public daemon hot key's
// renewal. The access class (SESSION_KEY, SIGN_IN) is the one a grantee's
// owner must be able to renew, so an access-class grant from a granter the
// grantee has not allowed waits as PENDING: it is indexed under the granter
// only, never counts toward the grantee cap, and becomes ACTIVE only when
// the grantee accepts it (MsgAcceptGrant, or the first MsgExecSession under
// a session key). Acceptance also allows the granter, so its later grants to
// that grantee are active at creation.
//
// Payout-class grants (RECURRING_PULL, SPENDING_ALLOWANCE, SCHEDULED_ONESHOT)
// are not gated: they push value toward the grantee, have their own class
// cap, and a module grantee (x/artifact's Harberger tax pulls) could never
// sign an acceptance. Self-grants are refused outright (ErrSelfDelegation)
// and module-bypass grants are created by a module for granters it controls,
// so both are active immediately.

// MaxPendingGrantsPerGranter caps the PENDING grants one granter may hold
// across all grantees. Compiled: a pending grant costs the chain an index
// entry and an escrow row until the grantee answers or the TTL runs out.
const MaxPendingGrantsPerGranter = 4

// MaxMigratedPendingGrants caps the PENDING grants naming a migrating member
// as grantee that the migration hook carries over. Pending grants are capped
// per granter, not per grantee, so anyone can point any number of them at an
// address; without the cap a flood of them would push the member's migration
// tx out of gas. Grants past the cap stay under the old address, where they
// are inert (a tombstone can neither accept nor exec) and expire with the
// TTL; their granters can still revoke them for the deposit.
const MaxMigratedPendingGrants = 32

// IsAccessGrantType reports whether grants of type t need the grantee's
// consent (session keys and sign-ins).
func IsAccessGrantType(t GrantType) bool {
	return t == GrantType_GRANT_TYPE_SESSION_KEY || t == GrantType_GRANT_TYPE_SIGN_IN
}
