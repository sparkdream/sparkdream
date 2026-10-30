package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/service/types"
)

// SVC-16: min_reporter_trust_level is empty (no gate) or a defined x/rep
// trust level; a typo must not pass validation and switch the gate off.
func TestParamsValidate_MinReporterTrustLevel(t *testing.T) {
	p := types.DefaultParams()
	require.NoError(t, p.Validate())

	p.MinReporterTrustLevel = ""
	require.NoError(t, p.Validate(), "empty means no gate")

	for name := range reptypes.TrustLevel_value {
		p.MinReporterTrustLevel = name
		require.NoError(t, p.Validate(), name)
	}

	p.MinReporterTrustLevel = "TRUST_LEVEL_ESTABLISED"
	require.ErrorIs(t, p.Validate(), types.ErrInvalidParams)
}
