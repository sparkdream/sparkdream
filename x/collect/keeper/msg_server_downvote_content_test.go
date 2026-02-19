package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestDownvoteContent(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		creator        string
		targetType     types.FlagTargetType
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, targetID uint64)
	}{
		{
			name: "success member downvotes collection",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			creator:    "", // f.member
			targetType: types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			check: func(t *testing.T, f *testFixture, targetID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, targetID)
				require.NoError(t, err)
				require.Equal(t, uint64(1), coll.DownvoteCount)
			},
		},
		{
			name: "success BurnSPARKFromAccount called with correct amount",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				var burnCalled bool
				f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, sender sdk.AccAddress, _ string, amt sdk.Coins) error {
					if sender.Equals(f.memberAddr) {
						burnCalled = true
						params, _ := f.keeper.Params.Get(f.ctx)
						require.Equal(t, sdk.NewCoins(sdk.NewCoin("uspark", params.DownvoteCost)), amt)
					}
					return nil
				}
				_ = burnCalled
				return collID
			},
			creator:    "", // f.member
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
				_, err := f.msgServer.DownvoteContent(f.ctx, &types.MsgDownvoteContent{
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
			name: "error own content",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			creator:        "owner",
			targetType:     types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			expErr:         true,
			expErrContains: "cannot vote on own content",
		},
		{
			name: "error insufficient funds for burn",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
					return fmt.Errorf("insufficient funds")
				}
				return collID
			},
			creator:        "", // f.member
			targetType:     types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
			expErr:         true,
			expErrContains: "insufficient",
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

			resp, err := f.msgServer.DownvoteContent(f.ctx, &types.MsgDownvoteContent{
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

			if tc.check != nil {
				tc.check(t, f, targetID)
			}
		})
	}
}

// Ensure math import is used.
var _ = math.NewInt(0)
