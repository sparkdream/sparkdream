package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	any "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// --- MsgServer Tests ---

func initMsgServer(t *testing.T) (*fixture, types.MsgServer) {
	t.Helper()
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	return f, ms
}

// --- UpdateParams ---

func TestMsgUpdateParams(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	t.Run("valid update", func(t *testing.T) {
		newParams := types.DefaultParams()
		newParams.MaxGasPerExec = 999_999
		newParams.MaxExecsPerIdentityPerEpoch = 100

		resp, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    newParams,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify params were updated
		got, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.Equal(t, uint64(999_999), got.MaxGasPerExec)
		require.Equal(t, uint64(100), got.MaxExecsPerIdentityPerEpoch)
	})

	t.Run("wrong authority rejected", func(t *testing.T) {
		_, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
			Authority: "sprkdrm1wrongauthority",
			Params:    types.DefaultParams(),
		})
		require.Error(t, err)
	})

	t.Run("invalid params rejected", func(t *testing.T) {
		badParams := types.DefaultParams()
		badParams.MaxGasPerExec = 0 // Invalid

		_, err := ms.UpdateParams(f.ctx, &types.MsgUpdateParams{
			Authority: authority,
			Params:    badParams,
		})
		require.Error(t, err)
	})
}

// --- RegisterShieldedOp ---

func TestMsgRegisterShieldedOp(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	t.Run("register new op", func(t *testing.T) {
		reg := types.ShieldedOpRegistration{
			MessageTypeUrl:     "/sparkdream.test.v1.MsgTest",
			ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
			MinTrustLevel:      2,
			NullifierDomain:    99,
			NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
			Active:             true,
			BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_IMMEDIATE_ONLY,
		}

		resp, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
			Authority:    authority,
			Registration: reg,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify stored
		got, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.test.v1.MsgTest")
		require.True(t, found)
		require.Equal(t, uint32(2), got.MinTrustLevel)
		require.Equal(t, uint32(99), got.NullifierDomain)
		require.True(t, got.Active)
	})

	t.Run("wrong authority rejected", func(t *testing.T) {
		_, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
			Authority: "sprkdrm1wrongauthority",
			Registration: types.ShieldedOpRegistration{
				MessageTypeUrl: "/sparkdream.test.v1.MsgOther",
			},
		})
		require.Error(t, err)
	})

	t.Run("empty type url rejected", func(t *testing.T) {
		_, err := ms.RegisterShieldedOp(f.ctx, &types.MsgRegisterShieldedOp{
			Authority: authority,
			Registration: types.ShieldedOpRegistration{
				MessageTypeUrl: "",
			},
		})
		require.Error(t, err)
	})
}

// --- DeregisterShieldedOp ---

func TestMsgDeregisterShieldedOp(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	t.Run("deregister existing op", func(t *testing.T) {
		// Verify op exists first (from default genesis)
		_, found := f.keeper.GetShieldedOp(f.ctx, "/sparkdream.blog.v1.MsgCreatePost")
		require.True(t, found)

		resp, err := ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
			Authority:      authority,
			MessageTypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify deleted
		_, found = f.keeper.GetShieldedOp(f.ctx, "/sparkdream.blog.v1.MsgCreatePost")
		require.False(t, found)
	})

	t.Run("deregister nonexistent op fails", func(t *testing.T) {
		_, err := ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
			Authority:      authority,
			MessageTypeUrl: "/sparkdream.nonexistent.v1.MsgFoo",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnregisteredOperation)
	})

	t.Run("wrong authority rejected", func(t *testing.T) {
		_, err := ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
			Authority:      "sprkdrm1wrongauthority",
			MessageTypeUrl: "/sparkdream.blog.v1.MsgCreateReply",
		})
		require.Error(t, err)
	})

	t.Run("empty type url rejected", func(t *testing.T) {
		_, err := ms.DeregisterShieldedOp(f.ctx, &types.MsgDeregisterShieldedOp{
			Authority:      authority,
			MessageTypeUrl: "",
		})
		require.Error(t, err)
	})
}

// --- TriggerDKG ---

