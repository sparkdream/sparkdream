package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/name/types"
)

func TestDisputeKey(t *testing.T) {
	require.NotEmpty(t, []byte(types.DisputeKey))
	require.NotEqual(t, []byte(types.KeyDisputes), []byte(types.DisputeKey),
		"legacy DisputeKey prefix should not collide with the collections KeyDisputes prefix")
}
