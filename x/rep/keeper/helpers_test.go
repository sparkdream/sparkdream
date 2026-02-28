package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// ---------------------------------------------------------------------------
// TestIsOperationsCommittee
// ---------------------------------------------------------------------------

func TestIsOperationsCommittee(t *testing.T) {
	addr := sdk.AccAddress([]byte("test_operations_addr"))

	t.Run("nil commonsKeeper returns false", func(t *testing.T) {
		f := initFixtureNilCommons(t)
		result := f.keeper.IsOperationsCommittee(f.ctx, addr)
		require.False(t, result)
	})

	t.Run("technical/operations member returns true", func(t *testing.T) {
		f := initFixture(t)
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			if council == "technical" && committee == "operations" && address.Equals(addr) {
				return true, nil
			}
			return false, nil
		}

		result := f.keeper.IsOperationsCommittee(f.ctx, addr)
		require.True(t, result)
	})

	t.Run("commons/operations member returns true", func(t *testing.T) {
		f := initFixture(t)
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			if council == "commons" && committee == "operations" && address.Equals(addr) {
				return true, nil
			}
			return false, nil
		}

		result := f.keeper.IsOperationsCommittee(f.ctx, addr)
		require.True(t, result)
	})

	t.Run("not a member of any operations committee returns false", func(t *testing.T) {
		f := initFixture(t)
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ string) (bool, error) {
			return false, nil
		}

		result := f.keeper.IsOperationsCommittee(f.ctx, addr)
		require.False(t, result)
	})
}

// ---------------------------------------------------------------------------
// TestIsHRCommittee
// ---------------------------------------------------------------------------

func TestIsHRCommittee(t *testing.T) {
	addr := sdk.AccAddress([]byte("test_hr_member_addr_"))

	t.Run("nil commonsKeeper returns false", func(t *testing.T) {
		f := initFixtureNilCommons(t)
		result := f.keeper.IsHRCommittee(f.ctx, addr)
		require.False(t, result)
	})

	t.Run("commons/hr member returns true", func(t *testing.T) {
		f := initFixture(t)
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			if council == "commons" && committee == "hr" && address.Equals(addr) {
				return true, nil
			}
			return false, nil
		}

		result := f.keeper.IsHRCommittee(f.ctx, addr)
		require.True(t, result)
	})

	t.Run("not an hr member returns false", func(t *testing.T) {
		f := initFixture(t)
		f.commonsKeeper.IsCommitteeMemberFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ string) (bool, error) {
			return false, nil
		}

		result := f.keeper.IsHRCommittee(f.ctx, addr)
		require.False(t, result)
	})
}

// ---------------------------------------------------------------------------
// TestGetReputationForTags
// ---------------------------------------------------------------------------

func TestGetReputationForTags(t *testing.T) {
	t.Run("empty tags returns zero", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("rep_tags_empty_addr_"))

		result, err := f.keeper.GetReputationForTags(f.ctx, addr, []string{})
		require.NoError(t, err)
		require.True(t, result.IsZero(), "expected zero for empty tags, got %s", result)
	})

	t.Run("member with no matching tags returns zero", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("rep_tags_nomatch_addr"))

		// Create a member with reputation in "go" and "rust", but query for "python"
		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"go": "100.5", "rust": "50.0"},
		})
		require.NoError(t, err)

		result, err := f.keeper.GetReputationForTags(f.ctx, addr, []string{"python"})
		require.NoError(t, err)
		require.True(t, result.IsZero(), "expected zero for non-matching tags, got %s", result)
	})

	t.Run("member with one matching tag returns that score", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("rep_tags_one_addr___"))

		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"go": "100.5", "rust": "50.0"},
		})
		require.NoError(t, err)

		result, err := f.keeper.GetReputationForTags(f.ctx, addr, []string{"go"})
		require.NoError(t, err)

		expected, err := math.LegacyNewDecFromStr("100.5")
		require.NoError(t, err)
		require.True(t, result.Equal(expected), "expected %s, got %s", expected, result)
	})

	t.Run("member with multiple matching tags returns average", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("rep_tags_multi_addr_"))

		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"go": "100.0", "rust": "50.0"},
		})
		require.NoError(t, err)

		result, err := f.keeper.GetReputationForTags(f.ctx, addr, []string{"go", "rust"})
		require.NoError(t, err)

		// Average of 100.0 and 50.0 = 75.0
		expected, err := math.LegacyNewDecFromStr("75.0")
		require.NoError(t, err)
		require.True(t, result.Equal(expected), "expected %s, got %s", expected, result)
	})

	t.Run("member not found returns error", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("rep_tags_missing_addr"))

		_, err := f.keeper.GetReputationForTags(f.ctx, addr, []string{"go"})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})
}

