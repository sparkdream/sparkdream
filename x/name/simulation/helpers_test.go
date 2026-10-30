package simulation

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/name/types"
)

// The generated names pass MsgRegisterName's length and character rules.
func TestRandomNameValid(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	params := types.DefaultParams()
	for range 2000 {
		n := randomName(r, params)
		require.NotEmpty(t, n)
		require.GreaterOrEqual(t, uint64(len(n)), params.MinNameLength, n)
		require.LessOrEqual(t, uint64(len(n)), params.MaxNameLength, n)
		require.Regexp(t, validNameRegex, n)
	}
	params.MinNameLength, params.MaxNameLength = 5, 4
	require.Empty(t, randomName(r, params))
}

// The generated display names are ASCII, trimmed and within 32 runes.
func TestRandomDisplayNameValid(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for range 500 {
		d := randomDisplayName(r)
		require.NotEmpty(t, d)
		require.LessOrEqual(t, len(d), 32, d)
		require.Equal(t, normalize(d), d, "no edge whitespace or capitals")
	}
}

func TestBlockedIsCaseInsensitive(t *testing.T) {
	require.True(t, blocked("zenith", []string{" Zenith "}))
	require.False(t, blocked("aurora", []string{"zenith"}))
}
