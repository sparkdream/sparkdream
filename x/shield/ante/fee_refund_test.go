package ante_test

import (
	"encoding/hex"
	"errors"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	shieldante "sparkdream/x/shield/ante"
	shieldtypes "sparkdream/x/shield/types"
)

func uspark(n int64) sdk.Coins { return sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(n))) }

// finalizeCtx is a DeliverTx context for one tx with a gas limit.
func finalizeCtx(t *testing.T, gasLimit uint64) sdk.Context {
	t.Helper()
	return makeTestContext(t).
		WithExecMode(sdk.ExecModeFinalize).
		WithBlockHeight(10).
		WithTxBytes([]byte("tx-" + t.Name())).
		WithGasMeter(storetypes.NewGasMeter(gasLimit))
}

func terminalPost(ctx sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) { return ctx, nil }

// runAnteAndPost charges the fee in the ante, burns gasUsed of the tx's gas,
// then runs the post handler as baseapp would (success tells it whether the
// messages ran through).
func runAnteAndPost(t *testing.T, sk mockShieldKeeper, bk *mockBankKeeper, tx mockTx, mode sdk.ExecMode, simulate, success bool, gasUsed uint64) (sdk.Context, error) {
	t.Helper()
	ctx := finalizeCtx(t, tx.GetGas()).WithExecMode(mode)
	ante := shieldante.NewShieldGasDecorator(sk, bk)
	post := shieldante.NewFeeRefundDecorator(sk, bk, ante.FeeMarks())
	anteCtx, err := ante.AnteHandle(ctx, tx, simulate, terminalHandler)
	if err != nil {
		return anteCtx, err
	}
	ctx.GasMeter().ConsumeGas(gasUsed, "msgs")
	// baseapp hands the post handler a fresh event manager on the tx context
	return post.PostHandle(ctx.WithEventManager(sdk.NewEventManager()), tx, simulate, success, terminalPost)
}

// TestRefundAmount: the charged part is ceil(fee x used / limit), so the
// refund rounds down and the fee collector never gets less than gas used at
// the implied price; nothing comes back once the limit is reached.
func TestRefundAmount(t *testing.T) {
	cases := []struct {
		name            string
		fee             int64
		used, limit     uint64
		refund, charged int64
	}{
		{"half the gas", 1000, 100_000, 200_000, 500, 500},
		{"rounds the refund down", 1000, 1, 3, 666, 334},
		{"all the gas", 1000, 200_000, 200_000, 0, 1000},
		{"over the limit", 1000, 300_000, 200_000, 0, 1000},
		{"zero limit", 1000, 0, 0, 0, 1000},
		{"no gas used", 1000, 0, 200_000, 1000, 0},
		{"one unit fee", 1, 1, 200_000, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shieldante.RefundAmount(uspark(tc.fee), tc.used, tc.limit)
			require.Equal(t, uspark(tc.refund).String(), got.String())
			require.True(t, got.AmountOf("uspark").LTE(math.NewInt(tc.fee)), "refund never exceeds what was paid")
			// exact charge, before rounding, never exceeds the rounded one
			if tc.limit > 0 && tc.used < tc.limit {
				exact := math.LegacyNewDec(tc.fee).MulInt64(int64(tc.used)).QuoInt64(int64(tc.limit))
				require.True(t, math.LegacyNewDec(tc.charged).GTE(exact))
			}
		})
	}
	require.True(t, shieldante.RefundAmount(sdk.NewCoins(), 1, 10).IsZero())
}

