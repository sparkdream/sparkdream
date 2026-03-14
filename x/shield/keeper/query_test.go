package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

func initQueryServer(t *testing.T) (*fixture, types.QueryServer) {
	t.Helper()
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	return f, qs
}

func TestQueryParams(t *testing.T) {
	f, qs := initQueryServer(t)

	resp, err := qs.Params(f.ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.Params.Enabled)
	require.Equal(t, types.DefaultMaxGasPerExec, resp.Params.MaxGasPerExec)
}

func TestQueryShieldedOp(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ShieldedOp(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("existing op", func(t *testing.T) {
		resp, err := qs.ShieldedOp(f.ctx, &types.QueryShieldedOpRequest{
			MessageTypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
		})
		require.NoError(t, err)
		require.True(t, resp.Registration.Active)
		require.Equal(t, types.ProofDomain_PROOF_DOMAIN_TRUST_TREE, resp.Registration.ProofDomain)
	})

	t.Run("nonexistent op", func(t *testing.T) {
		_, err := qs.ShieldedOp(f.ctx, &types.QueryShieldedOpRequest{
			MessageTypeUrl: "/sparkdream.unknown.v1.MsgFoo",
		})
		require.Error(t, err)
	})
}

func TestQueryShieldedOps(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ShieldedOps(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("list all ops", func(t *testing.T) {
		resp, err := qs.ShieldedOps(f.ctx, &types.QueryShieldedOpsRequest{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(resp.Registrations), 12) // Default genesis ops
	})
}

func TestQueryModuleBalance(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ModuleBalance(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("returns balance", func(t *testing.T) {
		resp, err := qs.ModuleBalance(f.ctx, &types.QueryModuleBalanceRequest{})
		require.NoError(t, err)
		// Mock bank keeper returns 1000000000uspark
		require.Equal(t, "uspark", resp.Balance.Denom)
		require.Equal(t, math.NewInt(1000000000), resp.Balance.Amount)
	})
}

func TestQueryNullifierUsed(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.NullifierUsed(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("unused nullifier", func(t *testing.T) {
		resp, err := qs.NullifierUsed(f.ctx, &types.QueryNullifierUsedRequest{
			Domain:       1,
			Scope:        100,
			NullifierHex: "abc123",
		})
		require.NoError(t, err)
		require.False(t, resp.Used)
		require.Equal(t, int64(0), resp.UsedAtHeight)
	})

	t.Run("used nullifier", func(t *testing.T) {
		require.NoError(t, f.keeper.RecordNullifier(f.ctx, 1, 100, "abc123", 42))

		resp, err := qs.NullifierUsed(f.ctx, &types.QueryNullifierUsedRequest{
			Domain:       1,
			Scope:        100,
			NullifierHex: "abc123",
		})
		require.NoError(t, err)
		require.True(t, resp.Used)
		require.Equal(t, int64(42), resp.UsedAtHeight)
	})
}

func TestQueryDayFunding(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.DayFunding(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("no funding", func(t *testing.T) {
		resp, err := qs.DayFunding(f.ctx, &types.QueryDayFundingRequest{Day: 1})
		require.NoError(t, err)
		require.True(t, resp.DayFunding.AmountFunded.IsZero())
	})

	t.Run("with funding", func(t *testing.T) {
		require.NoError(t, f.keeper.SetDayFunding(f.ctx, 5, math.NewInt(500000)))

		resp, err := qs.DayFunding(f.ctx, &types.QueryDayFundingRequest{Day: 5})
		require.NoError(t, err)
		require.Equal(t, uint64(5), resp.DayFunding.Day)
		require.Equal(t, math.NewInt(500000), resp.DayFunding.AmountFunded)
	})
}

func TestQueryShieldEpoch(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ShieldEpoch(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("default epoch", func(t *testing.T) {
		resp, err := qs.ShieldEpoch(f.ctx, &types.QueryShieldEpochRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.EpochState.CurrentEpoch)
	})

	t.Run("after epoch update", func(t *testing.T) {
		require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{
			CurrentEpoch:     10,
			EpochStartHeight: 500,
		}))

		resp, err := qs.ShieldEpoch(f.ctx, &types.QueryShieldEpochRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(10), resp.EpochState.CurrentEpoch)
		require.Equal(t, int64(500), resp.EpochState.EpochStartHeight)
	})
}

func TestQueryPendingOps(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.PendingOps(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty queue", func(t *testing.T) {
		resp, err := qs.PendingOps(f.ctx, &types.QueryPendingOpsRequest{})
		require.NoError(t, err)
		require.Len(t, resp.PendingOps, 0)
	})

	t.Run("with pending ops", func(t *testing.T) {
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
			Id: 1, TargetEpoch: 5, EncryptedPayload: []byte("data1"),
		}))
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
			Id: 2, TargetEpoch: 6, EncryptedPayload: []byte("data2"),
		}))

		resp, err := qs.PendingOps(f.ctx, &types.QueryPendingOpsRequest{})
		require.NoError(t, err)
		require.Len(t, resp.PendingOps, 2)
	})

	t.Run("filter by epoch", func(t *testing.T) {
		resp, err := qs.PendingOps(f.ctx, &types.QueryPendingOpsRequest{Epoch: 5})
		require.NoError(t, err)
		require.Len(t, resp.PendingOps, 1)
		require.Equal(t, uint64(5), resp.PendingOps[0].TargetEpoch)
	})
}

