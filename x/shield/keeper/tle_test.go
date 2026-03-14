package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

func TestTLEKeySetOverwrite(t *testing.T) {
	f := initFixture(t)

	ks1 := types.TLEKeySet{
		MasterPublicKey: []byte("mpk1"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("s1"), ShareIndex: 0},
		},
	}
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, ks1))

	ks2 := types.TLEKeySet{
		MasterPublicKey: []byte("mpk2"),
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("s1_new"), ShareIndex: 0},
			{ValidatorAddress: "val2", PublicShare: []byte("s2_new"), ShareIndex: 1},
		},
	}
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, ks2))

	got, found := f.keeper.GetTLEKeySetVal(f.ctx)
	require.True(t, found)
	require.Equal(t, []byte("mpk2"), got.MasterPublicKey)
	require.Len(t, got.ValidatorShares, 2)
}

func TestDecryptionShareIsolation(t *testing.T) {
	f := initFixture(t)

	// Same validator, different epochs
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "val1", Share: []byte("epoch1_share"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 2, Validator: "val1", Share: []byte("epoch2_share"),
	}))

	s1, found := f.keeper.GetDecryptionShare(f.ctx, 1, "val1")
	require.True(t, found)
	require.Equal(t, []byte("epoch1_share"), s1.Share)

	s2, found := f.keeper.GetDecryptionShare(f.ctx, 2, "val1")
	require.True(t, found)
	require.Equal(t, []byte("epoch2_share"), s2.Share)

	// Different validators, same epoch
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 3, Validator: "val_a", Share: []byte("a_share"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 3, Validator: "val_b", Share: []byte("b_share"),
	}))

	require.Equal(t, uint32(2), f.keeper.CountDecryptionShares(f.ctx, 3))
}

func TestCountDecryptionSharesMultipleEpochs(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "v1", Share: []byte("s1"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 1, Validator: "v2", Share: []byte("s2"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 2, Validator: "v1", Share: []byte("s3"),
	}))

	require.Equal(t, uint32(2), f.keeper.CountDecryptionShares(f.ctx, 1))
	require.Equal(t, uint32(1), f.keeper.CountDecryptionShares(f.ctx, 2))
	require.Equal(t, uint32(0), f.keeper.CountDecryptionShares(f.ctx, 99))
}

func TestGetDecryptionSharesForEpochFiltering(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 5, Validator: "v1", Share: []byte("s1"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 5, Validator: "v2", Share: []byte("s2"),
	}))
	require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
		Epoch: 6, Validator: "v1", Share: []byte("s3"),
	}))

	shares5 := f.keeper.GetDecryptionSharesForEpoch(f.ctx, 5)
	require.Len(t, shares5, 2)

	shares6 := f.keeper.GetDecryptionSharesForEpoch(f.ctx, 6)
	require.Len(t, shares6, 1)

	sharesEmpty := f.keeper.GetDecryptionSharesForEpoch(f.ctx, 99)
	require.Len(t, sharesEmpty, 0)
}

func TestTLEMissCountMultipleValidators(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val_a", 10))
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val_b", 20))

	require.Equal(t, uint64(10), f.keeper.GetTLEMissCount(f.ctx, "val_a"))
	require.Equal(t, uint64(20), f.keeper.GetTLEMissCount(f.ctx, "val_b"))
	require.Equal(t, uint64(0), f.keeper.GetTLEMissCount(f.ctx, "val_c"))
}

func TestIterateTLEMissCountersEarlyStop(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "v1", 1))
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "v2", 2))
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "v3", 3))

	var count int
	err := f.keeper.IterateTLEMissCounters(f.ctx, func(_ string, _ uint64) bool {
		count++
		return count >= 2
	})
	require.NoError(t, err)
	require.Equal(t, 2, count)
}
