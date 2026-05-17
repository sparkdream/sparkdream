package simulation

// Sim helpers follow the x/forum simulation pattern: each top-level
// SimulateMsgX returns a simtypes.Operation that picks a random account
// and mutates state directly through the keeper's collections rather
// than the msg-server. This bypasses controller-group / bond-escrow /
// trust-level / rate-limit / signing preconditions that would otherwise
// require a much larger sim genesis. End-to-end behaviour is covered by
// the shell test suite at test/service/.

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// simServiceType is the placeholder service-type every service sim
// uses. ensureServiceType creates it on first call so subsequent sim
// ops have something to register against.
const simServiceType = "sim-service"

// ensureServiceType inserts a permissive ServiceTypeConfig under
// simServiceType if none exists. The defaults mirror the test-akash
// shape used by the e2e suite (small min_bond, short windows).
func ensureServiceType(ctx sdk.Context, k keeper.Keeper) (types.ServiceTypeConfig, error) {
	cfg, err := k.ServiceTypes.Get(ctx, simServiceType)
	if err == nil {
		return cfg, nil
	}
	cfg = types.ServiceTypeConfig{
		ServiceType:            simServiceType,
		Description:            "sim",
		MinBond:                sdk.NewCoin(types.BondDenom, math.NewInt(1_000_000)),
		UnbondingPeriodBlocks:  20,
		UnilateralSlashCapBps:  500,
		Tier1WindowBlocks:      1000,
		Tier1AggregateCapBps:   1500,
		Tier1CooldownBlocks:    10,
		UnderfundedGraceBlocks: 10,
		Enabled:                true,
	}
	if err := k.ServiceTypes.Set(ctx, simServiceType, cfg); err != nil {
		return types.ServiceTypeConfig{}, err
	}
	return cfg, nil
}

// findRandomOperator returns a random live operator from state, or
// (zero, false) if none exist.
func findRandomOperator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Operator, bool) {
	var collected []types.Operator
	_ = k.Operators.Walk(ctx, nil, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
		collected = append(collected, op)
		return false, nil
	})
	if len(collected) == 0 {
		return types.Operator{}, false
	}
	return collected[r.Intn(len(collected))], true
}

// findOperatorWithStatus returns a random live operator with the given
// status, or (zero, false) if none match.
func findOperatorWithStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.OperatorStatus) (types.Operator, bool) {
	var collected []types.Operator
	_ = k.Operators.Walk(ctx, nil, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
		if op.Status == status {
			collected = append(collected, op)
		}
		return false, nil
	})
	if len(collected) == 0 {
		return types.Operator{}, false
	}
	return collected[r.Intn(len(collected))], true
}

// findReportWithStatus returns a random report row matching `status`,
// or (zero, 0, false) if none.
func findReportWithStatus(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.ReportStatus) (types.Report, uint64, bool) {
	type entry struct {
		id uint64
		r  types.Report
	}
	var collected []entry
	_ = k.Reports.Walk(ctx, nil, func(id uint64, rep types.Report) (bool, error) {
		if rep.Status == status {
			collected = append(collected, entry{id, rep})
		}
		return false, nil
	})
	if len(collected) == 0 {
		return types.Report{}, 0, false
	}
	pick := collected[r.Intn(len(collected))]
	return pick.r, pick.id, true
}

// findControllerTransferCase returns a random open controller-transfer
// case, or (zero, 0, false) if none.
func findControllerTransferCase(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.ControllerTransferCase, uint64, bool) {
	type entry struct {
		id   uint64
		case_ types.ControllerTransferCase
	}
	var collected []entry
	_ = k.ControllerTransferCases.Walk(ctx, nil, func(id uint64, c types.ControllerTransferCase) (bool, error) {
		collected = append(collected, entry{id, c})
		return false, nil
	})
	if len(collected) == 0 {
		return types.ControllerTransferCase{}, 0, false
	}
	pick := collected[r.Intn(len(collected))]
	return pick.case_, pick.id, true
}