// TestFeeRefund (SHD-102): the ante charges the declared fee in full; the
// post handler returns the part gas used did not cost from the fee collector
// to the module and unbooks it from the epoch ledger and the identity's
// share, in the epoch the fee was charged.
func TestFeeRefund(t *testing.T) {
	var charged []math.Int
	var refunded []refundCall
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), charged: &charged, refunded: &refunded}
	bk := &mockBankKeeper{}
	exec := validShieldedExec()
	exec.RateLimitNullifier[31] = 7
	tx := mockTx{msgs: []sdk.Msg{exec}, fees: uspark(1000), gas: 200_000}

	ctx, err := runAnteAndPost(t, sk, bk, tx, sdk.ExecModeFinalize, false, true, 50_000)
	require.NoError(t, err)

	require.Equal(t, []math.Int{math.NewInt(1000)}, charged, "the ante books the declared fee")
	require.Len(t, bk.transfers, 2)
	require.Equal(t, moduleTransfer{from: shieldtypes.ModuleName, to: authtypes.FeeCollectorName, amt: uspark(1000)}, bk.transfers[0])
	// 50k of 200k gas used at 1000/200k: 250 stays with the fee collector
	require.Equal(t, moduleTransfer{from: authtypes.FeeCollectorName, to: shieldtypes.ModuleName, amt: uspark(750)}, bk.transfers[1])
	require.Equal(t, []refundCall{{epoch: 1, amount: math.NewInt(750), identity: hex.EncodeToString(exec.RateLimitNullifier)}}, refunded)

	events := ctx.EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, shieldtypes.EventTypeShieldFeeRefund, events[0].Type)
	require.Equal(t, "750uspark", events[0].Attributes[0].Value)
	require.Equal(t, "50000", events[0].Attributes[1].Value)
	require.Equal(t, "200000", events[0].Attributes[2].Value)
}

// TestFeeRefund_NothingToRefund: a tx that used all its gas, or paid no fee,
// gets nothing back and books nothing.
func TestFeeRefund_NothingToRefund(t *testing.T) {
	for name, tc := range map[string]struct {
		fee  sdk.Coins
		used uint64
	}{
		"all gas used":  {uspark(1000), 200_000},
		"zero fee":      {sdk.NewCoins(), 10},
		"one unit fee":  {uspark(1), 100_000},
		"almost no gas": {uspark(1000), 199_999},
	} {
		t.Run(name, func(t *testing.T) {
			var refunded []refundCall
			sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), refunded: &refunded}
			bk := &mockBankKeeper{}
			tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: tc.fee, gas: 200_000}
			ctx, err := runAnteAndPost(t, sk, bk, tx, sdk.ExecModeFinalize, false, true, tc.used)
			require.NoError(t, err)
			for _, tr := range bk.transfers {
				require.Equal(t, shieldtypes.ModuleName, tr.from, "no transfer out of the fee collector")
			}
			if name == "almost no gas" {
				// 1000 x 199999 / 200000 = 999.995 -> charged 1000, refund 0
				require.Len(t, bk.transfers, 1)
			}
			require.Empty(t, refunded)
			require.Empty(t, ctx.EventManager().Events())
		})
	}
}

// TestFeeRefund_FailedTxNotRefunded: baseapp discards a failed tx's post
// handler writes with its messages while the ante's fee payment is already
// committed, so a refund could never land; none is attempted, and the mark
// is consumed so it cannot leak to a later tx.
func TestFeeRefund_FailedTxNotRefunded(t *testing.T) {
	var refunded []refundCall
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), refunded: &refunded}
	bk := &mockBankKeeper{}
	tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: uspark(1000), gas: 200_000}
	ctx, err := runAnteAndPost(t, sk, bk, tx, sdk.ExecModeFinalize, false, false, 50_000)
	require.NoError(t, err)
	require.Len(t, bk.transfers, 1, "the ante's payment only")
	require.Empty(t, refunded)
	require.Empty(t, ctx.EventManager().Events())
}

// TestFeeRefund_OnlyFinalize: CheckTx, ReCheckTx and simulation neither mark
// nor refund (messages do not run there, and the check state is discarded).
func TestFeeRefund_OnlyFinalize(t *testing.T) {
	for name, tc := range map[string]struct {
		mode     sdk.ExecMode
		simulate bool
	}{
		"check":    {sdk.ExecModeCheck, false},
		"recheck":  {sdk.ExecModeReCheck, false},
		"simulate": {sdk.ExecModeFinalize, true},
	} {
		t.Run(name, func(t *testing.T) {
			var refunded []refundCall
			sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), refunded: &refunded}
			bk := &mockBankKeeper{}
			tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: uspark(1000), gas: 200_000}
			_, err := runAnteAndPost(t, sk, bk, tx, tc.mode, tc.simulate, true, 50_000)
			require.NoError(t, err)
			require.Len(t, bk.transfers, 1)
			require.Empty(t, refunded)
		})
	}
}

