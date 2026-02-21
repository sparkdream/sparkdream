package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/types"
)

func TestDeactivateVoter(t *testing.T) {
	t.Run("happy: deactivate active voter", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.False(t, reg.Active)
	})

	t.Run("error: not registered", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.ErrorIs(t, err, types.ErrNotRegistered)
	})

	t.Run("error: already inactive", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		// Deactivate once.
		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		// Deactivate again.
		_, err = f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.ErrorIs(t, err, types.ErrAlreadyInactive)
	})

	t.Run("event: voter_deactivated with reason voluntary", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		events := sdkCtx.EventManager().Events()
		found := false
		for _, e := range events {
			if e.Type == types.EventVoterDeactivated {
				found = true
				var voterVal, reasonVal string
				for _, attr := range e.Attributes {
					switch attr.Key {
					case types.AttributeVoter:
						voterVal = attr.Value
					case types.AttributeReason:
						reasonVal = attr.Value
					}
				}
				require.Equal(t, f.member, voterVal)
				require.Equal(t, "voluntary", reasonVal)
			}
		}
		require.True(t, found, "expected voter_deactivated event")
	})
}
