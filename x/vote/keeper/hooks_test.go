package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/types"
)

func TestOnMemberRevoked(t *testing.T) {
	t.Run("deactivates active voter", func(t *testing.T) {
		f := initTestFixture(t)

		// Register an active voter.
		f.registerVoter(t, f.member, genZkPubKey(1))

		// Confirm registration is active.
		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.True(t, reg.Active)

		// Revoke the member.
		f.keeper.OnMemberRevoked(f.ctx, f.memberAddr, "trust level zeroed")

		// Confirm registration is now inactive.
		reg, err = f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.False(t, reg.Active)
	})

	t.Run("no-op for already inactive voter", func(t *testing.T) {
		f := initTestFixture(t)

		// Register and then deactivate.
		f.registerVoter(t, f.member, genZkPubKey(2))
		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		// Confirm inactive.
		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.False(t, reg.Active)

		// Call OnMemberRevoked again — should not error or panic.
		f.keeper.OnMemberRevoked(f.ctx, f.memberAddr, "double revoke")

		// Still inactive.
		reg, err = f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.False(t, reg.Active)
	})

	t.Run("no-op for unregistered voter", func(t *testing.T) {
		f := initTestFixture(t)

		// nonMember has never registered — should not panic.
		f.keeper.OnMemberRevoked(f.ctx, f.nonMemberAddr, "not a voter")

		// Verify nothing was stored.
		_, err := f.keeper.VoterRegistration.Get(f.ctx, f.nonMember)
		require.Error(t, err, "registration should not exist for unregistered address")
	})

	t.Run("emits voter_deactivated event with reason", func(t *testing.T) {
		f := initTestFixture(t)

		// Register a voter.
		f.registerVoter(t, f.member, genZkPubKey(3))

		// Reset event manager to isolate events from registration.
		f.sdkCtx = f.sdkCtx.WithEventManager(sdk.NewEventManager())
		f.ctx = f.sdkCtx

		// Revoke with a specific reason.
		reason := "challenge upheld"
		f.keeper.OnMemberRevoked(f.ctx, f.memberAddr, reason)

		// Find the voter_deactivated event.
		events := f.sdkCtx.EventManager().Events()
		found := false
		for _, ev := range events {
			if ev.Type == types.EventVoterDeactivated {
				found = true
				// Check attributes.
				var gotVoter, gotReason string
				for _, attr := range ev.Attributes {
					switch attr.Key {
					case types.AttributeVoter:
						gotVoter = attr.Value
					case types.AttributeReason:
						gotReason = attr.Value
					}
				}
				require.Equal(t, f.member, gotVoter)
				require.Equal(t, reason, gotReason)
			}
		}
		require.True(t, found, "expected voter_deactivated event to be emitted")
	})
}
