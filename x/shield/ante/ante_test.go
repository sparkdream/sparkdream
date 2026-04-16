package ante_test

import (
	"context"
	"errors"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

// validShieldedExec returns a MsgShieldedExec that passes ante-handler anti-spam checks
// (32-byte nullifier, submitter address). The inner message and proof are not validated
// by the ante handler — only by the msg_server.
func validShieldedExec() *shieldtypes.MsgShieldedExec {
	return &shieldtypes.MsgShieldedExec{
		Submitter: "cosmos1testsubmitter",
		Nullifier: make([]byte, 32),
		ExecMode:  shieldtypes.ShieldExecMode_SHIELD_EXEC_ENCRYPTED_BATCH,
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
