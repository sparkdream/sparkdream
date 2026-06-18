package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestSentinelAccuracyWindow_Default(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, types.DefaultSentinelAccuracyWindowEpochs, p.SentinelAccuracyWindowEpochs)
	require.NoError(t, p.Validate())

	op := types.DefaultRepOperationalParams()
	require.Equal(t, types.DefaultSentinelAccuracyWindowEpochs, op.SentinelAccuracyWindowEpochs)
	require.NoError(t, op.Validate())
}

func TestSentinelAccuracyWindow_ValidateBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		window  uint64
		wantErr bool
	}{
		{"zero is rejected", 0, true},
		{"one is allowed", 1, false},
		{"max is allowed", types.MaxSentinelAccuracyWindowEpochs, false},
		{"above max is rejected", types.MaxSentinelAccuracyWindowEpochs + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := types.DefaultParams()
			p.SentinelAccuracyWindowEpochs = tc.window
			if tc.wantErr {
				require.Error(t, p.Validate())
			} else {
				require.NoError(t, p.Validate())
			}

			op := types.DefaultRepOperationalParams()
			op.SentinelAccuracyWindowEpochs = tc.window
			if tc.wantErr {
				require.Error(t, op.Validate())
			} else {
				require.NoError(t, op.Validate())
			}
		})
	}
}

// TestSentinelAccuracyWindow_OperationalRoundTrip verifies the window param
// survives the OperationalParams apply/extract mapping (ops-tunable knob).
func TestSentinelAccuracyWindow_OperationalRoundTrip(t *testing.T) {
	op := types.DefaultRepOperationalParams()
	op.SentinelAccuracyWindowEpochs = 12

	applied := types.DefaultParams().ApplyOperationalParams(op)
	require.Equal(t, uint64(12), applied.SentinelAccuracyWindowEpochs)

	extracted := applied.ExtractOperationalParams()
	require.Equal(t, uint64(12), extracted.SentinelAccuracyWindowEpochs)
}

// TestSentinelAccuracyWindow_RingCapMatchesForum guards the cross-module
// invariant from the rep side too (the forum ring has a sibling assertion).
func TestSentinelAccuracyWindow_RingCapMatchesForum(t *testing.T) {
	require.Equal(t, uint64(24), types.MaxSentinelAccuracyWindowEpochs)
}
