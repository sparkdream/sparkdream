package ante_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

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

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	shieldante "sparkdream/x/shield/ante"
	shieldtypes "sparkdream/x/shield/types"
)

// --- Mock types ---

type mockShieldKeeper struct {
	params      shieldtypes.Params
	err         error
	precheckErr error
	// consumes makes PrecheckExec report that it spent the exec.
	consumes bool
	// chargeErr is what ChargeEpochFee returns; charged records each fee.
	chargeErr error
	charged   *[]math.Int
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

func (m mockShieldKeeper) PrecheckExec(_ sdk.Context, _ *shieldtypes.MsgShieldedExec) (bool, error) {
	if m.precheckErr != nil {
		return false, m.precheckErr
	}
	return m.consumes, nil
}

func (m mockShieldKeeper) ChargeEpochFee(_ sdk.Context, fee math.Int, _ math.Int) error {
	if m.chargeErr != nil {
		return m.chargeErr
	}
	if m.charged != nil {
		*m.charged = append(*m.charged, fee)
	}
	return nil
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

// mockUnorderedSigTx is a mockSigTx that reports its unordered flag.
type mockUnorderedSigTx struct {
	mockSigTx
	unordered bool
}

func (m mockUnorderedSigTx) GetUnordered() bool             { return m.unordered }
func (m mockUnorderedSigTx) GetTimeoutTimeStamp() time.Time { return time.Time{} }

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
	gas  uint64
}

func (m mockTx) GetMsgs() []sdk.Msg                  { return m.msgs }
func (m mockTx) GetMsgsV2() ([]proto.Message, error) { return nil, nil }
func (m mockTx) ValidateBasic() error                { return nil }
func (m mockTx) GetFee() sdk.Coins                   { return m.fees }
func (m mockTx) GetGas() uint64 {
	if m.gas != 0 {
		return m.gas
	}
	return 200000
}
func (m mockTx) FeePayer() []byte   { return nil }
func (m mockTx) FeeGranter() []byte { return nil }

// terminalHandler is a no-op next handler
func terminalHandler(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	return ctx, nil
}

// testSubmitter is the shared anonymous submitter anonymous clients sign with.
var testSubmitter = shieldtypes.PublicSubmitterAddress().String()

// validShieldedExec returns an immediate-mode MsgShieldedExec that passes the
// ante handler's format checks. Proof validity comes from the mock keeper's
// PrecheckExec.
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

// A batch exec is validated like an immediate one before the module pays:
// format checks here, then the keeper's PrecheckExec (every msg-server check
// plus the submission proof). A rejected submission costs the reserve nothing.
func TestShieldGasDecorator_BatchPrechecksBeforePayment(t *testing.T) {
	params := shieldtypes.DefaultParams()
	params.EncryptedBatchEnabled = true

	batchMsg := func() *shieldtypes.MsgShieldedExec {
		msg := validShieldedExec()
		msg.ExecMode = shieldtypes.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH
		msg.TargetEpoch = 1 // the mock keeper's current epoch
		msg.EncryptedPayload = make([]byte, 32)
		return msg
	}
	fees := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000)))

	cases := []struct {
		name        string
		mutate      func(*shieldtypes.MsgShieldedExec)
		precheckErr error
		wantErr     error
	}{
		{"valid batch exec is paid for", func(*shieldtypes.MsgShieldedExec) {}, nil, nil},
		{"submission proof too short", func(m *shieldtypes.MsgShieldedExec) { m.Proof = make([]byte, 64) }, nil, shieldtypes.ErrInvalidProof},
		{"short rate-limit nullifier", func(m *shieldtypes.MsgShieldedExec) { m.RateLimitNullifier = make([]byte, 16) }, nil, shieldtypes.ErrInvalidNullifierLength},
		{"keeper precheck fails (wrong epoch)", func(*shieldtypes.MsgShieldedExec) {}, shieldtypes.ErrInvalidTargetEpoch, shieldtypes.ErrInvalidTargetEpoch},
		{"keeper precheck fails (queue full)", func(*shieldtypes.MsgShieldedExec) {}, shieldtypes.ErrPendingQueueFull, shieldtypes.ErrPendingQueueFull},
		{"keeper precheck fails (bad submission proof)", func(*shieldtypes.MsgShieldedExec) {}, shieldtypes.ErrInvalidProof, shieldtypes.ErrInvalidProof},
		{"keeper precheck fails (identity over quota)", func(*shieldtypes.MsgShieldedExec) {}, shieldtypes.ErrRateLimitExceeded, shieldtypes.ErrRateLimitExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := makeTestContext(t)
			sk := mockShieldKeeper{params: params, precheckErr: tc.precheckErr}
			bk := &mockBankKeeper{}
			decorator := shieldante.NewShieldGasDecorator(sk, bk)
			msg := batchMsg()
			tc.mutate(msg)
			ctx, err := decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}, fees: fees}, false, terminalHandler)
			if tc.wantErr == nil {
				require.NoError(t, err)
				require.True(t, bk.sent, "fee paid by the module")
				require.True(t, shieldtypes.ProofVerified(ctx, msg), "msg server skips the second verification")
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
			require.False(t, bk.sent, "nothing paid for a rejected submission")
		})
	}
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

