package types_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// --- Params Validation ---

func TestParamsValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*types.Params)
		wantErr bool
	}{
		{
			name:    "default params are valid",
			modify:  func(p *types.Params) {},
			wantErr: false,
		},
		{
			name:    "negative max_funding_per_day",
			modify:  func(p *types.Params) { p.MaxFundingPerDay = math.NewInt(-1) },
			wantErr: true,
		},
		{
			name:    "zero max_funding_per_day is valid",
			modify:  func(p *types.Params) { p.MaxFundingPerDay = math.ZeroInt() },
			wantErr: false,
		},
		{
			name:    "negative min_gas_reserve",
			modify:  func(p *types.Params) { p.MinGasReserve = math.NewInt(-1) },
			wantErr: true,
		},
		{
			name:    "zero min_gas_reserve is valid",
			modify:  func(p *types.Params) { p.MinGasReserve = math.ZeroInt() },
			wantErr: false,
		},
		{
			name:    "zero max_gas_per_exec",
			modify:  func(p *types.Params) { p.MaxGasPerExec = 0 },
			wantErr: true,
		},
		{
			name:    "zero max_execs_per_identity",
			modify:  func(p *types.Params) { p.MaxExecsPerIdentityPerEpoch = 0 },
			wantErr: true,
		},
		{
			name:    "zero shield_epoch_interval",
			modify:  func(p *types.Params) { p.ShieldEpochInterval = 0 },
			wantErr: true,
		},
		{
			name:    "zero min_batch_size",
			modify:  func(p *types.Params) { p.MinBatchSize = 0 },
			wantErr: true,
		},
		{
			name:    "zero max_pending_epochs",
			modify:  func(p *types.Params) { p.MaxPendingEpochs = 0 },
			wantErr: true,
		},
		{
			name:    "zero max_pending_queue_size",
			modify:  func(p *types.Params) { p.MaxPendingQueueSize = 0 },
			wantErr: true,
		},
		{
			name:    "zero max_encrypted_payload_size",
			modify:  func(p *types.Params) { p.MaxEncryptedPayloadSize = 0 },
			wantErr: true,
		},
		{
			name:    "zero max_ops_per_batch",
			modify:  func(p *types.Params) { p.MaxOpsPerBatch = 0 },
			wantErr: true,
		},
		{
			name: "tle_miss_tolerance exceeds window",
			modify: func(p *types.Params) {
				p.TleMissTolerance = 200
				p.TleMissWindow = 100
			},
			wantErr: true,
		},
		{
			name: "tle_miss_tolerance equals window is valid",
			modify: func(p *types.Params) {
				p.TleMissTolerance = 100
				p.TleMissWindow = 100
			},
			wantErr: false,
		},
		{
			name:    "negative tle_jail_duration",
			modify:  func(p *types.Params) { p.TleJailDuration = -1 },
			wantErr: true,
		},
		{
			name:    "zero tle_jail_duration is valid",
			modify:  func(p *types.Params) { p.TleJailDuration = 0 },
			wantErr: false,
		},
		{
			name:    "disabled shield is valid",
			modify:  func(p *types.Params) { p.Enabled = false },
			wantErr: false,
		},
		{
			name:    "encrypted batch enabled is valid",
			modify:  func(p *types.Params) { p.EncryptedBatchEnabled = true },
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := types.NewParams()
			tc.modify(&p)
			err := p.Validate()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// --- Genesis Validation ---

func TestGenesisValidation(t *testing.T) {
	t.Run("default genesis is valid", func(t *testing.T) {
		gs := types.DefaultGenesis()
		require.NoError(t, gs.Validate())
	})

	t.Run("genesis with invalid params fails", func(t *testing.T) {
		gs := types.DefaultGenesis()
		gs.Params.MaxGasPerExec = 0
		require.Error(t, gs.Validate())
	})

	t.Run("genesis with nil registered ops is valid", func(t *testing.T) {
		gs := &types.GenesisState{
			Params:        types.NewParams(),
			RegisteredOps: nil,
		}
		require.NoError(t, gs.Validate())
	})

	t.Run("default genesis has 21 registered ops", func(t *testing.T) {
		gs := types.DefaultGenesis()
		require.Len(t, gs.RegisteredOps, 21)
	})

	t.Run("ownership op must be immediate-only", func(t *testing.T) {
		gs := types.DefaultGenesis()
		gs.RegisteredOps = append(gs.RegisteredOps, types.ShieldedOpRegistration{
			MessageTypeUrl: "/sparkdream.test.v1.MsgOwned",
			NullifierMode:  types.NullifierMode_NULLIFIER_MODE_OWNERSHIP,
			BatchMode:      types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
		})
		require.ErrorIs(t, gs.Validate(), types.ErrOwnershipBatchMode)
	})
}

// --- Default Genesis Ops ---

func TestDefaultGenesisOpsContent(t *testing.T) {
	gs := types.DefaultGenesis()

	// Build a map for easy lookup
	ops := make(map[string]types.ShieldedOpRegistration)
	for _, op := range gs.RegisteredOps {
		ops[op.MessageTypeUrl] = op
	}

	// Verify blog ops
	t.Run("blog MsgCreatePost", func(t *testing.T) {
		op, ok := ops["/sparkdream.blog.v1.MsgCreatePost"]
		require.True(t, ok)
		require.Equal(t, types.ProofDomain_PROOF_DOMAIN_TRUST_TREE, op.ProofDomain)
		require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH, op.NullifierScopeType)
		require.Equal(t, types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER, op.BatchMode)
		require.True(t, op.Active)
	})

	t.Run("blog MsgCreateReply", func(t *testing.T) {
		op, ok := ops["/sparkdream.blog.v1.MsgCreateReply"]
		require.True(t, ok)
		require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD, op.NullifierScopeType)
		require.Equal(t, "post_id", op.ScopeFieldPath)
	})

	t.Run("blog MsgReact", func(t *testing.T) {
		op, ok := ops["/sparkdream.blog.v1.MsgReact"]
		require.True(t, ok)
		require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD, op.NullifierScopeType)
		require.Equal(t, "reply_id|post_id", op.ScopeFieldPath)
	})

	// Verify rep ops (encrypted only)
	t.Run("rep MsgCreateChallenge", func(t *testing.T) {
		op, ok := ops["/sparkdream.rep.v1.MsgCreateChallenge"]
		require.True(t, ok)
		require.Equal(t, types.ProofDomain_PROOF_DOMAIN_TRUST_TREE, op.ProofDomain)
		require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL, op.NullifierScopeType)
		require.Equal(t, types.ShieldBatchMode_SHIELD_BATCH_MODE_ENCRYPTED_ONLY, op.BatchMode)
	})

	// Verify commons ops (EITHER mode — immediate needed while TLE is not yet active)
	t.Run("commons MsgSubmitAnonymousProposal", func(t *testing.T) {
		op, ok := ops["/sparkdream.commons.v1.MsgSubmitAnonymousProposal"]
		require.True(t, ok)
		require.Equal(t, types.ProofDomain_PROOF_DOMAIN_TRUST_TREE, op.ProofDomain)
		require.Equal(t, types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER, op.BatchMode)
	})

	t.Run("commons MsgAnonymousVoteProposal", func(t *testing.T) {
		op, ok := ops["/sparkdream.commons.v1.MsgAnonymousVoteProposal"]
		require.True(t, ok)
		require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD, op.NullifierScopeType)
		require.Equal(t, "proposal_id", op.ScopeFieldPath)
	})

	// FEDERATION-S2-5: per-content nullifier scope ensures one identity =
	// one vote per content, defeating the lone-attacker quorum drive.
	t.Run("federation MsgSubmitArbiterHash", func(t *testing.T) {
		op, ok := ops["/sparkdream.federation.v1.MsgSubmitArbiterHash"]
		require.True(t, ok)
		require.Equal(t, types.NullifierScopeType_NULLIFIER_SCOPE_MESSAGE_FIELD, op.NullifierScopeType)
		require.Equal(t, "content_id", op.ScopeFieldPath)
		require.Equal(t, uint32(2), op.MinTrustLevel, "must require ESTABLISHED+ to match identified bridge gate")
	})

	// Verify nullifier domains are unique
	t.Run("unique nullifier domains", func(t *testing.T) {
		domains := make(map[uint32]string)
		for _, op := range gs.RegisteredOps {
			// Ownership-mode ops record no nullifiers; the domain only labels them.
			if op.NullifierMode == types.NullifierMode_NULLIFIER_MODE_OWNERSHIP {
				continue
			}
			if existing, ok := domains[op.NullifierDomain]; ok && !sharedVoteDomains[[2]string{existing, op.MessageTypeUrl}] {
				t.Errorf("duplicate nullifier domain %d: %s and %s", op.NullifierDomain, existing, op.MessageTypeUrl)
			}
			domains[op.NullifierDomain] = op.MessageTypeUrl
		}
	})
}

