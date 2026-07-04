package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

// Validation rules for the sentinel self-correct window and the per-sentinel
// daily hide cap (see docs/x-collect-spec.md 5.25/5.26a).
func TestParamsValidate_SentinelModeration(t *testing.T) {
	t.Run("defaults valid", func(t *testing.T) {
		require.NoError(t, types.DefaultParams().Validate())
	})

	t.Run("unhide window must be positive", func(t *testing.T) {
		p := types.DefaultParams()
		p.SentinelUnhideWindowBlocks = 0
		require.ErrorContains(t, p.Validate(), "sentinel_unhide_window_blocks must be positive")
	})

	t.Run("unhide window must be below hide expiry", func(t *testing.T) {
		p := types.DefaultParams()
		p.SentinelUnhideWindowBlocks = p.HideExpiryBlocks // == is already too late
		require.ErrorContains(t, p.Validate(), "must be less than hide_expiry_blocks")
	})

	t.Run("daily hide cap must be positive", func(t *testing.T) {
		p := types.DefaultParams()
		p.MaxHidesPerSentinelPerDay = 0
		require.ErrorContains(t, p.Validate(), "max_hides_per_sentinel_per_day must be positive")
	})

	t.Run("operational merge applies positive overrides only", func(t *testing.T) {
		p := types.DefaultParams()
		op := types.CollectOperationalParams{
			SentinelUnhideWindowBlocks: 99,
			MaxHidesPerSentinelPerDay:  7,
		}
		p = p.ApplyOperationalParams(op)
		require.Equal(t, int64(99), p.SentinelUnhideWindowBlocks)
		require.Equal(t, uint32(7), p.MaxHidesPerSentinelPerDay)

		// Zero-valued fields leave the current values untouched.
		p = p.ApplyOperationalParams(types.CollectOperationalParams{})
		require.Equal(t, int64(99), p.SentinelUnhideWindowBlocks)
		require.Equal(t, uint32(7), p.MaxHidesPerSentinelPerDay)
	})
}
