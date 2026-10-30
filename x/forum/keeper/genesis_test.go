package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:               types.DefaultParams(),
		PostMap:              []types.Post{{PostId: 0, MediaRulesVersion: commontypes.MediaRulesVersion}, {PostId: 1, MediaRulesVersion: commontypes.MediaRulesVersion}},
		UserRateLimitMap:     []types.UserRateLimit{{UserAddress: "addr0"}, {UserAddress: "addr1"}},
		UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "addr0"}, {UserAddress: "addr1"}},
		SentinelActivityMap:  []types.SentinelActivity{{Address: "sentinel0"}, {Address: "sentinel1"}},
		HideRecordMap:        []types.HideRecord{{PostId: 0}, {PostId: 1}},
		ThreadLockRecordMap:  []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}},
		ThreadMoveRecordMap:  []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}},
		PostFlagMap:          []types.PostFlag{{PostId: 0}, {PostId: 1}},
		BountyList:           []types.Bounty{{Id: 0}, {Id: 1}},
		BountyCount:          2,
		ThreadMetadataMap:    []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}},
		ThreadFollowMap:      []types.ThreadFollow{{Follower: "follower0"}, {Follower: "follower1"}},
		ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}},
		ArchiveMetadataMap:   []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}},
	}
	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.PostMap, got.PostMap)
	require.EqualExportedValues(t, genesisState.UserRateLimitMap, got.UserRateLimitMap)
	require.EqualExportedValues(t, genesisState.UserReactionLimitMap, got.UserReactionLimitMap)
	require.EqualExportedValues(t, genesisState.SentinelActivityMap, got.SentinelActivityMap)
	require.EqualExportedValues(t, genesisState.HideRecordMap, got.HideRecordMap)
	require.EqualExportedValues(t, genesisState.ThreadLockRecordMap, got.ThreadLockRecordMap)
	require.EqualExportedValues(t, genesisState.ThreadMoveRecordMap, got.ThreadMoveRecordMap)
	require.EqualExportedValues(t, genesisState.PostFlagMap, got.PostFlagMap)
	require.EqualExportedValues(t, genesisState.BountyList, got.BountyList)
	require.Equal(t, genesisState.BountyCount, got.BountyCount)
	require.EqualExportedValues(t, genesisState.ThreadMetadataMap, got.ThreadMetadataMap)
	require.EqualExportedValues(t, genesisState.ThreadFollowMap, got.ThreadFollowMap)
	require.EqualExportedValues(t, genesisState.ThreadFollowCountMap, got.ThreadFollowCountMap)
	require.EqualExportedValues(t, genesisState.ArchiveMetadataMap, got.ArchiveMetadataMap)
}

// The promotion queue is hook-driven, so genesis carries it verbatim.
func TestGenesisKeepsPromotionQueue(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.PromotionQueue.Set(f.ctx, "phoenix", 60))
	exported, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, exported.PromotionQueue, 1)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *exported))
	at, err := g.keeper.PromotionQueue.Get(g.ctx, "phoenix")
	require.NoError(t, err)
	require.Equal(t, int64(60), at)
}

// Import indexes still-ephemeral posts by author, but not a soft-deleted
// one: MsgDeletePost dropped its entry while the post kept its
// expiration_time for the TTL prune.
func TestGenesisSkipsDeletedEphemeralInAuthorIndex(t *testing.T) {
	f := initFixture(t)
	gs := types.DefaultGenesis()
	gs.PostMap = []types.Post{
		{PostId: 1, Author: "phoenix", ExpirationTime: 1000, Status: types.PostStatus_POST_STATUS_ACTIVE, MediaRulesVersion: commontypes.MediaRulesVersion},
		{PostId: 2, Author: "phoenix", ExpirationTime: 1000, Status: types.PostStatus_POST_STATUS_DELETED, MediaRulesVersion: commontypes.MediaRulesVersion},
	}
	require.NoError(t, f.keeper.InitGenesis(f.ctx, *gs))

	has, err := f.keeper.EphemeralByAuthor.Has(f.ctx, collections.Join("phoenix", uint64(1)))
	require.NoError(t, err)
	require.True(t, has, "active ephemeral post indexed")
	has, err = f.keeper.EphemeralByAuthor.Has(f.ctx, collections.Join("phoenix", uint64(2)))
	require.NoError(t, err)
	require.False(t, has, "deleted post not indexed")
	has, err = f.keeper.ExpirationQueue.Has(f.ctx, collections.Join(int64(1000), uint64(2)))
	require.NoError(t, err)
	require.True(t, has, "deleted post still queued for the TTL prune")
}
