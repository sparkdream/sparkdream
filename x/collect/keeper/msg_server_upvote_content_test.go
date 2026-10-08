package keeper_test

import (
	"testing"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	shieldtypes "sparkdream/x/shield/types"
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

// Anonymous votes all carry the shield module as voter; x/shield's nullifier
// limits each member to one vote per target, so collect's per-voter dedup and
// daily limit must not block a second member's anonymous vote.
func TestUpvoteContent_AnonymousVotesAreIndependent(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()
	collID := f.createCollection(t, f.owner)

	for i := 0; i < 3; i++ {
		_, err := f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
			Creator:    shield,
			TargetId:   collID,
			TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		})
		require.NoError(t, err, "anonymous upvote %d", i+1)
	}
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)
	require.Equal(t, uint64(3), coll.UpvoteCount)
}

// The shield address owns every anonymous collection, so an anonymous vote on
// an anonymous collection can't be told apart from the owner voting on their
// own: the own-content check is skipped for it. An identified owner voting on
// their own collection is still refused.
func TestUpvoteContent_AnonymousOwnContentRelaxation(t *testing.T) {
	f := initTestFixture(t)
	shield := authtypes.NewModuleAddress("shield").String()

	anonID := createAnonCollection(t, f, 3, f.sdkCtx.BlockHeight()+100)
	anonColl, err := f.keeper.Collection.Get(f.ctx, anonID)
	require.NoError(t, err)
	require.Equal(t, shield, anonColl.Owner)

	_, err = f.msgServer.UpvoteContent(shieldtypes.WithProvenTrustLevel(f.sdkCtx, 1), &types.MsgUpvoteContent{
		Creator:    shield,
		TargetId:   anonID,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
	})
	require.NoError(t, err)
	anonColl, err = f.keeper.Collection.Get(f.ctx, anonID)
	require.NoError(t, err)
	require.Equal(t, uint64(1), anonColl.UpvoteCount)

	ownID := f.createCollection(t, f.owner)
	_, err = f.msgServer.UpvoteContent(f.ctx, &types.MsgUpvoteContent{
		Creator:    f.owner,
		TargetId:   ownID,
		TargetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
	})
	require.ErrorIs(t, err, types.ErrCannotVoteOwnContent)
}
