package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// seedRoleCandidate creates a member with configurable DREAM balance,
// reputation tier, and trust level for bond-role eligibility gate tests.
func seedRoleCandidate(t *testing.T, f *fixture, addr sdk.AccAddress, balance math.Int, repScore string, trust types.TrustLevel) {
	t.Helper()
	zero := math.ZeroInt()
	reputation := map[string]string{}
	if repScore != "" {
		reputation["backend"] = repScore
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), types.Member{
		Address:          addr.String(),
		DreamBalance:     &balance,
		StakedDream:      &zero,
		LifetimeEarned:   &zero,
		LifetimeBurned:   &zero,
		ReputationScores: reputation,
		TrustLevel:       trust,
	}))
}

func TestBondRole_HappyPath_Sentinel(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("sentinelX"))
	// Seeded default FORUM_SENTINEL config requires rep tier 3.
	seedRoleCandidate(t, f, addr, math.NewInt(5_000), "250.0", types.TrustLevel_TRUST_LEVEL_NEW)

	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "2000",
	})
	require.NoError(t, err)

	br, err := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, addr.String())
	require.NoError(t, err)
	require.Equal(t, "2000", br.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, br.BondStatus)
	require.Equal(t, types.RoleType_ROLE_TYPE_FORUM_SENTINEL, br.RoleType)

	mem, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	require.Equal(t, "2000", mem.StakedDream.String())
}

func TestBondRole_HappyPath_Curator(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("curatorX"))
	// Seeded default COLLECT_CURATOR config requires TRUST_LEVEL_PROVISIONAL.
	seedRoleCandidate(t, f, addr, math.NewInt(2_000), "", types.TrustLevel_TRUST_LEVEL_PROVISIONAL)

	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR,
		Amount:   "600",
	})
	require.NoError(t, err)

	br, err := f.keeper.GetBondedRole(f.ctx, types.RoleType_ROLE_TYPE_COLLECT_CURATOR, addr.String())
	require.NoError(t, err)
	require.Equal(t, "600", br.CurrentBond)
	require.Equal(t, types.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL, br.BondStatus)
}

func TestBondRole_RejectsInsufficientRepTier(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("novice"))
	// Reputation below tier 3.
	seedRoleCandidate(t, f, addr, math.NewInt(5_000), "10.0", types.TrustLevel_TRUST_LEVEL_NEW)

	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "2000",
	})
	require.ErrorIs(t, err, types.ErrInsufficientReputation)
}

func TestBondRole_RejectsInsufficientTrustLevel(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("greenhorn"))
	// Trust level NEW is below default COLLECT_CURATOR config's PROVISIONAL.
	seedRoleCandidate(t, f, addr, math.NewInt(2_000), "", types.TrustLevel_TRUST_LEVEL_NEW)

	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_COLLECT_CURATOR,
		Amount:   "600",
	})
	require.ErrorIs(t, err, types.ErrInsufficientReputation)
}

func TestBondRole_RejectsBelowMinBondOnFirst(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("shortboy"))
	seedRoleCandidate(t, f, addr, math.NewInt(5_000), "250.0", types.TrustLevel_TRUST_LEVEL_NEW)

	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_FORUM_SENTINEL,
		Amount:   "500", // below seeded min_bond of 1000
	})
	require.ErrorIs(t, err, types.ErrBondAmountTooSmall)
}

func TestBondRole_RejectsInvalidRoleType(t *testing.T) {
	f := initFixture(t)
	srv := keeper.NewMsgServerImpl(f.keeper)

	addr := sdk.AccAddress([]byte("any"))
	_, err := srv.BondRole(f.ctx, &types.MsgBondRole{
		Creator:  addr.String(),
		RoleType: types.RoleType_ROLE_TYPE_UNSPECIFIED,
		Amount:   "1000",
	})
	require.ErrorIs(t, err, types.ErrInvalidRoleType)
}

// UnbondRole handler tests live in msg_server_unbond_role_test.go (one
// test file per implementation file).
