package ante_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	shieldante "sparkdream/x/shield/ante"
	shieldtypes "sparkdream/x/shield/types"
)

// --- Mock types ---

type mockShieldKeeper struct {
	params          shieldtypes.Params
	err             error
	submitterCounts map[string]uint64
	precheckErr     error
}

func (m mockShieldKeeper) GetShieldParams(ctx sdk.Context) (shieldtypes.Params, error) {
	if m.err != nil {
		return shieldtypes.Params{}, m.err
	}
	return m.params, nil
}

func (m mockShieldKeeper) GetCurrentEpoch(_ context.Context) uint64 {
	return 1
}

func (m mockShieldKeeper) GetSubmitterExecCount(_ context.Context, _ uint64, _ string) uint64 {
	return 0
}

func (m mockShieldKeeper) IncrementSubmitterExecCount(_ context.Context, _ uint64, _ string) {
}

func (m mockShieldKeeper) PrecheckImmediate(_ sdk.Context, _ *shieldtypes.MsgShieldedExec) error {
	return m.precheckErr
}

func (m mockShieldKeeper) BondDenom(_ context.Context) string {
	return "uspark"
}

// No VK stored: the proof guard stands aside, as nothing is verified.
func (m mockShieldKeeper) GetVerificationKeyVal(_ context.Context, _ string) (shieldtypes.VerificationKey, bool) {
	return shieldtypes.VerificationKey{}, false
}

func (m mockShieldKeeper) GetShieldedOp(_ context.Context, _ string) (shieldtypes.ShieldedOpRegistration, bool) {
	return shieldtypes.ShieldedOpRegistration{}, false
}

// mockSigTx is a mockTx that reports its signers.
type mockSigTx struct {
	mockTx
	signers [][]byte
}

func (m mockSigTx) GetSigners() ([][]byte, error)                   { return m.signers, nil }
func (m mockSigTx) GetPubKeys() ([]cryptotypes.PubKey, error)       { return nil, nil }
func (m mockSigTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return nil, nil }

type mockBankKeeper struct {
	sendErr error
	sent    bool
}

func (m *mockBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = true
	return nil
}

// mockTx implements sdk.Tx and sdk.FeeTx
type mockTx struct {
	msgs []sdk.Msg
	fees sdk.Coins
}

func (m mockTx) GetMsgs() []sdk.Msg                  { return m.msgs }
func (m mockTx) GetMsgsV2() ([]proto.Message, error) { return nil, nil }
func (m mockTx) ValidateBasic() error                { return nil }
func (m mockTx) GetFee() sdk.Coins                   { return m.fees }
func (m mockTx) GetGas() uint64                      { return 200000 }
func (m mockTx) FeePayer() []byte                    { return nil }
func (m mockTx) FeeGranter() []byte                  { return nil }

// terminalHandler is a no-op next handler
func terminalHandler(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	return ctx, nil
}

// testSubmitter is the shared anonymous submitter anonymous clients sign with.
var testSubmitter = shieldtypes.PublicSubmitterAddress().String()

// validShieldedExec returns an immediate-mode MsgShieldedExec that passes the
// ante handler's format checks. Proof validity comes from the mock keeper's
// PrecheckImmediate.
func validShieldedExec() *shieldtypes.MsgShieldedExec {
	return &shieldtypes.MsgShieldedExec{
		Submitter:          testSubmitter,
		Nullifier:          make([]byte, 32),
		RateLimitNullifier: make([]byte, 32),
		Proof:              make([]byte, 128),
		ExecMode:           shieldtypes.ShieldExecMode_SHIELD_EXEC_IMMEDIATE,
	}
}

// --- ShieldGasDecorator Tests ---

func TestShieldGasDecorator_NonShieldedTx(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	// Non-shielded tx should pass through
	tx := mockTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{}},
	}

	newCtx, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)

	// ContextKeyFeePaid should NOT be set
	feePaid, ok := newCtx.Value(shieldtypes.ContextKeyFeePaid).(bool)
	require.False(t, ok && feePaid)

	// Bank should NOT have been called
	require.False(t, bk.sent)
}

func TestShieldGasDecorator_MultiMsgRejected(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	// Multi-message tx with MsgShieldedExec should be rejected
	tx := mockTx{
		msgs: []sdk.Msg{
			&shieldtypes.MsgShieldedExec{},
			&banktypes.MsgSend{},
		},
	}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.Error(t, err)
	require.ErrorIs(t, err, shieldtypes.ErrMultiMsgNotAllowed)
}

func TestShieldGasDecorator_ShieldDisabled(t *testing.T) {
	ctx := makeTestContext(t)
	params := shieldtypes.DefaultParams()
	params.Enabled = false
	sk := mockShieldKeeper{params: params}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	tx := mockTx{
		msgs: []sdk.Msg{&shieldtypes.MsgShieldedExec{}},
	}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.Error(t, err)
	require.ErrorIs(t, err, shieldtypes.ErrShieldDisabled)
}

func TestShieldGasDecorator_ZeroFees(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	tx := mockTx{
		msgs: []sdk.Msg{validShieldedExec()},
		fees: sdk.Coins{}, // Zero fees
	}

	newCtx, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)

	// Fee-paid flag should be set
	feePaid, ok := newCtx.Value(shieldtypes.ContextKeyFeePaid).(bool)
	require.True(t, ok)
	require.True(t, feePaid)

	// Bank should NOT have been called (no fees to transfer)
	require.False(t, bk.sent)
}

func TestShieldGasDecorator_FeesPaid(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	tx := mockTx{
		msgs: []sdk.Msg{validShieldedExec()},
		fees: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000))),
	}

	newCtx, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)

	// Fee-paid flag should be set
	feePaid, ok := newCtx.Value(shieldtypes.ContextKeyFeePaid).(bool)
	require.True(t, ok)
	require.True(t, feePaid)

	// Bank should have been called
	require.True(t, bk.sent)
}

