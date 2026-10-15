package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

type goldenShareFixture struct {
	k        keeper.Keeper
	ctx      sdk.Context
	ms       types.MsgServer
	gov      string
	commons  string // Commons Council policy: the golden-share seat
	tech     string // Technical Council policy
	techGov  string // Technical Governance Committee policy (electoral)
	leadDev  string
	newDev   string
	otherDev string
}

// setupGoldenShare mirrors the genesis shape: the Technical Council holds the
// lead dev and the Commons Council seat at 3 + 3, with the Technical
// Governance Committee as its electoral delegate.
func setupGoldenShare(t *testing.T) goldenShareFixture {
	t.Helper()
	k, ctx, _ := setupCommonsKeeper(t)
	f := goldenShareFixture{
		k:        k,
		ctx:      ctx,
		ms:       keeper.NewMsgServerImpl(k),
		gov:      k.GetModuleAddressByName(govtypes.ModuleName).String(),
		commons:  keeper.DeriveCouncilAddress(1, "standard").String(),
		tech:     keeper.DeriveCouncilAddress(2, "standard").String(),
		techGov:  keeper.DeriveCouncilAddress(3, "standard").String(),
		leadDev:  sdk.AccAddress([]byte("lead_dev____________")).String(),
		newDev:   sdk.AccAddress([]byte("new_dev_____________")).String(),
		otherDev: sdk.AccAddress([]byte("other_dev___________")).String(),
	}

	setGroup := func(name string, g types.Group) {
		require.NoError(t, k.Groups.Set(ctx, name, g))
		require.NoError(t, k.PolicyToName.Set(ctx, g.PolicyAddress, name))
	}
	setGroup("Commons Council", types.Group{GroupId: 1, PolicyAddress: f.commons, ParentPolicyAddress: f.gov, MinMembers: 1, MaxMembers: 100})
	setGroup("Technical Council", types.Group{
		GroupId: 2, PolicyAddress: f.tech, ParentPolicyAddress: f.gov, ElectoralPolicyAddress: f.techGov,
		MinMembers: 2, MaxMembers: 100,
	})
	setGroup("Technical Governance Committee", types.Group{GroupId: 3, PolicyAddress: f.techGov, ParentPolicyAddress: f.tech, MinMembers: 1, MaxMembers: 5})

	require.NoError(t, k.AddMember(ctx, "Technical Council", types.Member{Address: f.leadDev, Weight: "3"}))
	require.NoError(t, k.AddMember(ctx, "Technical Council", types.Member{Address: f.commons, Weight: "3"}))
	return f
}

func (f goldenShareFixture) weight(t *testing.T, addr string) string {
	t.Helper()
	m, err := f.k.GetMember(f.ctx, "Technical Council", addr)
	require.NoError(t, err)
	return m.Weight
}

func (f goldenShareFixture) update(authority string, add []string, weights []string, remove []string) error {
	_, err := f.ms.UpdateGroupMembers(f.ctx, &types.MsgUpdateGroupMembers{
		Authority:          authority,
		GroupPolicyAddress: f.tech,
		MembersToAdd:       add,
		WeightsToAdd:       weights,
		MembersToRemove:    remove,
	})
	return err
}

// New developers join at weight 1 and the Commons Council seat grows to match
// everyone else, so it keeps half the votes.
func TestGoldenShare_SeatScalesWithNewMembers(t *testing.T) {
	f := setupGoldenShare(t)

	require.NoError(t, f.update(f.techGov, []string{f.newDev}, []string{"1"}, nil))
	require.Equal(t, "4", f.weight(t, f.commons))
	require.Equal(t, "3", f.weight(t, f.leadDev))

	require.NoError(t, f.update(f.techGov, []string{f.otherDev}, []string{"1"}, nil))
	require.Equal(t, "5", f.weight(t, f.commons))

	// Removing a developer shrinks the seat again.
	require.NoError(t, f.update(f.techGov, nil, nil, []string{f.otherDev}))
	require.Equal(t, "4", f.weight(t, f.commons))
}

