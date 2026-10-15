package simulation

import (
	"math/rand"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgClaimUnbondedBond archives a random UNBONDING operator
// whose unbond_complete_at has elapsed.
func SimulateMsgClaimUnbondedBond(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgClaimUnbondedBond{}

		// Partial-withdrawal branch (§3.5a): half the time, look for an
		// ACTIVE operator whose queued withdrawal has matured and release
		// min(pending, bond - min_bond), leaving the record live. Every
		// handler precondition that the direct write depends on is
		// mirrored here so the state stays invariant-clean.
		if r.Intn(2) == 0 {
			if op, found := findOperatorWithStatus(r, ctx, k, types.OperatorStatus_OPERATOR_STATUS_ACTIVE); found &&
				!op.PendingWithdrawAmount.IsNil() && op.PendingWithdrawAmount.IsPositive() &&
				ctx.BlockHeight() >= op.WithdrawCompleteAt {
				cfg, err := k.ServiceTypes.Get(ctx, op.ServiceType)
				if err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "missing service-type config"), nil, nil
				}
				release := op.PendingWithdrawAmount
				if spare := op.BondAmount.Sub(cfg.MinBondAmount); release.GT(spare) {
					release = spare
				}
				if !release.IsPositive() {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "nothing releasable above min_bond"), nil, nil
				}
				msg.Operator = op.Address
				msg.ServiceType = op.ServiceType
				op.BondAmount = op.BondAmount.Sub(release)
				remaining := op.PendingWithdrawAmount.Sub(release)
				if remaining.IsPositive() {
					op.PendingWithdrawAmount = remaining
				} else {
					op.PendingWithdrawAmount = sdkmath.ZeroInt()
					op.WithdrawCompleteAt = 0
				}
				if err := k.PutOperator(ctx, op); err != nil {
					return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update operator"), nil, nil
				}
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (partial withdrawal, direct keeper call)"), nil, nil
			}
		}

		op, found := findOperatorWithStatus(r, ctx, k, types.OperatorStatus_OPERATOR_STATUS_UNBONDING)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no unbonding operator"), nil, nil
		}
		if ctx.BlockHeight() < op.UnbondCompleteAt {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "unbonding period not elapsed"), nil, nil
		}
		msg.Operator = op.Address
		msg.ServiceType = op.ServiceType

		op.BondAmount = sdkmath.ZeroInt()
		op.Status = types.OperatorStatus_OPERATOR_STATUS_RETIRED
		op.RetiredAt = ctx.BlockHeight()
		if err := k.ArchiveOperator(ctx, op); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to archive operator"), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}