// TestFeeRefund_UnmarkedTxUntouched: a tx the shield ante did not charge
// (no MsgShieldedExec) is left alone by the post handler.
func TestFeeRefund_UnmarkedTxUntouched(t *testing.T) {
	sk := mockShieldKeeper{params: shieldtypes.DefaultParams()}
	bk := &mockBankKeeper{}
	post := shieldante.NewFeeRefundDecorator(sk, bk, shieldante.NewFeeMarks())
	ctx := finalizeCtx(t, 200_000)
	ctx.GasMeter().ConsumeGas(10, "x")
	_, err := post.PostHandle(ctx, mockTx{fees: uspark(1000)}, false, true, terminalPost)
	require.NoError(t, err)
	require.Empty(t, bk.transfers)
}

// TestFeeRefund_ErrorsNeverFailTheTx: a refund the bank or the ledger cannot
// take is logged; the exec stands, and a bank failure books nothing.
func TestFeeRefund_ErrorsNeverFailTheTx(t *testing.T) {
	t.Run("bank", func(t *testing.T) {
		var refunded []refundCall
		sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), refunded: &refunded}
		bk := &mockBankKeeper{}
		tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: uspark(1000), gas: 200_000}
		ctx := finalizeCtx(t, tx.GetGas())
		ante := shieldante.NewShieldGasDecorator(sk, bk)
		post := shieldante.NewFeeRefundDecorator(sk, bk, ante.FeeMarks())
		_, err := ante.AnteHandle(ctx, tx, false, terminalHandler)
		require.NoError(t, err)
		bk.sendErr = errors.New("fee collector empty")
		ctx, err = post.PostHandle(ctx.WithEventManager(sdk.NewEventManager()), tx, false, true, terminalPost)
		require.NoError(t, err)
		require.Empty(t, refunded, "nothing unbooked when no coins moved")
		require.Empty(t, ctx.EventManager().Events())
	})
	t.Run("ledger", func(t *testing.T) {
		sk := mockShieldKeeper{params: shieldtypes.DefaultParams(), refundErr: errors.New("store")}
		bk := &mockBankKeeper{}
		tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: uspark(1000), gas: 200_000}
		_, err := runAnteAndPost(t, sk, bk, tx, sdk.ExecModeFinalize, false, true, 50_000)
		require.NoError(t, err)
		require.Len(t, bk.transfers, 2, "the coins still moved")
	})
}

// TestFeeMarks: a mark is taken once, and marks from an earlier block are
// dropped (a tx whose ante failed after the mark never reaches the post
// handler).
func TestFeeMarks(t *testing.T) {
	m := shieldante.NewFeeMarks()
	ctx := finalizeCtx(t, 1)
	_, ok := m.Take(ctx)
	require.False(t, ok)

	ante := shieldante.NewShieldGasDecorator(mockShieldKeeper{params: shieldtypes.DefaultParams()}, &mockBankKeeper{})
	tx := mockTx{msgs: []sdk.Msg{validShieldedExec()}, fees: uspark(1000), gas: 200_000}
	_, err := ante.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)
	c, ok := ante.FeeMarks().Take(ctx)
	require.True(t, ok)
	require.Equal(t, shieldante.FeeCharge{Fee: uspark(1000), GasLimit: 200_000, Epoch: 1, Identity: hex.EncodeToString(make([]byte, 32))}, c)
	_, ok = ante.FeeMarks().Take(ctx)
	require.False(t, ok, "taken once")

	_, err = ante.AnteHandle(ctx, tx, false, terminalHandler)
	require.NoError(t, err)
	_, ok = ante.FeeMarks().Take(ctx.WithBlockHeight(11))
	require.False(t, ok, "a new block drops the old block's marks")
}
