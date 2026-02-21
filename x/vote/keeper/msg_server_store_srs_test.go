package keeper_test

import (
	"crypto/sha256"
	"testing"

	"sparkdream/x/vote/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestStoreSRS_HappyPath_GovernanceAuthority(t *testing.T) {
	f := initTestFixture(t)

	srsData := []byte("trusted-setup-reference-string")

	_, err := f.msgServer.StoreSRS(f.ctx, &types.MsgStoreSRS{
		Authority: f.authority,
		Srs:       srsData,
	})
	require.NoError(t, err)

	// Verify SRS is stored.
	srsState, err := f.keeper.SrsState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, srsData, srsState.Srs)

	// Verify hash is SHA-256 of the SRS data.
	expectedHash := sha256.Sum256(srsData)
	require.Equal(t, expectedHash[:], srsState.Hash)
}

func TestStoreSRS_NilHashInParams(t *testing.T) {
	f := initTestFixture(t)

	// Default params have nil SrsHash, so any SRS should be accepted.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Nil(t, params.SrsHash)

	srsData := []byte("any-srs-data-is-fine")

	_, err = f.msgServer.StoreSRS(f.ctx, &types.MsgStoreSRS{
		Authority: f.authority,
		Srs:       srsData,
	})
	require.NoError(t, err)

	srsState, err := f.keeper.SrsState.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, srsData, srsState.Srs)
}

func TestStoreSRS_NotGovernance(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.StoreSRS(f.ctx, &types.MsgStoreSRS{
		Authority: f.member, // not governance
		Srs:       []byte("srs-data"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCancelNotAuthorized)
}

func TestStoreSRS_HashMismatch(t *testing.T) {
	f := initTestFixture(t)

	// Set a specific SrsHash in params.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	expectedHash := sha256.Sum256([]byte("expected-srs-data"))
	params.SrsHash = expectedHash[:]
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Submit different SRS data whose hash will not match.
	_, err = f.msgServer.StoreSRS(f.ctx, &types.MsgStoreSRS{
		Authority: f.authority,
		Srs:       []byte("wrong-srs-data"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSRSHashMismatch)
}

func TestStoreSRS_EmitsEvent(t *testing.T) {
	f := initTestFixture(t)

	_, err := f.msgServer.StoreSRS(f.ctx, &types.MsgStoreSRS{
		Authority: f.authority,
		Srs:       []byte("srs-data"),
	})
	require.NoError(t, err)

	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	events := sdkCtx.EventManager().Events()

	found := false
	for _, e := range events {
		if e.Type == types.EventSRSStored {
			found = true
			break
		}
	}
	require.True(t, found, "expected %s event to be emitted", types.EventSRSStored)
}
