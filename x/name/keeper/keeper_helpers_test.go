package keeper_test

import (
	"testing"

	"sparkdream/x/name/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// --- Params Helper ---

func TestGetParams_Default(t *testing.T) {
	f := initFixture(t)

	params := f.keeper.GetParams(f.ctx)
	require.Equal(t, types.DefaultParams(), params)
}

func TestGetParams_Custom(t *testing.T) {
	f := initFixture(t)

	custom := types.DefaultParams()
	custom.MaxNamesPerAddress = 10
	require.NoError(t, f.keeper.Params.Set(f.ctx, custom))

	got := f.keeper.GetParams(f.ctx)
	require.Equal(t, uint64(10), got.MaxNamesPerAddress)
}

// --- Name Helpers ---

func TestSetName_GetName(t *testing.T) {
	f := initFixture(t)

	record := types.NameRecord{
		Name:  "alice",
		Owner: sdk.AccAddress([]byte("owner_alice_________")).String(),
	}
	require.NoError(t, f.keeper.SetName(f.ctx, record))

	got, found := f.keeper.GetName(f.ctx, "alice")
	require.True(t, found)
	require.Equal(t, "alice", got.Name)
	require.Equal(t, record.Owner, got.Owner)
}

func TestGetName_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetName(f.ctx, "nonexistent")
	require.False(t, found)
}

func TestGetNameOwner(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_getname_______"))
	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{
		Name: "zenith", Owner: owner.String(),
	}))

	gotOwner, found := f.keeper.GetNameOwner(f.ctx, "zenith")
	require.True(t, found)
	require.Equal(t, owner, gotOwner)
}

func TestGetNameOwner_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetNameOwner(f.ctx, "ghost")
	require.False(t, found)
}

func TestIsNameAvailable(t *testing.T) {
	f := initFixture(t)

	require.True(t, f.keeper.IsNameAvailable(f.ctx, "available"))

	require.NoError(t, f.keeper.SetName(f.ctx, types.NameRecord{
		Name: "taken", Owner: sdk.AccAddress([]byte("owner_______________")).String(),
	}))
	require.False(t, f.keeper.IsNameAvailable(f.ctx, "taken"))
}

// --- ClaimName (atomic cross-module registration) ---

func TestClaimName_Success(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_claim_success_"))
	require.NoError(t, f.keeper.ClaimName(f.ctx, "phoenix", owner.String(), "guild"))

	got, found := f.keeper.GetName(f.ctx, "phoenix")
	require.True(t, found)
	require.Equal(t, owner.String(), got.Owner)
	require.Equal(t, "guild", got.Data)

	count, err := f.keeper.GetOwnedNamesCount(f.ctx, owner)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
}

func TestClaimName_AlreadyTaken(t *testing.T) {
	f := initFixture(t)

	first := sdk.AccAddress([]byte("owner_claim_first___"))
	second := sdk.AccAddress([]byte("owner_claim_second__"))
	require.NoError(t, f.keeper.ClaimName(f.ctx, "aurora", first.String(), "guild"))

	err := f.keeper.ClaimName(f.ctx, "aurora", second.String(), "guild")
	require.ErrorIs(t, err, types.ErrNameTaken)
}

func TestClaimName_BlockedName(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_claim_blocked_"))
	// "admin" is in DefaultBlockedNames
	err := f.keeper.ClaimName(f.ctx, "admin", owner.String(), "guild")
	require.ErrorIs(t, err, types.ErrNameReserved)
}

func TestClaimName_TooManyNames(t *testing.T) {
	f := initFixture(t)

	// Constrain the per-address limit to 1.
	params := f.keeper.GetParams(f.ctx)
	params.MaxNamesPerAddress = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	owner := sdk.AccAddress([]byte("owner_claim_cap_____"))
	require.NoError(t, f.keeper.ClaimName(f.ctx, "zenith", owner.String(), "guild"))

	err := f.keeper.ClaimName(f.ctx, "nova", owner.String(), "guild")
	require.ErrorIs(t, err, types.ErrTooManyNames)
}

func TestClaimName_InvalidOwner(t *testing.T) {
	f := initFixture(t)

	err := f.keeper.ClaimName(f.ctx, "lyra", "not-a-bech32-address", "guild")
	require.Error(t, err)
}

// --- Owner Name Index ---

func TestAddNameToOwner_And_GetOwnedNamesCount(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_count_________"))

	count, err := f.keeper.GetOwnedNamesCount(f.ctx, owner)
	require.NoError(t, err)
	require.Equal(t, uint64(0), count)

	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner, "name1"))
	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner, "name2"))

	count, err = f.keeper.GetOwnedNamesCount(f.ctx, owner)
	require.NoError(t, err)
	require.Equal(t, uint64(2), count)
}

