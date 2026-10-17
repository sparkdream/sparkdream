package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/keeper"
	"sparkdream/x/shield/types"
)

// writeThenRouter routes every message to a handler that first writes a
// marker record (the decryption key of markerEpoch), then burns gas, then
// returns err. burn over the per-exec cap makes the handler run out of gas.
type writeThenRouter struct {
	k    keeper.Keeper
	burn uint64
	err  error
}

const markerEpoch = 999_999

func (r writeThenRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler { return r.handle }

func (r writeThenRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return r.handle }

func (r writeThenRouter) handle(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
	if err := r.k.SetShieldEpochDecryptionKey(ctx, types.ShieldEpochDecryptionKey{Epoch: markerEpoch, DecryptionKey: []byte{1}}); err != nil {
		return nil, err
	}
	ctx.GasMeter().ConsumeGas(r.burn, "inner work")
	return &sdk.Result{}, r.err
}

// TestBatchOpExecutionIsContained: an encrypted batch op runs in the
// EndBlocker, where no runTx recovery exists. Its inner message runs on a
// cache context, so a failing handler leaves no partial state, and a panic
// (here: running out of the max_gas_per_exec meter) drops the op instead of
// halting the chain. A succeeding handler's writes are kept.
func TestBatchOpExecutionIsContained(t *testing.T) {
	cases := []struct {
		name       string
		burn       uint64
		err        error
		wantResult int
		wantMarker bool
	}{
		{"success keeps writes", 1_000, nil, keeper.BatchResultOK, true},
		{"error rolls back writes", 1_000, types.ErrInvalidInnerMessage, keeper.BatchResultOtherFail, false},
		{"out of gas is recovered", 600_000, nil, keeper.BatchResultOtherFail, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			f.keeper.SetRouter(writeThenRouter{k: f.keeper, burn: tc.burn, err: tc.err})
			f.keeper.RegisterShieldAwareModule("/sparkdream.shield.v1.", &ownedModule{})

			inner := innerAny(t, &types.MsgTriggerDkg{Authority: authtypes.NewModuleAddress(types.ModuleName).String()})
			require.NoError(t, f.keeper.SetShieldedOp(f.ctx, types.ShieldedOpRegistration{
				MessageTypeUrl:     inner.TypeUrl,
				ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
				NullifierDomain:    11,
				NullifierScopeType: types.NullifierScopeType_NULLIFIER_SCOPE_GLOBAL,
				Active:             true,
				BatchMode:          types.ShieldBatchMode_SHIELD_BATCH_MODE_EITHER,
			}))

			// No verification key is stored, so this testparams build skips
			// proof verification and a dummy proof reaches execution.
			nullifier := make([]byte, 32)
			nullifier[0] = byte(i + 1)
			plaintext, err := (&types.MsgShieldedExec{InnerMessage: inner, Proof: []byte("proof"), Nullifier: nullifier}).Marshal()
			require.NoError(t, err)
			const epoch = uint64(3)
			master := tle.Suite.G1().Scalar().Pick(random.New())
			payload, err := tle.Encrypt(tle.MasterPublicKeyG2(master), epoch, plaintext)
			require.NoError(t, err)
			op := &types.PendingShieldedOp{
				Id:                 1,
				TargetEpoch:        epoch,
				EncryptedPayload:   payload,
				MerkleRoot:         make([]byte, 32),
				ProofDomain:        types.ProofDomain_PROOF_DOMAIN_TRUST_TREE,
				RateLimitNullifier: make([]byte, 32),
				SubmittedAtEpoch:   epoch,
			}

			params := types.DefaultParams()
			params.MaxGasPerExec = 500_000
			var result int
			require.NotPanics(t, func() {
				result = f.keeper.ProcessEncryptedOp(f.ctx, params, op, tle.EpochDecryptionKey(master, epoch))
			})
			require.Equal(t, tc.wantResult, result)

			_, found := f.keeper.GetShieldEpochDecryptionKeyVal(f.ctx, markerEpoch)
			require.Equal(t, tc.wantMarker, found, "inner handler writes kept only on success")
		})
	}
}