// ---------------------------------------------------------------------------
// TestIncrementMemberCompletedInterims
// ---------------------------------------------------------------------------

func TestIncrementMemberCompletedInterims(t *testing.T) {
	t.Run("increments from 0 to 1", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("interims_incr_addr__"))

		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:                addr.String(),
			DreamBalance:           PtrInt(math.ZeroInt()),
			StakedDream:            PtrInt(math.ZeroInt()),
			LifetimeEarned:         PtrInt(math.ZeroInt()),
			LifetimeBurned:         PtrInt(math.ZeroInt()),
			CompletedInterimsCount: 0,
		})
		require.NoError(t, err)

		err = f.keeper.IncrementMemberCompletedInterims(f.ctx, addr)
		require.NoError(t, err)

		member, err := f.keeper.Member.Get(f.ctx, addr.String())
		require.NoError(t, err)
		require.Equal(t, uint32(1), member.CompletedInterimsCount)
	})

	t.Run("multiple increments work correctly", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("interims_multi_addr_"))

		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:                addr.String(),
			DreamBalance:           PtrInt(math.ZeroInt()),
			StakedDream:            PtrInt(math.ZeroInt()),
			LifetimeEarned:         PtrInt(math.ZeroInt()),
			LifetimeBurned:         PtrInt(math.ZeroInt()),
			CompletedInterimsCount: 0,
		})
		require.NoError(t, err)

		for i := 0; i < 5; i++ {
			err = f.keeper.IncrementMemberCompletedInterims(f.ctx, addr)
			require.NoError(t, err)
		}

		member, err := f.keeper.Member.Get(f.ctx, addr.String())
		require.NoError(t, err)
		require.Equal(t, uint32(5), member.CompletedInterimsCount)
	})

	t.Run("non-existent member returns error", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("interims_missing_addr"))

		err := f.keeper.IncrementMemberCompletedInterims(f.ctx, addr)
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// TestIncrementMemberCompletedInitiatives
// ---------------------------------------------------------------------------

func TestIncrementMemberCompletedInitiatives(t *testing.T) {
	t.Run("increments from 0 to 1", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("init_incr_addr______"))

		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:                   addr.String(),
			DreamBalance:              PtrInt(math.ZeroInt()),
			StakedDream:               PtrInt(math.ZeroInt()),
			LifetimeEarned:            PtrInt(math.ZeroInt()),
			LifetimeBurned:            PtrInt(math.ZeroInt()),
			CompletedInitiativesCount: 0,
		})
		require.NoError(t, err)

		err = f.keeper.IncrementMemberCompletedInitiatives(f.ctx, addr)
		require.NoError(t, err)

		member, err := f.keeper.Member.Get(f.ctx, addr.String())
		require.NoError(t, err)
		require.Equal(t, uint32(1), member.CompletedInitiativesCount)
	})

	t.Run("multiple increments work correctly", func(t *testing.T) {
		f := initFixture(t)
		addr := sdk.AccAddress([]byte("init_multi_addr_____"))

		err := f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
			Address:                   addr.String(),
			DreamBalance:              PtrInt(math.ZeroInt()),
			StakedDream:               PtrInt(math.ZeroInt()),
			LifetimeEarned:            PtrInt(math.ZeroInt()),
			LifetimeBurned:            PtrInt(math.ZeroInt()),
			CompletedInitiativesCount: 0,
		})
		require.NoError(t, err)

		for i := 0; i < 3; i++ {
			err = f.keeper.IncrementMemberCompletedInitiatives(f.ctx, addr)
			require.NoError(t, err)
		}

		member, err := f.keeper.Member.Get(f.ctx, addr.String())
		require.NoError(t, err)
		require.Equal(t, uint32(3), member.CompletedInitiativesCount)
	})
}
