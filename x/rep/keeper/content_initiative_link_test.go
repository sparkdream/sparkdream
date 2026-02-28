package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestValidateInitiativeReference(t *testing.T) {
	t.Run("valid initiative passes", func(t *testing.T) {
		f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
		k := f.keeper
		ctx := sdk.UnwrapSDKContext(f.ctx)

		_, initiativeID := SetupProjectWithInitiative(t, &k, ctx, TestAddrCreator)
		err := k.ValidateInitiativeReference(f.ctx, initiativeID)
		require.NoError(t, err)
	})

	t.Run("not found fails", func(t *testing.T) {
		f := initFixture(t)
		err := f.keeper.ValidateInitiativeReference(f.ctx, 999)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("completed initiative fails", func(t *testing.T) {
		f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
		k := f.keeper
		ctx := sdk.UnwrapSDKContext(f.ctx)

		_, initiativeID := SetupProjectWithInitiative(t, &k, ctx, TestAddrCreator)

		// Set status to completed
		initiative, err := k.GetInitiative(f.ctx, initiativeID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED
		require.NoError(t, k.UpdateInitiative(f.ctx, initiative))

		err = k.ValidateInitiativeReference(f.ctx, initiativeID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "terminal status")
	})

	t.Run("rejected initiative fails", func(t *testing.T) {
		f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
		k := f.keeper
		ctx := sdk.UnwrapSDKContext(f.ctx)

		_, initiativeID := SetupProjectWithInitiative(t, &k, ctx, TestAddrCreator)

		initiative, err := k.GetInitiative(f.ctx, initiativeID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_REJECTED
		require.NoError(t, k.UpdateInitiative(f.ctx, initiative))

		err = k.ValidateInitiativeReference(f.ctx, initiativeID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "terminal status")
	})

	t.Run("abandoned initiative fails", func(t *testing.T) {
		f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
		k := f.keeper
		ctx := sdk.UnwrapSDKContext(f.ctx)

		_, initiativeID := SetupProjectWithInitiative(t, &k, ctx, TestAddrCreator)

		initiative, err := k.GetInitiative(f.ctx, initiativeID)
		require.NoError(t, err)
		initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_ABANDONED
		require.NoError(t, k.UpdateInitiative(f.ctx, initiative))

		err = k.ValidateInitiativeReference(f.ctx, initiativeID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "terminal status")
	})
}

func TestRegisterAndRemoveContentInitiativeLink(t *testing.T) {
	t.Run("round trip register and remove", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		initiativeID := uint64(1)
		targetType := int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT)
		targetID := uint64(42)

		// Register
		err := k.RegisterContentInitiativeLink(f.ctx, initiativeID, targetType, targetID)
		require.NoError(t, err)

		// Verify it exists
		links, err := k.GetContentInitiativeLinks(f.ctx, initiativeID)
		require.NoError(t, err)
		require.Len(t, links, 1)
		require.Equal(t, targetType, links[0].TargetType)
		require.Equal(t, targetID, links[0].TargetID)

		// Remove
		err = k.RemoveContentInitiativeLink(f.ctx, initiativeID, targetType, targetID)
		require.NoError(t, err)

		// Verify gone
		links, err = k.GetContentInitiativeLinks(f.ctx, initiativeID)
		require.NoError(t, err)
		require.Empty(t, links)
	})

	t.Run("multiple links for same initiative", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		initiativeID := uint64(1)
		blogType := int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT)

		// Register two posts
		require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, initiativeID, blogType, 10))
		require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, initiativeID, blogType, 20))

		links, err := k.GetContentInitiativeLinks(f.ctx, initiativeID)
		require.NoError(t, err)
		require.Len(t, links, 2)
	})

	t.Run("links scoped to initiative", func(t *testing.T) {
		f := initFixture(t)
		k := f.keeper

		blogType := int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT)

		// Register links for two different initiatives
		require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, 1, blogType, 10))
		require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, 2, blogType, 20))

		// Query each initiative separately
		links1, err := k.GetContentInitiativeLinks(f.ctx, 1)
		require.NoError(t, err)
		require.Len(t, links1, 1)
		require.Equal(t, uint64(10), links1[0].TargetID)

		links2, err := k.GetContentInitiativeLinks(f.ctx, 2)
		require.NoError(t, err)
		require.Len(t, links2, 1)
		require.Equal(t, uint64(20), links2[0].TargetID)
	})
}

func TestGetPropagatedConviction_NoLinks(t *testing.T) {
	f := initFixture(t)

	conviction, err := f.keeper.GetPropagatedConviction(f.ctx, 1, "", "")
	require.NoError(t, err)
	require.True(t, conviction.IsZero(), "expected zero propagated conviction with no links")
}

