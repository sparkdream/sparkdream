package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// unrelatedMsg is a no-op sdk.Msg used to assert the default case rejects
// message types that are not designed for anonymous execution.
type unrelatedMsg struct{}

func (unrelatedMsg) Reset()         {}
func (unrelatedMsg) String() string { return "unrelatedMsg" }
func (unrelatedMsg) ProtoMessage()  {}

func TestIsShieldCompatible(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	tests := []struct {
		name string
		msg  sdk.Msg
		want bool
	}{
		{"CreateChallenge is shield-compatible", &types.MsgCreateChallenge{}, true},
		{"AcceptInvitation is not shield-compatible", &types.MsgAcceptInvitation{}, false},
		{"unrelated msg type is rejected", unrelatedMsg{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, k.IsShieldCompatible(f.ctx, tc.msg))
		})
	}
}
