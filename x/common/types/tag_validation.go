package types

import (
	"regexp"
)

// TagPattern validates tag format: lowercase alphanumeric and hyphens.
// Single-character tags (e.g., "x") are allowed.
var TagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// ValidateTagFormat checks if a tag name matches the required format.
func ValidateTagFormat(name string) bool {
	return TagPattern.MatchString(name)
}

// ValidateTagLength checks if a tag name is within the maximum length.
func ValidateTagLength(name string, maxLen uint64) bool {
	return uint64(len(name)) <= maxLen
}
