package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// setupAuthorBondFixture creates a test fixture with an author member who has DREAM.
func setupAuthorBondFixture(t *testing.T) (*fixture, sdk.AccAddress) {
	t.Helper()
	f := initFixture(t)

	authorAddr := sdk.AccAddress([]byte("author_bond_addr____"))

	member := types.Member{
		Address:          authorAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)), // 5000 DREAM
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{"general": "100.0"},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, member.Address, member))

	return f, authorAddr
}

func TestCreateAuthorBond_Success(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	bondAmount := math.NewInt(500000000) // 500 DREAM
	stakeID, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		bondAmount,
	)
	require.NoError(t, err)
	require.NotZero(t, stakeID)

	// Verify stored stake
	stake, err := f.keeper.Stake.Get(f.ctx, stakeID)
	require.NoError(t, err)
	require.Equal(t, authorAddr.String(), stake.Staker)
	require.Equal(t, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, stake.TargetType)
	require.Equal(t, uint64(1), stake.TargetId)
	require.Equal(t, bondAmount, stake.Amount)
	require.True(t, stake.RewardDebt.IsZero())
}

func TestCreateAuthorBond_AllTargetTypes(t *testing.T) {
	bondTypes := []types.StakeTargetType{
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
		types.StakeTargetType_STAKE_TARGET_COLLECTION_AUTHOR_BOND,
	}

	for _, bondType := range bondTypes {
		t.Run(bondType.String(), func(t *testing.T) {
			f, authorAddr := setupAuthorBondFixture(t)

			stakeID, err := f.keeper.CreateAuthorBond(
				f.ctx, authorAddr, bondType, 1, math.NewInt(100000000),
			)
			require.NoError(t, err)
			require.NotZero(t, stakeID)
		})
	}
}

func TestCreateAuthorBond_InvalidTargetType(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	// Initiative is not an author bond type
	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		1,
		math.NewInt(100),
	)
	require.ErrorIs(t, err, types.ErrNotAuthorBondType)

	// Content conviction is not an author bond type
	_, err = f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT,
		1,
		math.NewInt(100),
	)
	require.ErrorIs(t, err, types.ErrNotAuthorBondType)
}

func TestCreateAuthorBond_ZeroAmount(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		math.ZeroInt(),
	)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestCreateAuthorBond_NegativeAmount(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		math.NewInt(-100),
	)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestCreateAuthorBond_ExceedsCap(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	// Default MaxAuthorBondPerContent is 1,000 DREAM (1e9 micro-DREAM)
	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		math.NewInt(2000000000), // 2000 DREAM > 1000 cap
	)
	require.ErrorIs(t, err, types.ErrAuthorBondCap)
}

func TestCreateAuthorBond_DuplicateBond(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	// First bond succeeds
	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		math.NewInt(100000000),
	)
	require.NoError(t, err)

	// Second bond on same content fails
	_, err = f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		math.NewInt(100000000),
	)
	require.ErrorIs(t, err, types.ErrAuthorBondExists)
}

func TestCreateAuthorBond_DifferentContentIDs(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	// Bond on post 1
	id1, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		math.NewInt(100000000),
	)
	require.NoError(t, err)

	// Bond on post 2 (different content ID) should succeed
	id2, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		2,
		math.NewInt(100000000),
	)
	require.NoError(t, err)
	require.NotEqual(t, id1, id2)
}

func TestGetAuthorBond_Success(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	bondAmount := math.NewInt(500000000)
	stakeID, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		bondAmount,
	)
	require.NoError(t, err)

	bond, err := f.keeper.GetAuthorBond(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
	)
	require.NoError(t, err)
	require.Equal(t, stakeID, bond.Id)
	require.Equal(t, authorAddr.String(), bond.Staker)
	require.Equal(t, bondAmount, bond.Amount)
}

func TestGetAuthorBond_NotFound(t *testing.T) {
	f := initFixture(t)

	_, err := f.keeper.GetAuthorBond(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		999,
	)
	require.ErrorIs(t, err, types.ErrAuthorBondNotFound)
}

func TestGetAuthorBond_InvalidTargetType(t *testing.T) {
	f := initFixture(t)

	_, err := f.keeper.GetAuthorBond(
		f.ctx,
		types.StakeTargetType_STAKE_TARGET_INITIATIVE,
		1,
	)
	require.ErrorIs(t, err, types.ErrNotAuthorBondType)
}

func TestSlashAuthorBond_Success(t *testing.T) {
	f, authorAddr := setupAuthorBondFixture(t)

	bondAmount := math.NewInt(500000000)
	_, err := f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		bondAmount,
	)
	require.NoError(t, err)

	// Verify bond exists
	_, err = f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)

	// Slash the bond
	err = f.keeper.SlashAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)

	// Verify bond is gone
	_, err = f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.ErrorIs(t, err, types.ErrAuthorBondNotFound)
}

func TestSlashAuthorBond_InvalidTargetType(t *testing.T) {
	f := initFixture(t)

	err := f.keeper.SlashAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_INITIATIVE, 1)
	require.ErrorIs(t, err, types.ErrNotAuthorBondType)
}

func TestSlashAuthorBond_NoBondExists(t *testing.T) {
	f := initFixture(t)

	// Slashing a nonexistent bond should return nil (silent no-op)
	err := f.keeper.SlashAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 999)
	require.NoError(t, err)
}

func TestSlashAuthorBond_SlashingDisabled(t *testing.T) {
	params := types.DefaultParams()
	params.AuthorBondSlashOnModeration = false

	f := initFixture(t, WithCustomParams(params))
	authorAddr := sdk.AccAddress([]byte("author_no_slash_____"))

	member := types.Member{
		Address:          authorAddr.String(),
		DreamBalance:     PtrInt(math.NewInt(5000000000)),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		TrustLevel:       types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
		ReputationScores: map[string]string{},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, member.Address, member))

	// Create bond
	_, err := f.keeper.CreateAuthorBond(
		f.ctx, authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1,
		math.NewInt(100000000),
	)
	require.NoError(t, err)

	// Slash should return nil (disabled) and bond should still exist
	err = f.keeper.SlashAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)

	// Bond should still exist
	bond, err := f.keeper.GetAuthorBond(f.ctx, types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND, 1)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100000000), bond.Amount)
}
