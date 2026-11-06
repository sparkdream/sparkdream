package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/reveal/types"
)

func TestValidateFieldLength(t *testing.T) {
	require.NoError(t, types.ValidateFieldLength("f", "", 8))
	require.NoError(t, types.ValidateFieldLength("f", strings.Repeat("a", 8), 8))
	require.ErrorIs(t, types.ValidateFieldLength("f", strings.Repeat("a", 9), 8), types.ErrFieldTooLong)
	// Runes, not bytes: eight 3-byte runes fit; nine do not.
	require.NoError(t, types.ValidateFieldLength("f", strings.Repeat("日", 8), 8))
	require.ErrorIs(t, types.ValidateFieldLength("f", strings.Repeat("日", 9), 8), types.ErrFieldTooLong)
	// The byte budget still bounds a pathological encoding.
	require.ErrorIs(t, types.ValidateFieldLength("f", strings.Repeat("\xff", 4*8+1), 8), types.ErrFieldTooLong)
}
