package types

import "fmt"

// ShieldCircuitID is the circuit id of the verification key immediate execs
// are verified against (shield.verification_keys).
const ShieldCircuitID = "shield_v1"

// ScopeFieldSeparator joins two field names in a MESSAGE_FIELD scope path,
// e.g. "target_type,target_id". The first field qualifies the second, so ids
// drawn from different sequences (a collection 5 and an item 5) get different
// nullifier scopes.
const ScopeFieldSeparator = ","

// ScopeFieldFallback joins two field names in a MESSAGE_FIELD scope path,
// e.g. "reply_id|post_id": the first field when it is non-zero, else the
// second. The scope is PackNullifierScope(1, first) or PackNullifierScope(0,
// second) = second, so a target named by either field gets its own scope
// (a reaction on a reply doesn't spend the post's).
const ScopeFieldFallback = "|"

// PackNullifierScope combines a two-field scope into one uint64: the qualifier
// in the top byte, the id in the low 56 bits. Clients compute the same value
// for the proof's Scope input: (qualifier << 56) | id.
func PackNullifierScope(qualifier, id uint64) (uint64, error) {
	if qualifier >= 1<<8 {
		return 0, fmt.Errorf("scope qualifier %d does not fit in 8 bits", qualifier)
	}
	if id >= 1<<56 {
		return 0, fmt.Errorf("scope id %d does not fit in 56 bits", id)
	}
	return qualifier<<56 | id, nil
}