// --- NewParams ---

func TestNewParams(t *testing.T) {
	p := types.NewParams()

	require.True(t, p.Enabled)
	require.Equal(t, types.DefaultMaxFundingPerDay, p.MaxFundingPerDay)
	require.Equal(t, types.DefaultMinGasReserve, p.MinGasReserve)
	require.Equal(t, types.DefaultMaxGasPerExec, p.MaxGasPerExec)
	require.Equal(t, types.DefaultMaxExecsPerIdentity, p.MaxExecsPerIdentityPerEpoch)
	require.False(t, p.EncryptedBatchEnabled)
	require.Equal(t, types.DefaultShieldEpochInterval, p.ShieldEpochInterval)
	require.Equal(t, types.DefaultMinBatchSize, p.MinBatchSize)
	require.Equal(t, types.DefaultMaxPendingEpochs, p.MaxPendingEpochs)
	require.Equal(t, types.DefaultMaxPendingQueueSize, p.MaxPendingQueueSize)
	require.Equal(t, types.DefaultMaxEncryptedPayload, p.MaxEncryptedPayloadSize)
	require.Equal(t, types.DefaultMaxOpsPerBatch, p.MaxOpsPerBatch)
	require.Equal(t, types.DefaultTLEMissWindow, p.TleMissWindow)
	require.Equal(t, types.DefaultTLEMissTolerance, p.TleMissTolerance)
	require.Equal(t, types.DefaultTLEJailDuration, p.TleJailDuration)
}