func TestQueryPendingOpCount(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.PendingOpCount(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("zero count", func(t *testing.T) {
		resp, err := qs.PendingOpCount(f.ctx, &types.QueryPendingOpCountRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.Count)
	})

	t.Run("after adding ops", func(t *testing.T) {
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1}))
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 2}))

		resp, err := qs.PendingOpCount(f.ctx, &types.QueryPendingOpCountRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(2), resp.Count)
	})
}

func TestQueryTLEMasterPublicKey(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TLEMasterPublicKey(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("no key set", func(t *testing.T) {
		resp, err := qs.TLEMasterPublicKey(f.ctx, &types.QueryTLEMasterPublicKeyRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.MasterPublicKey)
	})

	t.Run("with key set", func(t *testing.T) {
		require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, types.TLEKeySet{
			MasterPublicKey: []byte("master_pk_bytes"),
		}))

		resp, err := qs.TLEMasterPublicKey(f.ctx, &types.QueryTLEMasterPublicKeyRequest{})
		require.NoError(t, err)
		require.Equal(t, []byte("master_pk_bytes"), resp.MasterPublicKey)
	})
}

func TestQueryTLEKeySet(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TLEKeySet(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("no key set", func(t *testing.T) {
		resp, err := qs.TLEKeySet(f.ctx, &types.QueryTLEKeySetRequest{})
		require.NoError(t, err)
		require.Empty(t, resp.KeySet.MasterPublicKey)
	})

	t.Run("with key set", func(t *testing.T) {
		ks := types.TLEKeySet{
			MasterPublicKey:      []byte("master_pk"),
			ThresholdNumerator:   2,
			ThresholdDenominator: 3,
			ValidatorShares: []*types.TLEValidatorPublicShare{
				{ValidatorAddress: "val1", PublicShare: []byte("s1"), ShareIndex: 0},
			},
		}
		require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, ks))

		resp, err := qs.TLEKeySet(f.ctx, &types.QueryTLEKeySetRequest{})
		require.NoError(t, err)
		require.Equal(t, []byte("master_pk"), resp.KeySet.MasterPublicKey)
		require.Len(t, resp.KeySet.ValidatorShares, 1)
		require.Equal(t, uint64(2), resp.KeySet.ThresholdNumerator)
	})
}

func TestQueryVerificationKey(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.VerificationKey(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		_, err := qs.VerificationKey(f.ctx, &types.QueryVerificationKeyRequest{CircuitId: "nonexistent"})
		require.Error(t, err)
	})

	t.Run("found", func(t *testing.T) {
		require.NoError(t, f.keeper.SetVerificationKey(f.ctx, types.VerificationKey{
			CircuitId: "circuit_1",
			VkBytes:   []byte("vk_data"),
		}))

		resp, err := qs.VerificationKey(f.ctx, &types.QueryVerificationKeyRequest{CircuitId: "circuit_1"})
		require.NoError(t, err)
		require.Equal(t, "circuit_1", resp.VerificationKey.CircuitId)
		require.Equal(t, []byte("vk_data"), resp.VerificationKey.VkBytes)
	})
}

func TestQueryTLEMissCount(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TLEMissCount(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("zero count", func(t *testing.T) {
		resp, err := qs.TLEMissCount(f.ctx, &types.QueryTLEMissCountRequest{ValidatorAddress: "val1"})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.MissCount)
	})

	t.Run("with misses", func(t *testing.T) {
		require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val1", 5))

		resp, err := qs.TLEMissCount(f.ctx, &types.QueryTLEMissCountRequest{ValidatorAddress: "val1"})
		require.NoError(t, err)
		require.Equal(t, uint64(5), resp.MissCount)
	})
}

func TestQueryDecryptionShares(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.DecryptionShares(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("no shares", func(t *testing.T) {
		resp, err := qs.DecryptionShares(f.ctx, &types.QueryDecryptionSharesRequest{Epoch: 1})
		require.NoError(t, err)
		require.Len(t, resp.Shares, 0)
	})

	t.Run("with shares", func(t *testing.T) {
		require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
			Epoch: 1, Validator: "val1", Share: []byte("share1"),
		}))
		require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
			Epoch: 1, Validator: "val2", Share: []byte("share2"),
		}))

		resp, err := qs.DecryptionShares(f.ctx, &types.QueryDecryptionSharesRequest{Epoch: 1})
		require.NoError(t, err)
		require.Len(t, resp.Shares, 2)
	})
}

func TestQueryIdentityRateLimit(t *testing.T) {
	f, qs := initQueryServer(t)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.IdentityRateLimit(f.ctx, nil)
		require.Error(t, err)
	})

	t.Run("no usage", func(t *testing.T) {
		resp, err := qs.IdentityRateLimit(f.ctx, &types.QueryIdentityRateLimitRequest{
			RateLimitNullifierHex: "identity1",
		})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.UsedCount)
		require.Equal(t, types.DefaultMaxExecsPerIdentity, resp.MaxCount)
		require.Equal(t, types.DefaultMaxExecsPerIdentity, resp.Remaining)
	})

	t.Run("with usage", func(t *testing.T) {
		// Use rate limit a few times
		f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 100)
		f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 100)
		f.keeper.CheckAndIncrementRateLimit(f.ctx, "identity1", 100)

		resp, err := qs.IdentityRateLimit(f.ctx, &types.QueryIdentityRateLimitRequest{
			RateLimitNullifierHex: "identity1",
		})
		require.NoError(t, err)
		require.Equal(t, uint64(3), resp.UsedCount)
		require.Equal(t, types.DefaultMaxExecsPerIdentity, resp.MaxCount)
		require.Equal(t, types.DefaultMaxExecsPerIdentity-3, resp.Remaining)
	})
}
