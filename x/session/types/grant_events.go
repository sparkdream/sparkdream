package types

import "strings"

// GrantTypeEventValue is the `type` attribute value of every grant event
// (spec §25.1): the lowercase enum suffix, e.g. `session_key`,
// `recurring_pull`, `sign_in`.
func GrantTypeEventValue(t GrantType) string {
	return strings.ToLower(strings.TrimPrefix(t.String(), "GRANT_TYPE_"))
}
