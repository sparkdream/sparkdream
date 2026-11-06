package types

import (
	"unicode/utf8"

	errorsmod "cosmossdk.io/errors"
)

// Compiled length caps on the free-text fields stored by x/reveal. They
// bound state growth per message (a proposal description and every
// verification comment are kept for the life of the record) and are not
// params: no governance use case needs them to move.
const (
	MaxProjectNameLength = 128
	MaxDescriptionLength = 4096
	MaxCommentsLength    = 2048
	MaxReasonLength      = 2048
	MaxCodeURILength     = 512
)

// ValidateFieldLength refuses a free-text field longer than max runes (or
// bytes, whichever is hit first, so a pathological encoding cannot exceed
// the byte budget either).
func ValidateFieldLength(field, value string, max int) error {
	if len(value) > 4*max || utf8.RuneCountInString(value) > max {
		return errorsmod.Wrapf(ErrFieldTooLong, "%s exceeds %d characters", field, max)
	}
	return nil
}