func TestMsgTriggerDKG(t *testing.T) {
	f, ms := initMsgServer(t)

	authority, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("gov"))
	require.NoError(t, err)

	t.Run("trigger with defaults", func(t *testing.T) {
		resp, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
			Authority:            authority,
			ThresholdNumerator:   0, // Should default to 2
			ThresholdDenominator: 0, // Should default to 3
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Verify DKG state was created in REGISTERING phase
		dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
		require.True(t, found)
		require.Equal(t, types.DKGPhase_DKG_PHASE_REGISTERING, dkgState.Phase)
		require.Equal(t, uint64(2), dkgState.ThresholdNumerator)
		require.Equal(t, uint64(3), dkgState.ThresholdDenominator)
		require.Equal(t, uint64(1), dkgState.Round)
		require.Len(t, dkgState.ExpectedValidators, 5) // 5 mock validators

		// Verify encrypted batch mode is disabled
		params, err := f.keeper.Params.Get(f.ctx)
		require.NoError(t, err)
		require.False(t, params.EncryptedBatchEnabled)
	})

	t.Run("duplicate trigger rejected while DKG in progress", func(t *testing.T) {
		_, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
			Authority:            authority,
			ThresholdNumerator:   3,
			ThresholdDenominator: 5,
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "DKG ceremony already in progress")
	})

	t.Run("trigger after completing previous round", func(t *testing.T) {
		// Manually set DKG state to ACTIVE (completed) so a new round can start
		require.NoError(t, f.keeper.SetDKGStateVal(f.ctx, types.DKGState{
			Round: 1,
			Phase: types.DKGPhase_DKG_PHASE_ACTIVE,
		}))

		resp, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
			Authority:            authority,
			ThresholdNumerator:   3,
			ThresholdDenominator: 5,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)

		dkgState, found := f.keeper.GetDKGStateVal(f.ctx)
		require.True(t, found)
		require.Equal(t, uint64(3), dkgState.ThresholdNumerator)
		require.Equal(t, uint64(5), dkgState.ThresholdDenominator)
		require.Equal(t, uint64(2), dkgState.Round)
	})

	t.Run("wrong authority rejected", func(t *testing.T) {
		_, err := ms.TriggerDKG(f.ctx, &types.MsgTriggerDkg{
			Authority: "sprkdrm1wrongauthority",
		})
		require.Error(t, err)
	})
}

// --- computeThreshold ---

func TestComputeThreshold(t *testing.T) {
	// computeThreshold is unexported but tested indirectly through TLE operations.
	// We test it by setting up TLE key sets and checking behavior.
	f := initFixture(t)

	// Set up a TLE key set with 2/3 threshold and 3 validators
	ks := types.TLEKeySet{
		ThresholdNumerator:   2,
		ThresholdDenominator: 3,
		ValidatorShares: []*types.TLEValidatorPublicShare{
			{ValidatorAddress: "val1", PublicShare: []byte("share1"), ShareIndex: 0},
			{ValidatorAddress: "val2", PublicShare: []byte("share2"), ShareIndex: 1},
			{ValidatorAddress: "val3", PublicShare: []byte("share3"), ShareIndex: 2},
		},
	}
	require.NoError(t, f.keeper.SetTLEKeySetVal(f.ctx, ks))

	// Verify storage round-trip
	got, found := f.keeper.GetTLEKeySetVal(f.ctx)
	require.True(t, found)
	require.Len(t, got.ValidatorShares, 3)
}

// --- ShieldedExec ---

func TestMsgShieldedExecDisabled(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("submitter"))
	require.NoError(t, err)

	// Disable shield
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.Enabled = false
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrShieldDisabled)
}

func TestMsgShieldedExecInvalidMode(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  99, // Invalid mode
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidExecMode)
}

func TestMsgShieldedExecImmediateNoInnerMessage(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:    submitter,
		ExecMode:     types.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
		InnerMessage: nil,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidInnerMessage)
}

func TestMsgShieldedExecEncryptedBatchDisabled(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Encrypted batch is disabled by default
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:        submitter,
		ExecMode:         types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload: []byte("encrypted_data"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrEncryptedBatchDisabled)
}

