package keeper_test

import (
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params:                   types.DefaultParams(),
		PostMap:                  []types.Post{{PostId: 0}, {PostId: 1}},
		UserRateLimitMap:         []types.UserRateLimit{{UserAddress: "addr0"}, {UserAddress: "addr1"}},
		UserReactionLimitMap:     []types.UserReactionLimit{{UserAddress: "addr0"}, {UserAddress: "addr1"}},
		SentinelActivityMap:      []types.SentinelActivity{{Address: "sentinel0"}, {Address: "sentinel1"}},
		HideRecordMap:            []types.HideRecord{{PostId: 0}, {PostId: 1}},
		ThreadLockRecordMap:      []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}},
		ThreadMoveRecordMap:      []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}},
		PostFlagMap:              []types.PostFlag{{PostId: 0}, {PostId: 1}},
		BountyList:               []types.Bounty{{Id: 0}, {Id: 1}},
		BountyCount:              2,
		ThreadMetadataMap:        []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}},
		ThreadFollowMap:          []types.ThreadFollow{{Follower: "follower0"}, {Follower: "follower1"}},
		ThreadFollowCountMap:     []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}},
		ArchiveMetadataMap:       []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}},
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
