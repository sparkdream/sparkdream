package types

import (
	"regexp"
)

// ValidatePeerID checks that a peer ID is lowercase alphanumeric + hyphens + dots, 3-64 chars.
var peerIDRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{1,62}[a-z0-9]$`)

func ValidatePeerID(id string) bool {
	return peerIDRegex.MatchString(id)
}