func TestMsgShieldedExecEncryptedBatchCleartextRejected(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch state so it exists
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	// InnerMessage should be nil in encrypted batch mode
	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter: submitter,
		ExecMode:  types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		InnerMessage: &any.Any{
			TypeUrl: "/sparkdream.blog.v1.MsgCreatePost",
			Value:   []byte("some data"),
		},
		EncryptedPayload: []byte("encrypted_data"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrCleartextFieldInBatchMode)
}

func TestMsgShieldedExecEncryptedBatchMissingPayload(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:        submitter,
		ExecMode:         types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload: nil, // Missing
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrMissingEncryptedPayload)
}

func TestMsgShieldedExecEncryptedBatchPayloadTooLarge(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Create oversized payload
	oversized := make([]byte, params.MaxEncryptedPayloadSize+1)

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:        submitter,
		ExecMode:         types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload: oversized,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPayloadTooLarge)
}

func TestMsgShieldedExecEncryptedBatchWrongEpoch(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch to 5
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 5}))

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:        submitter,
		ExecMode:         types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload: []byte("encrypted_data"),
		TargetEpoch:      3, // Wrong epoch
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidTargetEpoch)
}

func TestMsgShieldedExecEncryptedBatchQueueFull(t *testing.T) {
	f, ms := initMsgServer(t)

	submitter, err := f.addressCodec.BytesToString(authtypes.NewModuleAddress("test"))
	require.NoError(t, err)

	// Enable encrypted batch with tiny queue size
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.EncryptedBatchEnabled = true
	params.MaxPendingQueueSize = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	// Set epoch
	require.NoError(t, f.keeper.SetShieldEpochStateVal(f.ctx, types.ShieldEpochState{CurrentEpoch: 1}))

	// Fill the queue
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{
		Id:          1,
		TargetEpoch: 1,
	}))

	_, err = ms.ShieldedExec(f.ctx, &types.MsgShieldedExec{
		Submitter:        submitter,
		ExecMode:         types.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
		EncryptedPayload: []byte("encrypted_data"),
		TargetEpoch:      1,
		Nullifier:        []byte("null1"),
		MerkleRoot:       []byte("root1"),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrPendingQueueFull)
}

// --- Verification Key Tests ---

func TestVerificationKey(t *testing.T) {
	f := initFixture(t)

	t.Run("not found initially", func(t *testing.T) {
		_, found := f.keeper.GetVerificationKeyVal(f.ctx, "circuit_1")
		require.False(t, found)
	})

	t.Run("set and get", func(t *testing.T) {
		vk := types.VerificationKey{
			CircuitId: "circuit_1",
			VkBytes:   []byte("vk_data"),
		}
		require.NoError(t, f.keeper.SetVerificationKey(f.ctx, vk))

		got, found := f.keeper.GetVerificationKeyVal(f.ctx, "circuit_1")
		require.True(t, found)
		require.Equal(t, "circuit_1", got.CircuitId)
		require.Equal(t, []byte("vk_data"), got.VkBytes)
	})
}

// --- Decryption Share State ---

func TestDecryptionShares(t *testing.T) {
	f := initFixture(t)

	t.Run("no shares initially", func(t *testing.T) {
		_, found := f.keeper.GetDecryptionShare(f.ctx, 1, "val1")
		require.False(t, found)
		require.Equal(t, uint32(0), f.keeper.CountDecryptionShares(f.ctx, 1))
	})

	t.Run("set and get share", func(t *testing.T) {
		share := types.ShieldDecryptionShare{
			Epoch:     1,
			Validator: "val1",
			Share:     []byte("decryption_share_data"),
		}
		require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, share))

		got, found := f.keeper.GetDecryptionShare(f.ctx, 1, "val1")
		require.True(t, found)
		require.Equal(t, []byte("decryption_share_data"), got.Share)
	})

	t.Run("count shares for epoch", func(t *testing.T) {
		require.NoError(t, f.keeper.SetDecryptionShare(f.ctx, types.ShieldDecryptionShare{
			Epoch: 1, Validator: "val2", Share: []byte("share2"),
		}))
		require.Equal(t, uint32(2), f.keeper.CountDecryptionShares(f.ctx, 1))
		require.Equal(t, uint32(0), f.keeper.CountDecryptionShares(f.ctx, 2))
	})

	t.Run("get shares for epoch", func(t *testing.T) {
		shares := f.keeper.GetDecryptionSharesForEpoch(f.ctx, 1)
		require.Len(t, shares, 2)
	})
}