func TestGoldenShare_ElectoralWeightRules(t *testing.T) {
	f := setupGoldenShare(t)

	// Only weight 1 for a new member.
	err := f.update(f.techGov, []string{f.newDev}, []string{"2"}, nil)
	require.ErrorIs(t, err, types.ErrInvalidMemberWeight)

	// A member may be re-added at the weight they already hold.
	require.NoError(t, f.update(f.techGov, []string{f.leadDev}, []string{"3"}, nil))
	require.Equal(t, "3", f.weight(t, f.leadDev))

	// But not raised.
	err = f.update(f.techGov, []string{f.leadDev}, []string{"4"}, nil)
	require.ErrorIs(t, err, types.ErrInvalidMemberWeight)

	// A weight given for the seat is ignored: it is recomputed.
	require.NoError(t, f.update(f.techGov, []string{f.commons, f.newDev}, []string{"50", "1"}, nil))
	require.Equal(t, "4", f.weight(t, f.commons))
}

func TestGoldenShare_OnlyGovRemovesSeat(t *testing.T) {
	f := setupGoldenShare(t)

	err := f.update(f.techGov, []string{f.newDev}, []string{"1"}, []string{f.commons})
	require.ErrorIs(t, err, types.ErrGoldenShareSeat)
	require.Equal(t, "3", f.weight(t, f.commons))

	require.NoError(t, f.update(f.gov, []string{f.newDev}, []string{"1"}, []string{f.commons}))
	has, err := f.k.HasMember(f.ctx, "Technical Council", f.commons)
	require.NoError(t, err)
	require.False(t, has)
}

// x/gov may set any positive weight; the seat still follows.
func TestGoldenShare_GovSetsWeights(t *testing.T) {
	f := setupGoldenShare(t)

	require.NoError(t, f.update(f.gov, []string{f.newDev}, []string{"5"}, nil))
	require.Equal(t, "5", f.weight(t, f.newDev))
	require.Equal(t, "8", f.weight(t, f.commons))
}

func TestMemberWeights_MustBePositive(t *testing.T) {
	for _, w := range []string{"0", "-1", "abc", ""} {
		f := setupGoldenShare(t)
		err := f.update(f.gov, []string{f.newDev}, []string{w}, nil)
		require.ErrorIs(t, err, types.ErrInvalidMemberWeight, "weight %q", w)
	}
}

// Groups other than the genesis governance bodies choose their own weights.
func TestMemberWeights_OtherGroupsUnrestricted(t *testing.T) {
	f := setupGoldenShare(t)
	guild := keeper.DeriveCouncilAddress(4, "standard").String()
	require.NoError(t, f.k.Groups.Set(f.ctx, "Phoenix Guild", types.Group{
		GroupId: 4, PolicyAddress: guild, ParentPolicyAddress: f.tech, MinMembers: 1, MaxMembers: 10,
	}))
	require.NoError(t, f.k.PolicyToName.Set(f.ctx, guild, "Phoenix Guild"))

	_, err := f.ms.UpdateGroupMembers(f.ctx, &types.MsgUpdateGroupMembers{
		Authority:          f.tech,
		GroupPolicyAddress: guild,
		MembersToAdd:       []string{f.newDev},
		WeightsToAdd:       []string{"2.5"},
	})
	require.NoError(t, err)
	m, err := f.k.GetMember(f.ctx, "Phoenix Guild", f.newDev)
	require.NoError(t, err)
	require.Equal(t, "2.5", m.Weight)
}

func TestGoldenShare_Renewal(t *testing.T) {
	f := setupGoldenShare(t)
	g, err := f.k.Groups.Get(f.ctx, "Technical Council")
	require.NoError(t, err)
	g.CurrentTermExpiration = f.ctx.BlockTime().Unix() - 1
	require.NoError(t, f.k.Groups.Set(f.ctx, "Technical Council", g))

	renew := func(members []string, weights []string) error {
		_, err := f.ms.RenewGroup(f.ctx, &types.MsgRenewGroup{
			Authority:        f.techGov,
			GroupName:        "Technical Council",
			NewMembers:       members,
			NewMemberWeights: weights,
		})
		return err
	}

	// Leaving the seat out of the new term would remove it.
	err = renew([]string{f.leadDev, f.newDev}, []string{"3", "1"})
	require.ErrorIs(t, err, types.ErrGoldenShareSeat)

	require.NoError(t, renew([]string{f.leadDev, f.newDev, f.commons}, []string{"3", "1", "3"}))
	require.Equal(t, "4", f.weight(t, f.commons))
}
