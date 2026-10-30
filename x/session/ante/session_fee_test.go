package ante_test

import (
	"context"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	commontypes "sparkdream/x/common/types"
	sessionante "sparkdream/x/session/ante"
	sessiontypes "sparkdream/x/session/types"
	shieldante "sparkdream/x/shield/ante"
)

type mockSessionKeeper struct {
	session sessiontypes.Session
	spent   []sdk.Coin
}

func (m *mockSessionKeeper) GetSession(_ context.Context, _, _ string) (sessiontypes.Session, error) {
	return m.session, nil
}

func (m *mockSessionKeeper) UpdateSessionSpent(_ context.Context, _, _ string, fee sdk.Coin) error {
	m.spent = append(m.spent, fee)
	m.session.Spent = m.session.Spent.Add(fee)
	return nil
}

type mockBank struct{ charges []sdk.Coins }

func (m *mockBank) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, _ string, amt sdk.Coins) error {
	m.charges = append(m.charges, amt)
	return nil
}

type feeTx struct {
	msgs []sdk.Msg
	fees sdk.Coins
	gas  uint64
}

func (t feeTx) GetMsgs() []sdk.Msg                  { return t.msgs }
func (t feeTx) GetMsgsV2() ([]proto.Message, error) { return nil, nil }
func (t feeTx) GetFee() sdk.Coins                   { return t.fees }
func (t feeTx) GetGas() uint64                      { return t.gas }
func (t feeTx) FeePayer() []byte                    { return nil }
func (t feeTx) FeeGranter() []byte                  { return nil }

// countingDeduct stands in for the SDK DeductFeeDecorator.
type countingDeduct struct{ ran int }

func (c *countingDeduct) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	c.ran++
	return next(ctx, tx, simulate)
}

var (
	granter = sdk.AccAddress([]byte("granter_____________")).String()
	grantee = sdk.AccAddress([]byte("grantee_____________")).String()
)

func newCtx(t *testing.T) sdk.Context {
	key := storetypes.NewKVStoreKey("session_ante_test")
	return testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("t_session_ante_test")).Ctx.
		WithBlockTime(time.Unix(1_000_000, 0))
}

func activeSession(spent, limit int64) sessiontypes.Session {
	return sessiontypes.Session{
		Granter:      granter,
		Grantee:      grantee,
		SpendLimit:   sdk.NewInt64Coin("uspark", limit),
		Spent:        sdk.NewInt64Coin("uspark", spent),
		Expiration:   time.Unix(2_000_000, 0),
		MaxExecCount: 10,
	}
}

func execMsg(n int) *sessiontypes.MsgExecSession {
	m := &sessiontypes.MsgExecSession{Granter: granter, Grantee: grantee}
	for i := 0; i < n; i++ {
		m.Msgs = append(m.Msgs, &codectypes.Any{TypeUrl: "/sparkdream.blog.v1.MsgCreatePost"})
	}
	return m
}

// run chains SessionFeeDecorator -> SkipIfFeePaidDecorator(deduct), the
// order app.go uses, and returns the deduct counter.
func run(ctx sdk.Context, sk *mockSessionKeeper, bk *mockBank, tx sdk.Tx) (*countingDeduct, sdk.Context, error) {
	deduct := &countingDeduct{}
	h := sdk.ChainAnteDecorators(
		sessionante.NewSessionFeeDecorator(sk, bk),
		shieldante.NewSkipIfFeePaidDecorator(deduct),
	)
	newCtx, err := h(ctx, tx, false)
	return deduct, newCtx, err
}

