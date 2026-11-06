package ante

import (
	"crypto/sha256"
	"strconv"
	"sync"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	shieldtypes "sparkdream/x/shield/types"
)

// FeeCharge is what ShieldGasDecorator made the module pay for one tx: the
// declared fee, the declared gas limit the fee was priced against, and the
// epoch and identity (rate-limit nullifier hex) the fee was booked under.
type FeeCharge struct {
	Fee      sdk.Coins
	GasLimit uint64
	Epoch    uint64
	Identity string
}

// FeeMarks carries a FeeCharge from the ante decorator to the post handler,
// keyed by the tx bytes' hash within the block being executed. The same
// shape as app/relayrefund's Marks, for the same reason: the ante chain's
// returned context does not reliably reach the post handler (a decorator
// that hands a zero sdk.Context to next makes baseapp fall back to its
// pre-ante context). Block execution is sequential and identical on every
// node, so an in-memory map is deterministic; a node that restarts mid-block
// replays the block and rebuilds it. Only finalize-mode txs are marked.
type FeeMarks struct {
	mu     sync.Mutex
	height int64
	set    map[[32]byte]FeeCharge
}

// NewFeeMarks returns an empty FeeMarks.
func NewFeeMarks() *FeeMarks { return &FeeMarks{set: map[[32]byte]FeeCharge{}} }

func (m *FeeMarks) mark(ctx sdk.Context, c FeeCharge) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rollover(ctx)
	m.set[sha256.Sum256(ctx.TxBytes())] = c
}

// Take returns the charge recorded for the tx being executed, if any, and
// forgets it.
func (m *FeeMarks) Take(ctx sdk.Context) (FeeCharge, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rollover(ctx)
	key := sha256.Sum256(ctx.TxBytes())
	c, ok := m.set[key]
	delete(m.set, key)
	return c, ok
}

// rollover drops marks left by an earlier block (a tx whose ante failed
// after the mark never reaches the post handler).
func (m *FeeMarks) rollover(ctx sdk.Context) {
	if ctx.BlockHeight() != m.height {
		m.height = ctx.BlockHeight()
		m.set = map[[32]byte]FeeCharge{}
	}
}

// RefundAmount is the part of a declared fee that gas actually used did not
// cost, at the price the submitter implied (fee / gasLimit): per denom,
// fee - ceil(fee x gasUsed / gasLimit). The charged part rounds up, so the
// fee collector never gets less than gas used x price, and the refund is
// zero once gas used reaches the limit (or the limit is zero).
func RefundAmount(fee sdk.Coins, gasUsed, gasLimit uint64) sdk.Coins {
	if gasLimit == 0 || gasUsed >= gasLimit {
		return sdk.NewCoins()
	}
	used := math.NewIntFromUint64(gasUsed)
	limit := math.NewIntFromUint64(gasLimit)
	out := sdk.NewCoins()
	for _, c := range fee {
		// ceil(amount x used / limit), all non-negative
		charged := c.Amount.Mul(used).Add(limit.SubRaw(1)).Quo(limit)
		if refund := c.Amount.Sub(charged); refund.IsPositive() {
			out = out.Add(sdk.NewCoin(c.Denom, refund))
		}
	}
	return out
}

// FeeRefundDecorator is the post handler that returns the unused part of a
// MsgShieldedExec's fee (see RefundAmount) from the fee collector to the
// shield module account and unbooks it from the epoch fee budget and the
// identity's share, so both measure the gas anonymous execs actually used
// rather than the gas limits their submitters declared.
//
// baseapp runs the post handler for a failed tx as well, but in the message
// state, which it discards along with the failed messages, while the ante
// state (the fee payment and the booking) is already committed. A refund
// for a failed exec therefore cannot land, and none is attempted: a failing
// exec costs the reserve its declared fee, as it costs its member the
// nullifier and the rate-limit slot (SHD-03), bounded by max_fee_per_exec
// and the identity's share of the epoch budget.
//
// A refund that cannot be paid is logged and the tx stands: the module is
// out the unused part, the member's exec is not undone. The refund's own
// writes run under an infinite gas meter, so a tx that used nearly all of
// its gas is not failed by its own refund; they are the module's
// bookkeeping (one transfer, two collection writes), not the member's
// operation.
type FeeRefundDecorator struct {
	shieldKeeper ShieldKeeper
	bankKeeper   BankKeeper
	marks        *FeeMarks
}

// NewFeeRefundDecorator pairs the post handler with the FeeMarks of the
// ShieldGasDecorator whose charges it refunds.
func NewFeeRefundDecorator(sk ShieldKeeper, bk BankKeeper, marks *FeeMarks) FeeRefundDecorator {
	return FeeRefundDecorator{shieldKeeper: sk, bankKeeper: bk, marks: marks}
}

func (d FeeRefundDecorator) PostHandle(ctx sdk.Context, tx sdk.Tx, simulate, success bool, next sdk.PostHandler) (sdk.Context, error) {
	if simulate || ctx.ExecMode() != sdk.ExecModeFinalize || d.marks == nil {
		return next(ctx, tx, simulate, success)
	}
	charge, marked := d.marks.Take(ctx)
	if !marked || !success {
		return next(ctx, tx, simulate, success)
	}
	// the tx gas meter: ante and message gas, before this refund's own work
	gasUsed := ctx.GasMeter().GasConsumed()
	refund := RefundAmount(charge.Fee, gasUsed, charge.GasLimit)
	if refund.IsZero() {
		return next(ctx, tx, simulate, success)
	}
	rctx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	if err := d.bankKeeper.SendCoinsFromModuleToModule(rctx, authtypes.FeeCollectorName, shieldtypes.ModuleName, refund); err != nil {
		ctx.Logger().Error("shield fee refund failed", "refund", refund.String(), "gas_used", gasUsed, "gas_limit", charge.GasLimit, "err", err)
		return next(ctx, tx, simulate, success)
	}
	bondDenom := d.shieldKeeper.BondDenom(rctx)
	if err := d.shieldKeeper.RefundEpochFee(rctx, charge.Epoch, refund.AmountOf(bondDenom), charge.Identity); err != nil {
		ctx.Logger().Error("shield fee refund not unbooked", "refund", refund.String(), "epoch", charge.Epoch, "err", err)
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		shieldtypes.EventTypeShieldFeeRefund,
		sdk.NewAttribute(shieldtypes.AttributeKeyAmount, refund.String()),
		sdk.NewAttribute(shieldtypes.AttributeKeyGasUsed, strconv.FormatUint(gasUsed, 10)),
		sdk.NewAttribute(shieldtypes.AttributeKeyGasLimit, strconv.FormatUint(charge.GasLimit, 10)),
	))
	return next(ctx, tx, simulate, success)
}
