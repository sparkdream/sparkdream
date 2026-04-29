package types_test

import (
	"testing"

	"sparkdream/x/common/types"
)

func TestValidateTagFormat(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Valid tags
		{"simple word", "hello", true},
		{"single char", "a", true},
		{"hyphenated", "my-tag", true},
		{"alphanumeric with hyphen", "tag-123", true},
		{"digits only", "123", true},
		{"single digit", "0", true},
		{"multi-hyphen segments", "a-b-c", true},

		// Invalid tags
		{"empty string", "", false},
		{"uppercase", "UPPER", false},
		{"mixed case", "Hello", false},
		{"contains space", "has space", false},
		{"leading hyphen", "-start-hyphen", false},
		{"trailing hyphen", "end-hyphen-", false},
		{"special char", "special!char", false},
		{"underscore", "under_score", false},
		{"consecutive hyphens", "bad--tag", false},
		{"dot separator", "my.tag", false},
		{"leading space", " tag", false},
		{"trailing space", "tag ", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := types.ValidateTagFormat(tc.input)
			if got != tc.want {
				t.Errorf("ValidateTagFormat(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidateTagLength(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen uint64
		want   bool
	}{
		{"within limit", "hello", 10, true},
		{"at limit", "hello", 5, true},
		{"over limit", "hello", 4, false},
		{"zero length name", "", 10, false},
		{"zero max with empty", "", 0, false},
		{"zero max with content", "a", 0, false},
		{"exact boundary single char", "a", 1, true},
		{"long tag within limit", "this-is-a-long-tag", 100, true},
		{"long tag at limit", "this-is-a-long-tag", 18, true},
		{"long tag over limit", "this-is-a-long-tag", 17, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := types.ValidateTagLength(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("ValidateTagLength(%q, %d) = %v, want %v", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}
