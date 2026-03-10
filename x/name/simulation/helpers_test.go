package simulation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCouncilNameConstant(t *testing.T) {
	require.Equal(t, "Commons Council", CouncilName)
}
