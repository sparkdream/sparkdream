package keeper_test

import (
	"testing"

	"sparkdream/x/commons/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestIsShieldCompatible(t *testing.T) {
	k, ctx, _ := setupCommonsKeeper(t)

	tests := []struct {
		name string
		msg  sdk.Msg
		want bool
	}{
		{"submit anonymous proposal", &types.MsgSubmitAnonymousProposal{}, true},
		{"anonymous vote proposal", &types.MsgAnonymousVoteProposal{}, true},
		{"regular submit proposal", &types.MsgSubmitProposal{}, false},
		{"regular vote proposal", &types.MsgVoteProposal{}, false},
		{"update params", &types.MsgUpdateParams{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, k.IsShieldCompatible(ctx, tc.msg))
		})
	}
}