// --- Error Codes ---

func TestErrorCodesUnique(t *testing.T) {
	// Verify key error codes exist and are distinct
	errors := []error{
		types.ErrShieldDisabled,
		types.ErrShieldGasDepleted,
		types.ErrUnregisteredOperation,
		types.ErrOperationInactive,
		types.ErrNullifierUsed,
		types.ErrRateLimitExceeded,
		types.ErrInvalidInnerMessage,
		types.ErrMultiMsgNotAllowed,
		types.ErrInvalidExecMode,
		types.ErrEncryptedBatchDisabled,
		types.ErrMissingEncryptedPayload,
		types.ErrPayloadTooLarge,
		types.ErrInvalidTargetEpoch,
		types.ErrPendingQueueFull,
	}

	seen := make(map[string]bool)
	for _, e := range errors {
		msg := e.Error()
		require.False(t, seen[msg], "duplicate error message: %s", msg)
		seen[msg] = true
	}
}

// --- Module Constants ---

func TestModuleConstants(t *testing.T) {
	require.Equal(t, "shield", types.ModuleName)
	require.Equal(t, "shield", types.StoreKey)
	require.Equal(t, "shield_fee_paid", types.ContextKeyFeePaid)
}

// sharedVoteDomains are op pairs that deliberately share a nullifier domain:
// an up- and a downvote on the same target spend the same nullifier, so a
// member casts one anonymous vote per target either way.
var sharedVoteDomains = map[[2]string]bool{
	{"/sparkdream.forum.v1.MsgUpvotePost", "/sparkdream.forum.v1.MsgDownvotePost"}:           true,
	{"/sparkdream.collect.v1.MsgUpvoteContent", "/sparkdream.collect.v1.MsgDownvoteContent"}: true,
}

func TestPackNullifierScope(t *testing.T) {
	got, err := types.PackNullifierScope(2, 5)
	require.NoError(t, err)
	require.Equal(t, uint64(2)<<56|5, got)

	got, err = types.PackNullifierScope(255, 1<<56-1)
	require.NoError(t, err)
	require.Equal(t, ^uint64(0), got)

	_, err = types.PackNullifierScope(256, 0)
	require.Error(t, err)
	_, err = types.PackNullifierScope(0, 1<<56)
	require.Error(t, err)
}

func TestParamsMaxFeePerExec(t *testing.T) {
	p := types.DefaultParams()
	require.True(t, p.MaxFeePerExec.IsPositive())

	p.MaxFeePerExec = math.NewInt(-1)
	require.Error(t, p.Validate())

	p.MaxFeePerExec = math.Int{}
	require.Error(t, p.Validate())
}

// The public submitter is derived from a fixed seed, so every client and node
// computes the same key; its address must be the one its key signs for.
func TestPublicSubmitter(t *testing.T) {
	priv := &secp256k1.PrivKey{Key: types.PublicSubmitterPrivKey()}
	require.Equal(t, sdk.AccAddress(priv.PubKey().Address()), types.PublicSubmitterAddress())
	require.Equal(t, types.PublicSubmitterAddress(), types.PublicSubmitterAddress())

	// The returned key is a copy: changing it doesn't change the submitter.
	key := types.PublicSubmitterPrivKey()
	key[0] ^= 0xff
	require.NotEqual(t, key, types.PublicSubmitterPrivKey())
}

func TestProvenTrustLevel(t *testing.T) {
	ctx := sdk.Context{}.WithContext(context.Background())

	_, ok := types.ProvenTrustLevel(ctx)
	require.False(t, ok, "no level outside a shield exec")

	level, ok := types.ProvenTrustLevel(types.WithProvenTrustLevel(ctx, 2))
	require.True(t, ok)
	require.Equal(t, uint32(2), level)

	// A zero level is still a proven level, distinct from none.
	level, ok = types.ProvenTrustLevel(types.WithProvenTrustLevel(ctx, 0))
	require.True(t, ok)
	require.Zero(t, level)
}
