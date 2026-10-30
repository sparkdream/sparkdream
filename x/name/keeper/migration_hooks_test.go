package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/name/keeper"
	nametypes "sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
)

// TestMigrationHooks_NamesFollowIdentity: owned names, the owner record and
// forward targets move; a target that already owns a name is refused.
func TestMigrationHooks_NamesFollowIdentity(t *testing.T) {
	f := initFixture(t)
	from := sdk.AccAddress([]byte("name-from__________"))
	to := sdk.AccAddress([]byte("name-to____________"))
	other := sdk.AccAddress([]byte("name-other_________"))
	require.NoError(t, f.keeper.ClaimName(f.ctx, "phoenix", from.String(), "guild"))
	require.NoError(t, f.keeper.SetPrimaryName(f.ctx, from, "phoenix"))
	require.NoError(t, f.keeper.ClaimName(f.ctx, "aurora", other.String(), "guild"))
	// aurora points at from (accepted).
	rec, _ := f.keeper.GetName(f.ctx, "aurora")
	rec.Target = from.String()
	rec.TargetAccepted = true
	require.NoError(t, f.keeper.SetName(f.ctx, rec))
	require.NoError(t, f.keeper.AcceptedTargets.Set(f.ctx, collections.Join(from.String(), "aurora")))

	h := keeper.NewMigrationHooks(&f.keeper)
	// A target that owns a name is not fresh.
	require.Error(t, h.BeforeMemberMigration(f.ctx, from, other, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	require.NoError(t, h.AfterMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))

	owner, found := f.keeper.GetNameOwner(f.ctx, "phoenix")
	require.True(t, found)
	require.Equal(t, to, owner)
	n, _ := f.keeper.GetOwnedNamesCount(f.ctx, from)
	require.Zero(t, n)
	info, err := f.keeper.Owners.Get(f.ctx, to.String())
	require.NoError(t, err)
	require.Equal(t, "phoenix", info.PrimaryName)
	_, err = f.keeper.Owners.Get(f.ctx, from.String())
	require.Error(t, err)
	rec, _ = f.keeper.GetName(f.ctx, "aurora")
	require.Equal(t, to.String(), rec.Target)
	require.True(t, rec.TargetAccepted)
	has, _ := f.keeper.AcceptedTargets.Has(f.ctx, collections.Join(to.String(), "aurora"))
	require.True(t, has)
}

// REP-R-04 / NAM-2: a resolved dispute never blocks migration; an active one
// blocks only the KEY path, and follows the name on RECOVERY and COUNCIL.
func TestMigrationHooks_DisputeActiveOnlyAndPathAware(t *testing.T) {
	f := initFixture(t)
	from := sdk.AccAddress([]byte("name-disp-from_____"))
	to := sdk.AccAddress([]byte("name-disp-to_______"))
	claimant := sdk.AccAddress([]byte("name-disp-claimant_"))
	require.NoError(t, f.keeper.ClaimName(f.ctx, "zenith", from.String(), "guild"))
	h := keeper.NewMigrationHooks(&f.keeper)

	d := nametypes.Dispute{Name: "zenith", Claimant: claimant.String(), FiledAt: 1, StakeAmount: math.NewInt(1), Active: true}
	require.NoError(t, f.keeper.SetDispute(f.ctx, d))
	require.Error(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_RECOVERY))
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_COUNCIL))

	// Dismissed: the record stays as history and blocks nothing.
	d.Active = false
	require.NoError(t, f.keeper.SetDispute(f.ctx, d))
	require.NoError(t, h.BeforeMemberMigration(f.ctx, from, to, reptypes.MigrationPath_MIGRATION_PATH_KEY))
}