func TestGetPropagatedConviction_ZeroRatio(t *testing.T) {
	params := types.DefaultParams()
	params.ConvictionPropagationRatio = math.LegacyZeroDec()
	f := initFixture(t, WithCustomParams(params))
	k := f.keeper

	// Register a link (content conviction won't matter since ratio is 0)
	blogType := int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT)
	require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, 1, blogType, 10))

	conviction, err := k.GetPropagatedConviction(f.ctx, 1, "", "")
	require.NoError(t, err)
	require.True(t, conviction.IsZero(), "expected zero propagated conviction with zero ratio")
}

func TestGetPropagatedConviction_WithLinks(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	// Set block time so stake has elapsed time for conviction
	createdAt := int64(1000000)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(createdAt+86400, 0)) // 1 day later

	// Create a member (required for staking)
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrStaker))

	// Create an initiative to link to
	_, initiativeID := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)

	// Create a content conviction stake (blog post 10)
	blogType := types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT
	stakeAmount := math.NewInt(1000)
	stake := types.Stake{
		Id:         100,
		Staker:     TestAddrStaker.String(),
		TargetType: blogType,
		TargetId:   10,
		Amount:     stakeAmount,
		CreatedAt:  createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, stake.Id, stake))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, stake))

	// Register the content-initiative link
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 10))

	// Get propagated conviction (pass creator address for affiliation filtering)
	propagated, err := k.GetPropagatedConviction(sdkCtx, initiativeID, "", TestAddrCreator.String())
	require.NoError(t, err)
	require.True(t, propagated.IsPositive(), "expected positive propagated conviction, got %s", propagated)

	// Verify the ratio is applied correctly
	// Content conviction for the stake = amount * min(1, t / (2 * halfLife))
	// Default ContentConvictionHalfLifeEpochs=14, EpochBlocks=100, 6s/block
	// halfLifeSeconds = 14 * 100 * 6 = 8400
	// timeFactor = 86400 / (2 * 8400) = 86400 / 16800 = 5.14... capped at 1.0
	// So content conviction = 1000 * 1.0 = 1000
	// Propagated = 1000 * 0.10 = 100
	contentConviction, err := k.GetContentConviction(sdkCtx, blogType, 10)
	require.NoError(t, err)

	params, err := k.Params.Get(sdkCtx)
	require.NoError(t, err)

	expectedPropagated := contentConviction.Mul(params.ConvictionPropagationRatio)
	require.True(t, propagated.Equal(expectedPropagated),
		"expected propagated=%s (content=%s * ratio=%s), got %s",
		expectedPropagated, contentConviction, params.ConvictionPropagationRatio, propagated)
}

func TestGetPropagatedConviction_MultipleLinks(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	createdAt := int64(1000000)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(createdAt+86400, 0))

	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrStaker))
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrMember1))

	_, initiativeID := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)

	blogType := types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT

	// Stake on blog post 10
	stake1 := types.Stake{
		Id:         100,
		Staker:     TestAddrStaker.String(),
		TargetType: blogType,
		TargetId:   10,
		Amount:     math.NewInt(500),
		CreatedAt:  createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, stake1.Id, stake1))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, stake1))

	// Stake on blog post 20
	stake2 := types.Stake{
		Id:         101,
		Staker:     TestAddrMember1.String(),
		TargetType: blogType,
		TargetId:   20,
		Amount:     math.NewInt(300),
		CreatedAt:  createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, stake2.Id, stake2))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, stake2))

	// Link both posts to the same initiative
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 10))
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 20))

	propagated, err := k.GetPropagatedConviction(sdkCtx, initiativeID, "", TestAddrCreator.String())
	require.NoError(t, err)
	require.True(t, propagated.IsPositive())

	// Should be sum of both content convictions * ratio
	c1, _ := k.GetContentConviction(sdkCtx, blogType, 10)
	c2, _ := k.GetContentConviction(sdkCtx, blogType, 20)
	params, _ := k.Params.Get(sdkCtx)
	expected := c1.Add(c2).Mul(params.ConvictionPropagationRatio)
	require.True(t, propagated.Equal(expected),
		"expected %s, got %s", expected, propagated)
}

