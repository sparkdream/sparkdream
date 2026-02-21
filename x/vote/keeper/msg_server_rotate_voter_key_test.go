package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/vote/types"
)

func TestRotateVoterKey(t *testing.T) {
	t.Run("happy: rotate both keys", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		newZk := genZkPubKey(10)
		newEnc := genZkPubKey(20) // arbitrary bytes for encryption key

		_, err := f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
			Voter:                  f.member,
			NewZkPublicKey:         newZk,
			NewEncryptionPublicKey: newEnc,
		})
		require.NoError(t, err)

		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.Equal(t, newZk, reg.ZkPublicKey)
		require.Equal(t, newEnc, reg.EncryptionPublicKey)
	})

	t.Run("happy: rotate zk key only (empty encryption key)", func(t *testing.T) {
		f := initTestFixture(t)

		origEnc := genZkPubKey(20)
		_, err := f.msgServer.RegisterVoter(f.ctx, &types.MsgRegisterVoter{
			Voter:               f.member,
			ZkPublicKey:         genZkPubKey(1),
			EncryptionPublicKey: origEnc,
		})
		require.NoError(t, err)

		newZk := genZkPubKey(10)
		_, err = f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
			Voter:                  f.member,
			NewZkPublicKey:         newZk,
			NewEncryptionPublicKey: nil, // empty: should keep original
		})
		require.NoError(t, err)

		reg, err := f.keeper.VoterRegistration.Get(f.ctx, f.member)
		require.NoError(t, err)
		require.Equal(t, newZk, reg.ZkPublicKey)
		require.Equal(t, origEnc, reg.EncryptionPublicKey)
	})

	t.Run("error: not registered", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
			Voter:          f.member,
			NewZkPublicKey: genZkPubKey(10),
		})
		require.ErrorIs(t, err, types.ErrNotRegistered)
	})

	t.Run("error: already inactive", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.DeactivateVoter(f.ctx, &types.MsgDeactivateVoter{
			Voter: f.member,
		})
		require.NoError(t, err)

		_, err = f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
			Voter:          f.member,
			NewZkPublicKey: genZkPubKey(10),
		})
		require.ErrorIs(t, err, types.ErrAlreadyInactive)
	})

	t.Run("error: duplicate new key", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))
		f.registerVoter(t, f.member2, genZkPubKey(2))

		// member tries to rotate to member2's key.
		_, err := f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
			Voter:          f.member,
			NewZkPublicKey: genZkPubKey(2),
		})
		require.ErrorIs(t, err, types.ErrDuplicatePublicKey)
	})

	t.Run("event: voter_key_rotated", func(t *testing.T) {
		f := initTestFixture(t)
		f.registerVoter(t, f.member, genZkPubKey(1))

		_, err := f.msgServer.RotateVoterKey(f.ctx, &types.MsgRotateVoterKey{
			Voter:          f.member,
			NewZkPublicKey: genZkPubKey(10),
		})
		require.NoError(t, err)

		sdkCtx := sdk.UnwrapSDKContext(f.ctx)
		events := sdkCtx.EventManager().Events()
		found := false
		for _, e := range events {
			if e.Type == types.EventVoterKeyRotated {
				found = true
				for _, attr := range e.Attributes {
					if attr.Key == types.AttributeVoter {
						require.Equal(t, f.member, attr.Value)
					}
				}
			}
		}
		require.True(t, found, "expected voter_key_rotated event")
	})
}