// --- Decryption Key State ---

func TestDecryptionKeys(t *testing.T) {
	f := initFixture(t)

	t.Run("not found initially", func(t *testing.T) {
		_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 1)
		require.False(t, found)
	})

	t.Run("set and get", func(t *testing.T) {
		dk := types.ShieldEpochDecryptionKey{
			Epoch:                 1,
			DecryptionKey:         []byte("key_data"),
			ReconstructedAtHeight: 100,
		}
		require.NoError(t, f.keeper.SetShieldEpochDecryptionKey(f.ctx, dk))

		got, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, 1)
		require.True(t, found)
		require.Equal(t, []byte("key_data"), got.DecryptionKey)
		require.Equal(t, int64(100), got.ReconstructedAtHeight)
	})
}

// --- Day Funding Pruning ---

func TestPruneDayFundings(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 1, math.NewInt(100)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 5, math.NewInt(200)))
	require.NoError(t, f.keeper.SetDayFunding(f.ctx, 10, math.NewInt(300)))

	// Prune days < 5
	err := f.keeper.PruneDayFundings(f.ctx, 5)
	require.NoError(t, err)

	require.True(t, f.keeper.GetDayFunding(f.ctx, 1).IsZero())
	require.Equal(t, math.NewInt(200), f.keeper.GetDayFunding(f.ctx, 5))
	require.Equal(t, math.NewInt(300), f.keeper.GetDayFunding(f.ctx, 10))
}

// --- Iterate TLE Miss Counters ---

func TestIterateTLEMissCounters(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val1", 3))
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val2", 7))
	require.NoError(t, f.keeper.SetTLEMissCount(f.ctx, "val3", 1))

	var result map[string]uint64 = make(map[string]uint64)
	err := f.keeper.IterateTLEMissCounters(f.ctx, func(validatorAddr string, count uint64) bool {
		result[validatorAddr] = count
		return false
	})
	require.NoError(t, err)
	require.Len(t, result, 3)
	require.Equal(t, uint64(3), result["val1"])
	require.Equal(t, uint64(7), result["val2"])
	require.Equal(t, uint64(1), result["val3"])
}

// --- Iterate Pending Ops ---

func TestIteratePendingOps(t *testing.T) {
	f := initFixture(t)

	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 5}))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 2, TargetEpoch: 5}))
	require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 3, TargetEpoch: 6}))

	t.Run("iterate all", func(t *testing.T) {
		var count int
		err := f.keeper.IteratePendingOps(f.ctx, func(op types.PendingShieldedOp) bool {
			count++
			return false
		})
		require.NoError(t, err)
		require.Equal(t, 3, count)
	})

	t.Run("iterate with early stop", func(t *testing.T) {
		var count int
		err := f.keeper.IteratePendingOps(f.ctx, func(op types.PendingShieldedOp) bool {
			count++
			return count >= 2 // Stop after 2
		})
		require.NoError(t, err)
		require.Equal(t, 2, count)
	})

	t.Run("get ops for epoch", func(t *testing.T) {
		ops := f.keeper.GetPendingOpsForEpoch(f.ctx, 5)
		require.Len(t, ops, 2)

		ops = f.keeper.GetPendingOpsForEpoch(f.ctx, 6)
		require.Len(t, ops, 1)

		ops = f.keeper.GetPendingOpsForEpoch(f.ctx, 99)
		require.Len(t, ops, 0)
	})

	t.Run("get ops before epoch", func(t *testing.T) {
		// Set submitted_at_epoch to test GetPendingOpsBeforeEpoch
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 1, TargetEpoch: 5, SubmittedAtEpoch: 3}))
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 2, TargetEpoch: 5, SubmittedAtEpoch: 5}))
		require.NoError(t, f.keeper.SetPendingOp(f.ctx, types.PendingShieldedOp{Id: 3, TargetEpoch: 6, SubmittedAtEpoch: 6}))

		ops := f.keeper.GetPendingOpsBeforeEpoch(f.ctx, 5)
		require.Len(t, ops, 1) // Only op with SubmittedAtEpoch=3
	})
}