func TestUpdateInitiativeConvictionLazy_WithPropagation(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	createdAt := int64(1000000)
	sdkCtx = sdkCtx.WithBlockTime(time.Unix(createdAt+86400, 0)).WithBlockHeight(50)

	// Setup members
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))
	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrStaker))

	// Create project + initiative
	_, initiativeID := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)

	// Create a content stake on blog post 10
	blogType := types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT
	stake := types.Stake{
		Id:         100,
		Staker:     TestAddrStaker.String(),
		TargetType: blogType,
		TargetId:   10,
		Amount:     math.NewInt(2000),
		CreatedAt:  createdAt,
	}
	require.NoError(t, k.Stake.Set(sdkCtx, stake.Id, stake))
	require.NoError(t, k.AddStakeToTargetIndex(sdkCtx, stake))

	// Link post to initiative
	require.NoError(t, k.RegisterContentInitiativeLink(sdkCtx, initiativeID, int32(blogType), 10))

	// Trigger lazy conviction update
	err := k.UpdateInitiativeConvictionLazy(sdkCtx, initiativeID)
	require.NoError(t, err)

	// Verify initiative now has propagated conviction
	initiative, err := k.GetInitiative(sdkCtx, initiativeID)
	require.NoError(t, err)

	require.NotNil(t, initiative.PropagatedConviction)
	require.True(t, initiative.PropagatedConviction.IsPositive(),
		"expected positive propagated conviction, got %s", initiative.PropagatedConviction)

	// Propagated conviction should be included in total and external
	require.NotNil(t, initiative.CurrentConviction)
	require.True(t, initiative.CurrentConviction.GTE(*initiative.PropagatedConviction),
		"total conviction %s should be >= propagated %s",
		initiative.CurrentConviction, initiative.PropagatedConviction)

	require.NotNil(t, initiative.ExternalConviction)
	require.True(t, initiative.ExternalConviction.GTE(*initiative.PropagatedConviction),
		"external conviction %s should be >= propagated %s",
		initiative.ExternalConviction, initiative.PropagatedConviction)
}

func TestUpdateInitiativeConvictionLazy_NoPropagationWithoutLinks(t *testing.T) {
	f := initFixture(t, WithAuthorizationPolicy(AlwaysAuthorized))
	k := f.keeper
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)

	sdkCtx = sdkCtx.WithBlockTime(time.Unix(1000000, 0)).WithBlockHeight(50)

	SetupMember(t, &k, sdkCtx, DefaultMemberConfig(TestAddrCreator))

	_, initiativeID := SetupProjectWithInitiative(t, &k, sdkCtx, TestAddrCreator)

	err := k.UpdateInitiativeConvictionLazy(sdkCtx, initiativeID)
	require.NoError(t, err)

	initiative, err := k.GetInitiative(sdkCtx, initiativeID)
	require.NoError(t, err)

	// PropagatedConviction should be zero (no links)
	if initiative.PropagatedConviction != nil {
		require.True(t, initiative.PropagatedConviction.IsZero(),
			"expected zero propagated conviction without links, got %s", initiative.PropagatedConviction)
	}
}

func TestGenesisContentInitiativeLinks(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	blogType := int32(types.StakeTargetType_STAKE_TARGET_BLOG_CONTENT)

	// Register some links
	require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, 1, blogType, 10))
	require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, 1, blogType, 20))
	require.NoError(t, k.RegisterContentInitiativeLink(f.ctx, 2, blogType, 30))

	// Export genesis
	genesis, err := k.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, genesis.ContentInitiativeLinks, 3)

	// Verify link data
	linkMap := make(map[uint64][]uint64)
	for _, link := range genesis.ContentInitiativeLinks {
		linkMap[link.InitiativeId] = append(linkMap[link.InitiativeId], link.TargetId)
		require.Equal(t, blogType, link.TargetType)
	}
	require.Len(t, linkMap[1], 2, "initiative 1 should have 2 links")
	require.Len(t, linkMap[2], 1, "initiative 2 should have 1 link")

	// Create fresh keeper and import
	f2 := initFixture(t)
	k2 := f2.keeper
	require.NoError(t, k2.InitGenesis(f2.ctx, *genesis))

	// Verify round-trip
	links1, err := k2.GetContentInitiativeLinks(f2.ctx, 1)
	require.NoError(t, err)
	require.Len(t, links1, 2)

	links2, err := k2.GetContentInitiativeLinks(f2.ctx, 2)
	require.NoError(t, err)
	require.Len(t, links2, 1)
}

func TestGenesisValidation_ContentInitiativeLinks(t *testing.T) {
	t.Run("valid links pass", func(t *testing.T) {
		gs := types.DefaultGenesis()
		gs.ContentInitiativeLinks = []types.ContentInitiativeLink{
			{InitiativeId: 1, TargetType: 4, TargetId: 10},
			{InitiativeId: 1, TargetType: 4, TargetId: 20},
		}
		require.NoError(t, gs.Validate())
	})

	t.Run("zero initiative_id fails", func(t *testing.T) {
		gs := types.DefaultGenesis()
		gs.ContentInitiativeLinks = []types.ContentInitiativeLink{
			{InitiativeId: 0, TargetType: 4, TargetId: 10},
		}
		require.Error(t, gs.Validate())
	})

	t.Run("duplicate link fails", func(t *testing.T) {
		gs := types.DefaultGenesis()
		gs.ContentInitiativeLinks = []types.ContentInitiativeLink{
			{InitiativeId: 1, TargetType: 4, TargetId: 10},
			{InitiativeId: 1, TargetType: 4, TargetId: 10},
		}
		require.Error(t, gs.Validate())
	})
}
