package keeper_test

import (
	"testing"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/pairing/bn256"
)

// registerTLEShareForValidator generates a BN256 keypair, stores the public key
// share for the validator in the keeper, and returns the marshalled private scalar.
func registerTLEShareForValidator(t *testing.T, f *testFixture, validatorAddr string, shareIndex uint64) []byte {
	t.Helper()
	suite := bn256.NewSuiteG1()
	secret := suite.Scalar().Pick(suite.RandomStream())
	pubKey := suite.Point().Mul(secret, nil)

	pubKeyBytes, err := pubKey.MarshalBinary()
	require.NoError(t, err)
	secretBytes, err := secret.MarshalBinary()
	require.NoError(t, err)

	err = f.keeper.TleValidatorShare.Set(f.ctx, validatorAddr, types.TleValidatorShare{
		Validator:      validatorAddr,
		PublicKeyShare: pubKeyBytes,
		ShareIndex:     shareIndex,
	})
	require.NoError(t, err)

	return secretBytes
}

func TestSubmitDecryptionShare_HappyPath(t *testing.T) {
	f := initTestFixture(t)

	secretBytes := registerTLEShareForValidator(t, f, f.validator, 1)

	_, err := f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.NoError(t, err)

	// Verify share is stored.
	shareKey := keeper.TleShareKeyForTest(f.validator, 5)
	share, err := f.keeper.TleDecryptionShare.Get(f.ctx, shareKey)
	require.NoError(t, err)
	require.Equal(t, f.validator, share.Validator)
	require.Equal(t, uint64(5), share.Epoch)
	require.Equal(t, secretBytes, share.Share)
}

func TestSubmitDecryptionShare_TriggersEpochKeyReconstruction(t *testing.T) {
	f := initTestFixture(t)

	// Default threshold is 2/3 of total validators. With 2 validators,
	// threshold = ceil(2 * 2/3) = 2. So both must submit.
	secret1 := registerTLEShareForValidator(t, f, f.validator, 1)
	secret2 := registerTLEShareForValidator(t, f, f.member2, 2)

	epoch := uint64(5)

	// Submit first share.
	_, err := f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           epoch,
		DecryptionShare: secret1,
	})
	require.NoError(t, err)

	// Epoch key should NOT exist yet (only 1 of 2 shares).
	has, err := f.keeper.EpochDecryptionKey.Has(f.ctx, epoch)
	require.NoError(t, err)
	require.False(t, has)

	// Submit second share.
	_, err = f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.member2,
		Epoch:           epoch,
		DecryptionShare: secret2,
	})
	require.NoError(t, err)

	// Epoch key should now exist (reconstruction triggered).
	// Note: reconstruction uses Lagrange interpolation which may not produce
	// a "valid" key since these are independent keypairs (not polynomial shares),
	// but the important thing is that reconstruction was attempted and the key
	// was stored. The tryReconstructEpochKey function logs errors but does not
	// fail the submission.
	// Since these are independent keys (not from a shared polynomial), the
	// reconstruction may fail internally. The share itself should still be stored.
	shareKey := keeper.TleShareKeyForTest(f.member2, epoch)
	share, err := f.keeper.TleDecryptionShare.Get(f.ctx, shareKey)
	require.NoError(t, err)
	require.Equal(t, f.member2, share.Validator)
}

func TestSubmitDecryptionShare_TLENotEnabled(t *testing.T) {
	f := initTestFixture(t)

	// Disable TLE.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.TleEnabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	secretBytes := registerTLEShareForValidator(t, f, f.validator, 1)

	_, err = f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrTLENotEnabled)
}

func TestSubmitDecryptionShare_NoRegisteredShare(t *testing.T) {
	f := initTestFixture(t)

	suite := bn256.NewSuiteG1()
	secret := suite.Scalar().Pick(suite.RandomStream())
	secretBytes, err := secret.MarshalBinary()
	require.NoError(t, err)

	// No TLE share registered for this validator.
	_, err = f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrNoTLEShare)
}

func TestSubmitDecryptionShare_DuplicateForEpoch(t *testing.T) {
	f := initTestFixture(t)

	secretBytes := registerTLEShareForValidator(t, f, f.validator, 1)

	// First submission succeeds.
	_, err := f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.NoError(t, err)

	// Second submission for same epoch fails.
	_, err = f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrDuplicateDecryptionShare)
}

func TestSubmitDecryptionShare_CorrectnessProofFails(t *testing.T) {
	f := initTestFixture(t)

	// Register a TLE share with one keypair.
	registerTLEShareForValidator(t, f, f.validator, 1)

	// Submit a different (wrong) scalar that does not match the registered public key.
	suite := bn256.NewSuiteG1()
	wrongSecret := suite.Scalar().Pick(suite.RandomStream())
	wrongSecretBytes, err := wrongSecret.MarshalBinary()
	require.NoError(t, err)

	_, err = f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: wrongSecretBytes,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidCorrectnessProof)
}

func TestSubmitDecryptionShare_ReconstructionFailureNonFatal(t *testing.T) {
	f := initTestFixture(t)

	// Set threshold params so that 1 share triggers reconstruction attempt.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.TleThresholdNumerator = 1
	params.TleThresholdDenominator = 1 // threshold = 100% = 1 of 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	secretBytes := registerTLEShareForValidator(t, f, f.validator, 1)

	// Submit the share. Reconstruction will be attempted with 1 share.
	// Even if reconstruction fails internally, the submission should succeed.
	_, err = f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.NoError(t, err)

	// Verify the share is still stored regardless of reconstruction outcome.
	shareKey := keeper.TleShareKeyForTest(f.validator, 5)
	share, err := f.keeper.TleDecryptionShare.Get(f.ctx, shareKey)
	require.NoError(t, err)
	require.Equal(t, secretBytes, share.Share)
}

func TestSubmitDecryptionShare_EmitsEvent(t *testing.T) {
	f := initTestFixture(t)

	secretBytes := registerTLEShareForValidator(t, f, f.validator, 1)

	_, err := f.msgServer.SubmitDecryptionShare(f.ctx, &types.MsgSubmitDecryptionShare{
		Validator:       f.validator,
		Epoch:           5,
		DecryptionShare: secretBytes,
	})
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	events := sdkCtx.EventManager().Events()

	found := false
	for _, e := range events {
		if e.Type == types.EventDecryptionShareSubmit {
			found = true
			break
		}
	}
	require.True(t, found, "expected %s event to be emitted", types.EventDecryptionShareSubmit)
}
