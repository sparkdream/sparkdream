package types

import "strings"

// MaxSignInGrantsPerGranter caps a granter's active SIGN_IN grants. A
// constant rather than a param: a param added to a running chain decodes as
// zero until a migration sets it, which would refuse every SIGN_IN grant on a
// chain upgraded without one.
const MaxSignInGrantsPerGranter = 20

// maxSignInServiceLen is the DNS limit on a full hostname.
const maxSignInServiceLen = 253

// ValidSignInService reports whether s is a lowercase DNS hostname with at
// least two labels: letters, digits and inner hyphens, 1-63 characters per
// label. It is what an off-chain service compares against its own hostname,
// so uppercase, a trailing dot, a port or a scheme is refused rather than
// normalized, and no grant can name a service in two spellings.
func ValidSignInService(s string) bool {
	if s == "" || len(s) > maxSignInServiceLen {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
