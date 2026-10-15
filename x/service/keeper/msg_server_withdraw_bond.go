package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// WithdrawBond queues a partial withdrawal of an ACTIVE operator's bond
// without leaving the service (§3.5a). The queued SPARK stays in the
// module bond pool, remains slashable, and is released by
// MsgClaimUnbondedBond once withdraw_complete_at has passed and no report
// or tier-1 escrow is open.
//
// Gates:
//   - status must be ACTIVE (UNDERFUNDED has nothing to spare; UNBONDING is
//     already returning the whole bond on the exit clock);
//   - bond - pending_withdraw - amount >= min_bond, so the operator can
//     never push itself into UNDERFUNDED through its own withdrawal.
//
// Calls are incremental: a second call adds to pending_withdraw_amount and
// resets the single withdraw_complete_at clock to now + unbonding period,
// so the lock only ever lengthens. MUST be signed by `operator` directly
// (listed in NonDelegableMsgTypeURLs).
func (k msgServer) WithdrawBond(ctx context.Context, msg *types.MsgWithdrawBond) (*types.MsgWithdrawBondResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}
	if msg.Amount.IsNil() || !msg.Amount.IsPositive() {
		return nil, types.ErrInsufficientBond.Wrap("amount must be a positive Int")
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}
	if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
		return nil, types.ErrOperatorUnbonding
	}
	if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE {
		return nil, types.ErrOperatorNotActive.Wrap(op.Status.String())
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
	if err != nil {
		return nil, err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	pending := pendingWithdraw(op)
	available := op.BondAmount.Sub(cfg.MinBondAmount).Sub(pending)
	if available.IsNegative() {
		available = sdkmath.ZeroInt()
	}
	if msg.Amount.GT(available) {
		return nil, types.ErrWithdrawExceedsAvailable.Wrapf(
			"bond=%s min_bond=%s already_pending=%s available=%s requested=%s",
			op.BondAmount, cfg.MinBondAmount, pending, available, msg.Amount)
	}

	// No bond or status change here, so no settle: the SPARK stays bonded
	// and keeps accruing bond-blocks until it is actually released.
	op.PendingWithdrawAmount = pending.Add(msg.Amount)
	op.WithdrawCompleteAt = sdkCtx.BlockHeight() + k.effectiveUnbondingPeriodBlocks(cfg, params)
	if err := k.PutOperator(ctx, op); err != nil {
		return nil, err
	}

	bondDenom := k.BondDenom(ctx)
	sdkCtx.EventManager().EmitEvent(types.NewOperatorWithdrawQueuedEvent(
		msg.Operator, msg.ServiceType,
		sdk.NewCoin(bondDenom, msg.Amount),
		sdk.NewCoin(bondDenom, op.PendingWithdrawAmount),
		op.WithdrawCompleteAt,
	))

	return &types.MsgWithdrawBondResponse{}, nil
}

// claimPartialWithdrawal releases a matured partial withdrawal (§3.5a) for
// an operator that is NOT unbonding. Shares the exit claim's gates: clock
// elapsed, no open reports, no active tier-1 escrow (expired escrows are
// eagerly released first). The release is capped at bond - min_bond so a
// slash taken during the wait can never drain the operator below the
// floor; whatever cannot be released yet stays queued for a later claim.
func (k msgServer) claimPartialWithdrawal(ctx context.Context, op types.Operator, opBytes []byte) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	pending := pendingWithdraw(op)
	if !pending.IsPositive() {
		return types.ErrNoPendingWithdrawal
	}
	if currentHeight < op.WithdrawCompleteAt {
		return types.ErrUnbondingPeriodNotElapsed.Wrapf("withdraw_complete_at=%d height=%d", op.WithdrawCompleteAt, currentHeight)
	}
	if op.Status != types.OperatorStatus_OPERATOR_STATUS_ACTIVE {
		// UNDERFUNDED: bond is already below min_bond, nothing is spare.
		return types.ErrOperatorNotActive.Wrapf("%s: top up before claiming a partial withdrawal", op.Status.String())
	}

	hasOpen, err := k.HasOpenReports(ctx, opBytes, op.ServiceType)
	if err != nil {
		return err
	}
	if hasOpen {
		return types.ErrOpenReports
	}
	hasActive, err := k.HasActiveTier1Escrow(ctx, opBytes, op.ServiceType, currentHeight)
	if err != nil {
		return err
	}
	if hasActive {
		return types.ErrEscrowStillActive
	}
	if err := k.eagerReleaseExpiredEscrowsForOperator(ctx, opBytes, op.ServiceType, currentHeight); err != nil {
		return err
	}

	cfg, err := k.resolveServiceTypeConfig(ctx, op.ServiceType)
	if err != nil {
		return err
	}
	release := pending
	if spare := op.BondAmount.Sub(cfg.MinBondAmount); release.GT(spare) {
		release = spare
	}
	if !release.IsPositive() {
		return types.ErrWithdrawExceedsAvailable.Wrapf(
			"bond=%s min_bond=%s: nothing releasable until the bond is topped up", op.BondAmount, cfg.MinBondAmount)
	}

	// Settle BEFORE the bond change so accrual to date uses the old bond.
	k.settleBondBlocks(&op, currentHeight)

	bondDenom := k.BondDenom(ctx)
	withdrawn := sdk.NewCoin(bondDenom, release)
	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(opBytes), sdk.NewCoins(withdrawn)); err != nil {
		return err
	}

	op.BondAmount = op.BondAmount.Sub(release)
	remaining := pending.Sub(release)
	if remaining.IsPositive() {
		// Capped by a mid-wait slash: keep the remainder queued on the same
		// clock so a later claim (after a top-up) finishes the withdrawal.
		op.PendingWithdrawAmount = remaining
	} else {
		clearPendingWithdrawal(&op)
	}
	if err := k.PutOperator(ctx, op); err != nil {
		return err
	}

	sdkCtx.EventManager().EmitEvent(types.NewOperatorBondWithdrawnEvent(
		op.Address, op.ServiceType, withdrawn, sdk.NewCoin(bondDenom, op.BondAmount),
	))
	return nil
}