func TestRemoveNameFromOwner(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_remove________"))
	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner, "alpha"))
	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner, "beta"))

	require.NoError(t, f.keeper.RemoveNameFromOwner(f.ctx, owner, "alpha"))

	count, err := f.keeper.GetOwnedNamesCount(f.ctx, owner)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count)
}

func TestOwnerIsolation(t *testing.T) {
	f := initFixture(t)

	owner1 := sdk.AccAddress([]byte("owner1_isolation_____"))
	owner2 := sdk.AccAddress([]byte("owner2_isolation_____"))

	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner1, "name_a"))
	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner1, "name_b"))
	require.NoError(t, f.keeper.AddNameToOwner(f.ctx, owner2, "name_c"))

	count1, _ := f.keeper.GetOwnedNamesCount(f.ctx, owner1)
	count2, _ := f.keeper.GetOwnedNamesCount(f.ctx, owner2)
	require.Equal(t, uint64(2), count1)
	require.Equal(t, uint64(1), count2)
}

// --- Primary Name ---

func TestSetPrimaryName(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_primary_______"))
	require.NoError(t, f.keeper.SetPrimaryName(f.ctx, owner, "myname"))

	// Read it back from owners collection
	info, err := f.keeper.Owners.Get(f.ctx, owner.String())
	require.NoError(t, err)
	require.Equal(t, "myname", info.PrimaryName)
}

func TestSetPrimaryName_Overwrite(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_primary_ow____"))
	require.NoError(t, f.keeper.SetPrimaryName(f.ctx, owner, "first"))
	require.NoError(t, f.keeper.SetPrimaryName(f.ctx, owner, "second"))

	info, err := f.keeper.Owners.Get(f.ctx, owner.String())
	require.NoError(t, err)
	require.Equal(t, "second", info.PrimaryName)
}

// --- Last Active Time ---

func TestGetLastActiveTime_Default(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_lat_default___"))
	lat := f.keeper.GetLastActiveTime(f.ctx, owner)
	require.Equal(t, int64(0), lat)
}

func TestGetLastActiveTime_Set(t *testing.T) {
	f := initFixture(t)

	owner := sdk.AccAddress([]byte("owner_lat_set_______"))
	require.NoError(t, f.keeper.Owners.Set(f.ctx, owner.String(), types.OwnerInfo{
		Address:        owner.String(),
		LastActiveTime: 12345,
	}))

	lat := f.keeper.GetLastActiveTime(f.ctx, owner)
	require.Equal(t, int64(12345), lat)
}

// --- Dispute Helpers ---

func TestSetDispute_GetDispute(t *testing.T) {
	f := initFixture(t)

	dispute := types.Dispute{
		Name:     "disputed-name",
		Claimant: sdk.AccAddress([]byte("claimant____________")).String(),
		Active:   true,
	}
	require.NoError(t, f.keeper.SetDispute(f.ctx, dispute))

	got, found := f.keeper.GetDispute(f.ctx, "disputed-name")
	require.True(t, found)
	require.Equal(t, "disputed-name", got.Name)
	require.Equal(t, dispute.Claimant, got.Claimant)
	require.True(t, got.Active)
}

func TestGetDispute_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetDispute(f.ctx, "no-dispute")
	require.False(t, found)
}

func TestRemoveDispute(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDispute(f.ctx, types.Dispute{Name: "to-remove"}))

	_, found := f.keeper.GetDispute(f.ctx, "to-remove")
	require.True(t, found)

	require.NoError(t, f.keeper.RemoveDispute(f.ctx, "to-remove"))

	_, found = f.keeper.GetDispute(f.ctx, "to-remove")
	require.False(t, found)
}

// --- Authority Helpers ---

func TestIsGovAuthority(t *testing.T) {
	f := initFixture(t)

	authorityAddr, _ := f.addressCodec.BytesToString(sdk.AccAddress([]byte("authority")))
	require.True(t, f.keeper.IsGovAuthority(authorityAddr))

	randomAddr := sdk.AccAddress([]byte("random______________")).String()
	require.False(t, f.keeper.IsGovAuthority(randomAddr))
}

func TestIsCommonsCouncilMember(t *testing.T) {
	f := initFixture(t)

	memberAddr := sdk.AccAddress([]byte("council_member______")).String()

	// Not a member initially
	isMember, err := f.keeper.IsCommonsCouncilMember(f.ctx, memberAddr)
	require.NoError(t, err)
	require.False(t, isMember)

	// Add as member
	f.mockCommons.Members["Commons Council|"+memberAddr] = true

	isMember, err = f.keeper.IsCommonsCouncilMember(f.ctx, memberAddr)
	require.NoError(t, err)
	require.True(t, isMember)
}