// SES-01: a zero-fee session tx no longer passes CheckTx on a node with
// minimum gas prices (it used to skip the check with the deduct decorator).
func TestSessionFee_ZeroFeeRefusedByMinGasPrices(t *testing.T) {
	minGas := sdk.NewDecCoins(sdk.NewDecCoinFromDec("uspark", sdkmath.LegacyMustNewDecFromStr("0.0025")))
	ctx := newCtx(t).WithIsCheckTx(true).WithMinGasPrices(minGas)

	sk := &mockSessionKeeper{session: activeSession(0, 1_000_000)}
	bk := &mockBank{}
	_, _, err := run(ctx, sk, bk, feeTx{msgs: []sdk.Msg{execMsg(1)}, gas: 200_000})
	require.ErrorIs(t, err, sdkerrors.ErrInsufficientFee)
	require.Empty(t, bk.charges)

	// A fee at the node minimum passes, is charged to the granter exactly
	// once, and the standard deduct decorator is skipped.
	sk = &mockSessionKeeper{session: activeSession(0, 1_000_000)}
	deduct, newCtx, err := run(ctx, sk, bk, feeTx{msgs: []sdk.Msg{execMsg(1)}, gas: 200_000, fees: sdk.NewCoins(sdk.NewInt64Coin("uspark", 500))})
	require.NoError(t, err)
	require.Len(t, bk.charges, 1)
	require.Equal(t, 0, deduct.ran)
	require.Len(t, sk.spent, 1)
	require.Zero(t, newCtx.Priority(), "fee/gas = 500/200_000 rounds down to 0")

	// DeliverTx keeps the SDK semantics (min gas prices are node-local).
	_, _, err = run(newCtx.WithIsCheckTx(false), &mockSessionKeeper{session: activeSession(0, 1_000_000)}, &mockBank{}, feeTx{msgs: []sdk.Msg{execMsg(1)}, gas: 200_000})
	require.NoError(t, err)
}

// SES-01: an exhausted budget or exec count refuses the tx in the ante.
func TestSessionFee_ExhaustedSessionRefused(t *testing.T) {
	ctx := newCtx(t)
	_, _, err := run(ctx, &mockSessionKeeper{session: activeSession(1, 1)}, &mockBank{}, feeTx{msgs: []sdk.Msg{execMsg(1)}, gas: 100_000})
	require.ErrorIs(t, err, sessiontypes.ErrSpendLimitExceeded)

	s := activeSession(0, 1_000)
	s.ExecCount = 9
	_, _, err = run(ctx, &mockSessionKeeper{session: s}, &mockBank{}, feeTx{msgs: []sdk.Msg{execMsg(2)}, gas: 100_000})
	require.ErrorIs(t, err, sessiontypes.ErrExecCountExceeded)
}

// VLT-01: an empty exec is refused before any fee moves.
func TestSessionFee_EmptyExecRefused(t *testing.T) {
	bk := &mockBank{}
	_, _, err := run(newCtx(t), &mockSessionKeeper{session: activeSession(0, 1_000)}, bk, feeTx{msgs: []sdk.Msg{execMsg(0)}, gas: 100_000, fees: sdk.NewCoins(sdk.NewInt64Coin("uspark", 10))})
	require.ErrorIs(t, err, sessiontypes.ErrEmptyMsgs)
	require.Empty(t, bk.charges)
}

// APP-10: x/session and x/shield share one typed context key.
func TestSessionFee_SharedTypedKey(t *testing.T) {
	require.Equal(t, commontypes.ContextKeyFeePaid, sessiontypes.ContextKeySessionFeePaid)
	_, newCtx, err := run(newCtx(t), &mockSessionKeeper{session: activeSession(0, 1_000)}, &mockBank{}, feeTx{msgs: []sdk.Msg{execMsg(1)}, gas: 100_000, fees: sdk.NewCoins(sdk.NewInt64Coin("uspark", 10))})
	require.NoError(t, err)
	paid, ok := newCtx.Value(commontypes.ContextKeyFeePaid).(bool)
	require.True(t, ok && paid)
	// The old untyped string no longer reads as paid.
	require.Nil(t, newCtx.Value("shield_fee_paid"))
}

// A tx without MsgExecSession goes through the normal deduct decorator.
func TestSessionFee_NonSessionTxUsesDeduct(t *testing.T) {
	deduct, _, err := run(newCtx(t), &mockSessionKeeper{}, &mockBank{}, feeTx{gas: 1})
	require.NoError(t, err)
	require.Equal(t, 1, deduct.ran)
}
