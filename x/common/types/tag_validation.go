package types

import (
	"regexp"
	"strings"
)

// TagPattern validates tag format: lowercase alphanumeric and hyphens.
// Single-character tags (e.g., "x") are allowed.
var TagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// ValidateTagFormat checks if a tag name matches the required format.
func ValidateTagFormat(name string) bool {
	if !TagPattern.MatchString(name) {
		return false
	}
	// Reject consecutive hyphens (regex permits them in the middle).
	if strings.Contains(name, "--") {
		return false
	}
	// Reject the IDN (internationalized domain name) ACE prefix to keep tags
	// out of the Punycode namespace used by homograph attacks.
	if strings.HasPrefix(name, "xn-") {
		return false
	}
	return true
}

// ValidateTagLength checks if a tag name is within the maximum length.
func ValidateTagLength(name string, maxLen uint64) bool {
	if name == "" {
		return false
	}
	return uint64(len(name)) <= maxLen
}
