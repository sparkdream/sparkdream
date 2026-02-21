package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"sparkdream/x/vote/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/pairing/bn256"
)

func validBN256Point(t *testing.T) []byte {
	t.Helper()
	suite := bn256.NewSuiteG1()
	point := suite.Point().Pick(suite.RandomStream())
	pointBytes, err := point.MarshalBinary()
	require.NoError(t, err)
	return pointBytes
}

func TestRegisterTLEShare_HappyPath(t *testing.T) {
	f := initTestFixture(t)

	pointBytes := validBN256Point(t)

	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: pointBytes,
		ShareIndex:     1,
	})
	require.NoError(t, err)

	// Verify stored.
	share, err := f.keeper.TleValidatorShare.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.Equal(t, f.validator, share.Validator)
	require.Equal(t, pointBytes, share.PublicKeyShare)
	require.Equal(t, uint64(1), share.ShareIndex)
}

func TestRegisterTLEShare_ReRegister(t *testing.T) {
	f := initTestFixture(t)

	point1 := validBN256Point(t)
	point2 := validBN256Point(t)

	// Register first time.
	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: point1,
		ShareIndex:     1,
	})
	require.NoError(t, err)

	// Re-register with different point and same index (update).
	_, err = f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: point2,
		ShareIndex:     1,
	})
	require.NoError(t, err)

	// Verify updated.
	share, err := f.keeper.TleValidatorShare.Get(f.ctx, f.validator)
	require.NoError(t, err)
	require.Equal(t, point2, share.PublicKeyShare)
}

func TestRegisterTLEShare_NotBonded(t *testing.T) {
	f := initTestFixture(t)

	// Override staking keeper to return unbonded validator.
	f.stakingKeeper.getValidatorFn = func(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
		return stakingtypes.Validator{Status: stakingtypes.Unbonded}, nil
	}

	pointBytes := validBN256Point(t)

	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: pointBytes,
		ShareIndex:     1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrNotValidator)
}

func TestRegisterTLEShare_ValidatorNotFound(t *testing.T) {
	f := initTestFixture(t)

	// Override staking keeper to return error.
	f.stakingKeeper.getValidatorFn = func(_ context.Context, _ sdk.ValAddress) (stakingtypes.Validator, error) {
		return stakingtypes.Validator{}, fmt.Errorf("not found")
	}

	pointBytes := validBN256Point(t)

	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: pointBytes,
		ShareIndex:     1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrNotValidator)
}

func TestRegisterTLEShare_ShareIndexZero(t *testing.T) {
	f := initTestFixture(t)

	pointBytes := validBN256Point(t)

	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: pointBytes,
		ShareIndex:     0,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidShareIndex)
}

func TestRegisterTLEShare_InvalidBN256Point(t *testing.T) {
	f := initTestFixture(t)

	// Random bytes that are not a valid BN256 point.
	invalidPoint := []byte("this-is-not-a-valid-bn256-point-at-all")

	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: invalidPoint,
		ShareIndex:     1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidPublicKeyShare)
}

func TestRegisterTLEShare_DuplicateShareIndex(t *testing.T) {
	f := initTestFixture(t)

	point1 := validBN256Point(t)
	point2 := validBN256Point(t)

	// Register first validator with share index 1.
	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: point1,
		ShareIndex:     1,
	})
	require.NoError(t, err)

	// Try to register member2 (also a member) with the same share index.
	_, err = f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.member2,
		PublicKeyShare: point2,
		ShareIndex:     1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrDuplicateShareIndex)
}

func TestRegisterTLEShare_EmitsEvent(t *testing.T) {
	f := initTestFixture(t)

	pointBytes := validBN256Point(t)

	_, err := f.msgServer.RegisterTLEShare(f.ctx, &types.MsgRegisterTLEShare{
		Validator:      f.validator,
		PublicKeyShare: pointBytes,
		ShareIndex:     1,
	})
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	events := sdkCtx.EventManager().Events()

	found := false
	for _, e := range events {
		if e.Type == types.EventTLEShareRegistered {
			found = true
			break
		}
	}
	require.True(t, found, "expected %s event to be emitted", types.EventTLEShareRegistered)
}
