package keeper

import (
	"context"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// ClaimUnbondedBond returns the bond to the operator and moves the
// record to ArchivedOperators with status=RETIRED. See §3.5 claim
// eligibility.
//
// All §3.5 gates are enforced here: open-reports gate (bullet 3),
// active-tier-1-escrow gate (bullet 4), and eager-release of expired-
// but-unswept escrows so a claimant doesn't have to wait for the
// EndBlocker sweep. The §6.6 reputation grant runs after the bond
// return and is best-effort (its failure does not block the bond
// return — see grantReputationOnClaim below).
func (k msgServer) ClaimUnbondedBond(ctx context.Context, msg *types.MsgClaimUnbondedBond) (*types.MsgClaimUnbondedBondResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	opBytes, err := k.addrBytes(msg.Operator)
	if err != nil {
		return nil, types.ErrInvalidSigner.Wrap("invalid operator address")
	}

	op, exists := k.GetOperator(ctx, opBytes, msg.ServiceType)
	if !exists {
		return nil, types.ErrOperatorNotFound.Wrapf("%s / %s", msg.Operator, msg.ServiceType)
	}
	if op.Status != types.OperatorStatus_OPERATOR_STATUS_UNBONDING {
		return nil, types.ErrOperatorNotActive.Wrap("operator is not unbonding")
	}

	currentHeight := sdkCtx.BlockHeight()
	if currentHeight < op.UnbondCompleteAt {
		return nil, types.ErrUnbondingPeriodNotElapsed.Wrapf("complete_at=%d height=%d", op.UnbondCompleteAt, currentHeight)
	}

	// Open-reports gate (§3.5).
	hasOpen, err := k.HasOpenReports(ctx, opBytes, msg.ServiceType)
	if err != nil {
		return nil, err
	}
	if hasOpen {
		return nil, types.ErrOpenReports
	}

	// Active tier-1 escrow gate (§3.5). Genuinely-still-in-window
	// entries block; expired-but-unswept entries are eagerly processed
	// inline below.
	hasActive, err := k.HasActiveTier1Escrow(ctx, opBytes, msg.ServiceType, currentHeight)
	if err != nil {
		return nil, err
	}
	if hasActive {
		return nil, types.ErrEscrowStillActive
	}

	// Eagerly release any expired-but-unswept escrows for this operator
	// to the community pool (§3.5). The EndBlocker sweep will eventually
	// do this anyway, but the operator shouldn't have to wait if the
	// queue is saturated.
	if err := k.eagerReleaseExpiredEscrowsForOperator(ctx, opBytes, msg.ServiceType, currentHeight); err != nil {
		return nil, err
	}

	// Final settle (no-op for UNBONDING — accrual already capped, but
	// keeps timestamp current).
	k.settleBondBlocks(&op, currentHeight)

	// Return bond to operator wallet.
	bondDenom := k.BondDenom(ctx)
	bondCoin := sdk.NewCoin(bondDenom, op.BondAmount)
	if !op.BondAmount.IsZero() {
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, sdk.AccAddress(opBytes), sdk.NewCoins(bondCoin)); err != nil {
			return nil, err
		}
	}

	// Reputation grant per §6.6. Compute the operator's
	// effective_bond_blocks under the anti-gaming cap (max single
	// record's bond-blocks, NOT sum across records), then write through
	// to x/rep's service-operator tag. Best-effort: the bond return is
	// the load-bearing action; if rep wiring is missing or returns an
	// error, log and proceed rather than trapping the operator's bond.
	if err := k.grantReputationOnClaim(ctx, op, opBytes); err != nil {
		sdkCtx.Logger().Warn(
			"x/service: reputation grant on claim failed (non-fatal)",
			"operator", msg.Operator,
			"service_type", msg.ServiceType,
			"error", err,
		)
	}

	// Capture returned amount before zeroing for the event.
	returnedBond := bondCoin

	// Zero bond + flip to RETIRED + archive.
	op.BondAmount = sdkmath.ZeroInt()
	op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
	op.RetiredAt = currentHeight
	if err := k.ArchiveOperator(ctx, op); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvents(sdk.Events{
		types.NewOperatorUnbondCompletedEvent(msg.Operator, msg.ServiceType, returnedBond),
		types.NewOperatorArchivedEvent(msg.Operator, msg.ServiceType, "RETIRED", currentHeight),
	})

	if k.hooks() != nil {
		k.hooks().AfterOperatorRetired(ctx, sdk.AccAddress(opBytes), msg.ServiceType)
	}

	return &types.MsgClaimUnbondedBondResponse{}, nil
}
