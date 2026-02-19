package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestUpvoteContent(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		creator        string
		targetType     types.FlagTargetType
		expErr         bool
		expErrContains string
	}{
		{
			name: "success member upvotes collection",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			creator:    "", // set in loop to f.member
			targetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		},
		{
			name: "error not member",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			creator:        "nonMember",
			targetType:     types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			expErr:         true,
			expErrContains: "not an active x/rep member",
		},
		{
			name: "error already voted",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// First upvote succeeds
				_, err := f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
					Creator:    f.member,
					TargetId:   collID,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
				})
				require.NoError(t, err)
				return collID
			},
			creator:        "", // f.member
			targetType:     types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			expErr:         true,
			expErrContains: "already voted",
		},
		{
			name: "error own content - owner votes on own collection",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			creator:        "owner",
			targetType:     types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			expErr:         true,
			expErrContains: "cannot vote on own content",
		},
		{
			name: "error daily limit exceeded",
			setup: func(f *testFixture) uint64 {
				// Set max upvotes per day to 2 for easy testing
				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)
				params.MaxUpvotesPerDay = 2
				require.NoError(t, f.keeper.Params.Set(f.ctx, params))

				// Create multiple collections owned by f.owner, upvote them all
				coll1 := f.createCollection(t, f.owner)
				coll2 := f.createCollection(t, f.owner)
				coll3 := f.createCollection(t, f.owner)

				_, err = f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
					Creator:    f.member,
					TargetId:   coll1,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
				})
				require.NoError(t, err)

				_, err = f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
					Creator:    f.member,
					TargetId:   coll2,
					TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
				})
				require.NoError(t, err)

				return coll3
			},
			creator:        "", // f.member
			targetType:     types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			expErr:         true,
			expErrContains: "daily reaction limit",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)

			var targetID uint64
			if tc.setup != nil {
				targetID = tc.setup(f)
			}

			creator := tc.creator
			switch creator {
			case "":
				creator = f.member
			case "nonMember":
				creator = f.nonMember
			case "owner":
				creator = f.owner
			}

			resp, err := f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
				Creator:    creator,
				TargetId:   targetID,
				TargetType: tc.targetType,
			})

			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			// Verify upvote count incremented
			coll, err := f.keeper.Collection.Get(f.ctx, targetID)
			require.NoError(t, err)
			require.Equal(t, uint64(1), coll.UpvoteCount)
		})
	}
}