// The submitter picks the fee but the module pays it, so a fee above
// max_fee_per_exec, or in any denom but the bond denom, is refused unpaid.
func TestShieldGasDecorator_FeeCapped(t *testing.T) {
	params := shieldtypes.DefaultParams()
	params.MaxFeePerExec = math.NewInt(1000)

	tests := []struct {
		name string
		fees sdk.Coins
		ok   bool
	}{
		{"at the cap", sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000))), true},
		{"above the cap", sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1001))), false},
		{"other denom", sdk.NewCoins(sdk.NewCoin("uatom", math.NewInt(1))), false},
		{"bond denom plus another", sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1)), sdk.NewCoin("uatom", math.NewInt(1))), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bk := &mockBankKeeper{}
			decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: params}, bk)
			tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: tc.fees}

			_, err := decorator.AnteHandle(makeTestContext(t), tx, false, terminalHandler)
			if tc.ok {
				require.NoError(t, err)
				require.True(t, bk.sent)
				return
			}
			require.ErrorIs(t, err, shieldtypes.ErrFeeTooHigh)
			require.False(t, bk.sent, "an over-cap fee must not be paid")
		})
	}
}

// After the ante handler verifies an immediate exec's proof, it tells the msg
// server so the proof isn't verified a second time.
func TestShieldGasDecorator_MarksProofVerified(t *testing.T) {
	decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams()}, &mockBankKeeper{})
	msg := validShieldedExec()

	newCtx, err := decorator.AnteHandle(makeTestContext(t), mockTx{msgs: []sdk.Msg{msg}}, false, terminalHandler)
	require.NoError(t, err)
	require.True(t, shieldtypes.ProofVerified(newCtx, msg))
	require.False(t, shieldtypes.ProofVerified(newCtx, validShieldedExec()), "only the verified message itself")
}

func TestShieldGasDecorator_GasDepleted(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{sendErr: errors.New("insufficient funds")}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	tx := mockTx{
		msgs: []sdk.Msg{validShieldedExec()},
		fees: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000))),
	}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.Error(t, err)
	require.ErrorIs(t, err, shieldtypes.ErrShieldGasDepleted)
}

func TestShieldGasDecorator_InvalidProofNotPaidFor(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), precheckErr: shieldtypes.ErrInvalidProof}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	tx := mockTx{
		msgs: []sdk.Msg{validShieldedExec()},
		fees: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000))),
	}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.ErrorIs(t, err, shieldtypes.ErrInvalidProof)
	require.False(t, bk.sent, "an invalid proof must not be paid for")
}

func TestShieldGasDecorator_PublicSubmitterOnlySignsShieldedExec(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	// Its private key is public: anything but MsgShieldedExec is refused.
	tx := mockSigTx{
		mockTx:  mockTx{msgs: []sdk.Msg{&banktypes.MsgSend{}}},
		signers: [][]byte{shieldtypes.PublicSubmitterAddress()},
	}
	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	// Other signers pass through untouched.
	tx.signers = [][]byte{sdk.AccAddress(make([]byte, 20))}
	_, err = decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)
}

func TestShieldGasDecorator_BatchRejectedWhenDisabled(t *testing.T) {
	ctx := makeTestContext(t)
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(sk, bk)

	msg := validShieldedExec()
	msg.ExecMode = shieldtypes.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH
	tx := mockTx{
		msgs: []sdk.Msg{msg},
		fees: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000))),
	}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.ErrorIs(t, err, shieldtypes.ErrEncryptedBatchDisabled)
	require.False(t, bk.sent)
}

// --- SkipIfFeePaidDecorator Tests ---

type mockInnerDecorator struct {
	called bool
}

func (m *mockInnerDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	m.called = true
	return next(ctx, tx, simulate)
}

func TestSkipIfFeePaid_SkipsWhenPaid(t *testing.T) {
	ctx := makeTestContext(t)
	inner := &mockInnerDecorator{}
	decorator := shieldante.NewSkipIfFeePaidDecorator(inner)

	// Set fee-paid flag
	ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, true)

	tx := mockTx{msgs: []sdk.Msg{&shieldtypes.MsgShieldedExec{}}}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)

	// Inner decorator should NOT have been called
	require.False(t, inner.called)
}

func TestSkipIfFeePaid_DelegatesWhenNotPaid(t *testing.T) {
	ctx := makeTestContext(t)
	inner := &mockInnerDecorator{}
	decorator := shieldante.NewSkipIfFeePaidDecorator(inner)

	// No fee-paid flag
	tx := mockTx{msgs: []sdk.Msg{&banktypes.MsgSend{}}}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)

	// Inner decorator SHOULD have been called
	require.True(t, inner.called)
}

func TestSkipIfFeePaid_DelegatesWhenFlagFalse(t *testing.T) {
	ctx := makeTestContext(t)
	inner := &mockInnerDecorator{}
	decorator := shieldante.NewSkipIfFeePaidDecorator(inner)

	// Fee-paid flag explicitly false
	ctx = ctx.WithValue(shieldtypes.ContextKeyFeePaid, false)

	tx := mockTx{msgs: []sdk.Msg{&banktypes.MsgSend{}}}

	_, err := decorator.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)

	// Inner decorator SHOULD have been called
	require.True(t, inner.called)
}

// --- Helpers ---

func makeTestContext(t *testing.T) sdk.Context {
	t.Helper()
	key := storetypes.NewKVStoreKey("test")
	testCtx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
	return testCtx.Ctx
}
