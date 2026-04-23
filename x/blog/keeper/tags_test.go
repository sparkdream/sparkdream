package keeper

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// tagPostIndexKey layout: "{tag}/" + 8-byte big-endian postID. The '/' separator
// is essential: without it, tags sharing a prefix (e.g. "gov" and "gov2") would
// collide in the secondary index.
func TestTagPostIndexKey(t *testing.T) {
	k1 := tagPostIndexKey("gov", 1)
	k2 := tagPostIndexKey("gov", 2)
	k3 := tagPostIndexKey("gov2", 1)

	require.True(t, bytes.HasPrefix(k1, []byte("gov/")))
	require.True(t, bytes.HasPrefix(k2, []byte("gov/")))
	require.True(t, bytes.HasPrefix(k3, []byte("gov2/")))

	// "gov" keys must not be a prefix of "gov2" keys.
	require.False(t, bytes.HasPrefix(k3, []byte("gov/")))

	// Post IDs encode in 8 bytes big-endian.
	require.Len(t, k1, len("gov/")+8)
	require.Equal(t, GetPostIDBytes(1), k1[len("gov/"):])
	require.Equal(t, GetPostIDBytes(2), k2[len("gov/"):])
}
