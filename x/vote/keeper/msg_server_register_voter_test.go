package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/types"
)

func TestRegisterVoter(t *testing.T) {
	t.Run("happy: new registration succeeds", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(1),
		})
		require.NoError(t, err)

		// Verify stored registration.
		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.True(t, reg.Active)
		require.Equal(t, f.member, reg.Address)
		require.Equal(t, genZkPubKey(1), reg.ZkPublicKey)
	})

	t.Run("happy: reactivate inactive voter", func(t *testing.T) {
		f := initTestFixture(t)

		// Register, then deactivate.
		f.registerVoter(t, f.member, genZkPubKey(1))
		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		// Verify inactive.
		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.False(t, reg.Active)

		// Re-register with a new key.
		_, err = f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(99),
		})
		require.NoError(t, err)

		reg, err = f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.True(t, reg.Active)
		require.Equal(t, genZkPubKey(99), reg.ZkPublicKey)
	})

	t.Run("error: not a member", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.nonMember,
			ZkPublicKey: genZkPubKey(1),
		})
		require.ErrorIs(t, err, types.ErrNotAMember)
	})

	t.Run("error: registration closed", func(t *testing.T) {
		f := initTestFixture(t)

		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		params.OpenRegistration = false
		require.NoError(t, f.keeper.Params.Set(f.ctx, params))

		_, err = f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(1),
		})
		require.ErrorIs(t, err, types.ErrRegistrationClosed)
	})

	t.Run("error: already registered with same key", func(t *testing.T) {
		f := initTestFixture(t)

		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(1),
		})
		require.ErrorIs(t, err, types.ErrAlreadyRegistered)
	})

	t.Run("error: active registration with different key", func(t *testing.T) {
		f := initTestFixture(t)

		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(2),
		})
		require.ErrorIs(t, err, types.ErrUseRotateKey)
	})

	t.Run("error: duplicate pubkey on new registration", func(t *testing.T) {
		f := initTestFixture(t)

		// member registers with key 1.
		f.registerVoter(t, f.member, genZkPubKey(1))

		// member2 tries to register with same key.
		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member2,
			ZkPublicKey: genZkPubKey(1),
		})
		require.ErrorIs(t, err, types.ErrDuplicatePublicKey)
	})

	t.Run("error: duplicate pubkey on reactivation", func(t *testing.T) {
		f := initTestFixture(t)

		// member2 registers with key 2 (stays active).
		f.registerVoter(t, f.member2, genZkPubKey(2))

		// member registers, then deactivates.
		f.registerVoter(t, f.member, genZkPubKey(1))
		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		// member tries to reactivate with member2's key.
		_, err = f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(2),
		})
		require.ErrorIs(t, err, types.ErrDuplicatePublicKey)
	})

	t.Run("event: voter_registered emitted", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:       f.member,
			ZkPublicKey: genZkPubKey(1),
		})
		require.NoError(t, err)

		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		events := sdkCtx.EventManager().Events()
		found := false
		for _, e := range events {
			if e.Type == types.EventVoterRegistered {
				found = true
				for _, attr := range e.Attributes {
					if attr.Key == types.AttributeVoter {
						require.Equal(t, f.member, attr.Value)
					}
				}
			}
		}
		require.True(t, found, "expected voter_registered event")
	})
}