// SHD-01: a nullifier, rate-limit nullifier or merkle root at or above the
// BN254 scalar modulus is refused before anything is checked or paid: the
// verifier would reduce it mod r and accept an alias of a spent nullifier.
func TestShieldGasDecorator_NonCanonicalInputsNotPaidFor(t *testing.T) {
	nPlusR := new(big.Int).Add(big.NewInt(7), fr.Modulus()).FillBytes(make([]byte, 32))
	cases := map[string]func(m *shieldtypes.MsgShieldedExec){
		"nullifier":            func(m *shieldtypes.MsgShieldedExec) { m.Nullifier = nPlusR },
		"rate-limit nullifier": func(m *shieldtypes.MsgShieldedExec) { m.RateLimitNullifier = nPlusR },
		"merkle root":          func(m *shieldtypes.MsgShieldedExec) { m.MerkleRoot = nPlusR },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			bk := &mockBankKeeper{}
			decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams()}, bk)
			msg := validShieldedExec()
			mutate(msg)
			tx := mockTx{msgs: []sdk.Msg{msg}, fees: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1000)))}
			_, err := decorator.AnteHandle(makeTestContext(t), tx, false, terminalHandler)
			require.ErrorIs(t, err, shieldtypes.ErrNonCanonicalPublicInput)
			require.False(t, bk.sent)
		})
	}
}

// SHD-03: when the keeper spent the exec in the ante state, the msg server is
// told so through the context and must not spend it again.
func TestShieldGasDecorator_MarksExecConsumed(t *testing.T) {
	for _, consumes := range []bool{true, false} {
		decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams(), consumes: consumes}, &mockBankKeeper{})
		msg := validShieldedExec()
		newCtx, err := decorator.AnteHandle(makeTestContext(t), mockTx{msgs: []sdk.Msg{msg}}, false, terminalHandler)
		require.NoError(t, err)
		require.Equal(t, consumes, shieldtypes.ExecConsumed(newCtx, msg))
		require.False(t, shieldtypes.ExecConsumed(newCtx, validShieldedExec()), "pointer identity, not equality")
	}
}

// SHD-04: in CheckTx a shield tx must meet the node's minimum gas prices like
// any other tx, though the module pays: otherwise a zero fee buys any gas
// limit. DeliverTx doesn't apply the node-local rule.
func TestShieldGasDecorator_MinGasPrices(t *testing.T) {
	minGas := sdk.NewDecCoins(sdk.NewDecCoinFromDec("uspark", math.LegacyMustNewDecFromStr("0.025")))
	cases := []struct {
		name    string
		check   bool
		fees    sdk.Coins
		gas     uint64
		wantErr error
	}{
		{"zero fee in CheckTx", true, nil, 200_000, sdkerrors.ErrInsufficientFee},
		{"fee below gas x price", true, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(4_999))), 200_000, sdkerrors.ErrInsufficientFee},
		{"fee meets gas x price", true, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(5_000))), 200_000, nil},
		{"huge gas limit, capped fee", true, sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(50_000))), 10_000_000, sdkerrors.ErrInsufficientFee},
		{"zero fee in DeliverTx", false, nil, 200_000, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bk := &mockBankKeeper{}
			decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams()}, bk)
			ctx := makeTestContext(t).WithMinGasPrices(minGas).WithIsCheckTx(tc.check)
			_, err := decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: tc.fees, gas: tc.gas}, false, terminalHandler)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.False(t, bk.sent)
				return
			}
			require.NoError(t, err)
		})
	}
}

// SHD-12: every paid fee is booked against the chain-wide epoch budget, and a
// fee the budget can't cover is refused before the module pays.
func TestShieldGasDecorator_EpochFeeBudget(t *testing.T) {
	var charged []math.Int
	bk := &mockBankKeeper{}
	decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams(), charged: &charged}, bk)
	fee := sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1234)))
	_, err := decorator.AnteHandle(makeTestContext(t), mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: fee}, false, terminalHandler)
	require.NoError(t, err)
	require.Equal(t, []math.Int{math.NewInt(1234)}, charged)
	require.True(t, bk.sent)

	// A zero fee costs the reserve nothing and is not booked.
	charged = nil
	_, err = decorator.AnteHandle(makeTestContext(t), mockTx{msgs: []sdk.Msg{validShieldedExec()}}, false, terminalHandler)
	require.NoError(t, err)
	require.Empty(t, charged)

	bk = &mockBankKeeper{}
	decorator = shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams(), chargeErr: shieldtypes.ErrEpochFeeBudgetExhausted}, bk)
	_, err = decorator.AnteHandle(makeTestContext(t), mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: fee}, false, terminalHandler)
	require.ErrorIs(t, err, shieldtypes.ErrEpochFeeBudgetExhausted)
	require.False(t, bk.sent, "an exec over the epoch budget must not be paid for")
}

// SHD-14: the shared submitter's key is public, so its shield txs must be
// unordered; other signers may send ordered shield txs.
func TestShieldGasDecorator_PublicSubmitterMustBeUnordered(t *testing.T) {
	decorator := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams()}, &mockBankKeeper{})
	public := shieldtypes.PublicSubmitterAddress()
	tx := mockUnorderedSigTx{mockSigTx: mockSigTx{mockTx: mockTx{msgs: []sdk.Msg{validShieldedExec()}}, signers: [][]byte{public}}}

	_, err := decorator.AnteHandle(makeTestContext(t), tx, false, terminalHandler)
	require.ErrorIs(t, err, shieldtypes.ErrOrderedPublicSubmitterTx)
	// A tx type without the unordered flag at all is treated as ordered.
	_, err = decorator.AnteHandle(makeTestContext(t), tx.mockSigTx, false, terminalHandler)
	require.ErrorIs(t, err, shieldtypes.ErrOrderedPublicSubmitterTx)

	tx.unordered = true
	_, err = decorator.AnteHandle(makeTestContext(t), tx, false, terminalHandler)
	require.NoError(t, err)

	tx.unordered = false
	tx.signers = [][]byte{sdk.AccAddress(make([]byte, 20))}
	_, err = decorator.AnteHandle(makeTestContext(t), tx, false, terminalHandler)
	require.NoError(t, err)
}
