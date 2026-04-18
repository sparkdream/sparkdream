package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/forum/types"
)

func TestIsShieldCompatible(t *testing.T) {
	f := initFixture(t)

	cases := []struct {
		name string
		msg  sdk.Msg
		want bool
	}{
		{"MsgCreatePost", &types.MsgCreatePost{}, true},
		{"MsgUpvotePost", &types.MsgUpvotePost{}, true},
		{"MsgDownvotePost", &types.MsgDownvotePost{}, true},
		{"MsgEditPost", &types.MsgEditPost{}, false},
		{"MsgDeletePost", &types.MsgDeletePost{}, false},
		{"MsgFlagPost", &types.MsgFlagPost{}, false},
		{"MsgHidePost", &types.MsgHidePost{}, false},
		{"MsgLockThread", &types.MsgLockThread{}, false},
		{"MsgFollowThread", &types.MsgFollowThread{}, false},
		{"MsgCreateBounty", &types.MsgCreateBounty{}, false},
		{"MsgUpdateParams", &types.MsgUpdateParams{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := f.keeper.IsShieldCompatible(f.ctx, tc.msg)
			if got != tc.want {
				t.Errorf("IsShieldCompatible(%T) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}
