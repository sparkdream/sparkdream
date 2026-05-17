package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/service/types"
)

// RegisterInvariants registers all x/service module invariants. See
// x-service-spec.md §13 for the full list. Each invariant is consulted
// by the crisis module (`simd q crisis invariant service/<name>`) and
// by simulation.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "bond-pool-accounting", BondPoolAccountingInvariant(k))
	ir.RegisterRoute(types.ModuleName, "live-archive-disjoint", LiveArchiveDisjointInvariant(k))
	ir.RegisterRoute(types.ModuleName, "controller-index-consistency", ControllerIndexConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "service-type-index-consistency", ServiceTypeIndexConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "underfunded-queue-consistency", UnderfundedQueueConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "pending-report-queue-consistency", PendingReportQueueConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "escalated-report-queue-consistency", EscalatedReportQueueConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "tier1-escrow-queue-consistency", Tier1EscrowQueueConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "report-state-machine-sanity", ReportStateMachineSanityInvariant(k))
}

// BondPoolAccountingInvariant — bank.balance(module, uspark) ==
// sum(operators[].bond) + sum(reports{PENDING|ESCALATED}.deposit) +
// sum(controller_transfer_cases[].deposit) + sum(tier1_escrow[].amount).
// (§13 + §3.4.7 four-pool decomposition.)
func BondPoolAccountingInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		bondSum := sdkmath.ZeroInt()
		err := k.Operators.Walk(ctx, nil, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
			if !op.Bond.Amount.IsNil() {
				bondSum = bondSum.Add(op.Bond.Amount)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("bond-pool-accounting", err), true
		}

		depositSum := sdkmath.ZeroInt()
		err = k.Reports.Walk(ctx, nil, func(_ uint64, r types.Report) (bool, error) {
			if r.Status == types.ReportStatus_REPORT_STATUS_PENDING ||
				r.Status == types.ReportStatus_REPORT_STATUS_ESCALATED {
				if !r.Deposit.Amount.IsNil() {
					depositSum = depositSum.Add(r.Deposit.Amount)
				}
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("bond-pool-accounting", err), true
		}

		caseDepositSum := sdkmath.ZeroInt()
		err = k.ControllerTransferCases.Walk(ctx, nil, func(_ uint64, c types.ControllerTransferCase) (bool, error) {
			if !c.Deposit.Amount.IsNil() {
				caseDepositSum = caseDepositSum.Add(c.Deposit.Amount)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("bond-pool-accounting", err), true
		}

		escrowSum := sdkmath.ZeroInt()
		err = k.Tier1Escrow.Walk(ctx, nil, func(_ uint64, e types.Tier1EscrowEntry) (bool, error) {
			if !e.Amount.Amount.IsNil() {
				escrowSum = escrowSum.Add(e.Amount.Amount)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("bond-pool-accounting", err), true
		}

		expected := bondSum.Add(depositSum).Add(caseDepositSum).Add(escrowSum)
		moduleAddr := k.bankModuleAddress()
		actual := k.bankKeeper.GetBalance(ctx, moduleAddr, types.BondDenom).Amount

		if !expected.Equal(actual) {
			return sdk.FormatInvariant(types.ModuleName, "bond-pool-accounting",
				fmt.Sprintf("module account balance %s != expected %s (bond=%s deposits=%s case_deposits=%s escrow=%s)",
					actual, expected, bondSum, depositSum, caseDepositSum, escrowSum)), true
		}
		return sdk.FormatInvariant(types.ModuleName, "bond-pool-accounting", "OK"), false
	}
}

// LiveArchiveDisjointInvariant — for every (address, service_type) pair
// there is at most one live record AND no archived record with
// retired_at >= the live record's registered_at.
func LiveArchiveDisjointInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.Operators.Walk(ctx, nil, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
			addrBytes, err := k.addrBytes(op.Address)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  live record %s/%s has invalid address\n", op.Address, op.ServiceType)
				return false, nil
			}
			// Iterate the archived store for this (addr, service_type) and
			// reject if any archived record has retired_at > op.registered_at.
			rng := collections.NewPrefixedTripleRange[[]byte, string, int64](addrBytes)
			iter, err := k.ArchivedOperators.Iterate(ctx, rng)
			if err != nil {
				return true, err
			}
			defer iter.Close()
			for ; iter.Valid(); iter.Next() {
				key, err := iter.Key()
				if err != nil {
					return true, err
				}
				if key.K2() != op.ServiceType {
					continue
				}
				if key.K3() >= op.RegisteredAt {
					broken++
					msg += fmt.Sprintf("  archived %s/%s@%d collides with live (registered_at=%d)\n",
						op.Address, op.ServiceType, key.K3(), op.RegisteredAt)
				}
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("live-archive-disjoint", err), true
		}

		return sdk.FormatInvariant(types.ModuleName, "live-archive-disjoint",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// ControllerIndexConsistencyInvariant — every OperatorsByController
// entry resolves to an Operators record with matching controller field.
func ControllerIndexConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string
		err := k.OperatorsByController.Walk(ctx, nil, func(key collections.Triple[[]byte, []byte, string]) (bool, error) {
			op, err := k.Operators.Get(ctx, collections.Join(key.K2(), key.K3()))
			if err != nil {
				broken++
				msg += fmt.Sprintf("  controller-index entry has no live operator (svc=%s)\n", key.K3())
				return false, nil
			}
			ctlBytes, _ := k.addrBytes(op.Controller)
			if string(ctlBytes) != string(key.K1()) {
				broken++
				msg += fmt.Sprintf("  controller-index mismatch for %s/%s\n", op.Address, op.ServiceType)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("controller-index-consistency", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "controller-index-consistency",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// ServiceTypeIndexConsistencyInvariant — every OperatorsByServiceType
// entry resolves to an Operators record with matching service_type.
func ServiceTypeIndexConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string
		err := k.OperatorsByServiceType.Walk(ctx, nil, func(key collections.Pair[string, []byte]) (bool, error) {
			op, err := k.Operators.Get(ctx, collections.Join(key.K2(), key.K1()))
			if err != nil {
				broken++
				msg += fmt.Sprintf("  service-type-index entry has no live operator (svc=%s)\n", key.K1())
				return false, nil
			}
			if op.ServiceType != key.K1() {
				broken++
				msg += fmt.Sprintf("  service-type-index mismatch for %s/%s\n", op.Address, op.ServiceType)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("service-type-index-consistency", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "service-type-index-consistency",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// UnderfundedQueueConsistencyInvariant — UnderfundedQueue ↔ UNDERFUNDED
// operators (bijection).
func UnderfundedQueueConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		// Every queue entry resolves to an UNDERFUNDED operator.
		err := k.UnderfundedQueue.Walk(ctx, nil, func(key collections.Triple[int64, []byte, string]) (bool, error) {
			op, err := k.Operators.Get(ctx, collections.Join(key.K2(), key.K3()))
			if err != nil || op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
				broken++
				msg += fmt.Sprintf("  queue entry (ts=%d, svc=%s) is not UNDERFUNDED in live store\n", key.K1(), key.K3())
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("underfunded-queue-consistency", err), true
		}

		// Every UNDERFUNDED operator has a queue entry.
		err = k.Operators.Walk(ctx, nil, func(_ collections.Pair[[]byte, string], op types.Operator) (bool, error) {
			if op.Status != types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED {
				return false, nil
			}
			addrBytes, _ := k.addrBytes(op.Address)
			has, _ := k.UnderfundedQueue.Has(ctx, collections.Join3(op.UnderfundedSince, addrBytes, op.ServiceType))
			if !has {
				broken++
				msg += fmt.Sprintf("  UNDERFUNDED %s/%s missing queue entry\n", op.Address, op.ServiceType)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("underfunded-queue-consistency", err), true
		}

		return sdk.FormatInvariant(types.ModuleName, "underfunded-queue-consistency",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// PendingReportQueueConsistencyInvariant — PendingReportsQueue ↔ PENDING reports.
func PendingReportQueueConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.PendingReportsQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			r, err := k.Reports.Get(ctx, key.K2())
			if err != nil || r.Status != types.ReportStatus_REPORT_STATUS_PENDING {
				broken++
				msg += fmt.Sprintf("  pending-queue entry report %d is not PENDING\n", key.K2())
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("pending-report-queue-consistency", err), true
		}

		err = k.Reports.Walk(ctx, nil, func(_ uint64, r types.Report) (bool, error) {
			if r.Status != types.ReportStatus_REPORT_STATUS_PENDING {
				return false, nil
			}
			has, _ := k.PendingReportsQueue.Has(ctx, collections.Join(r.FiledAt, r.ReportId))
			if !has {
				broken++
				msg += fmt.Sprintf("  PENDING report %d missing queue entry\n", r.ReportId)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("pending-report-queue-consistency", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "pending-report-queue-consistency",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// EscalatedReportQueueConsistencyInvariant — EscalatedReportsQueue ↔ ESCALATED reports.
func EscalatedReportQueueConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.EscalatedReportsQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			r, err := k.Reports.Get(ctx, key.K2())
			if err != nil || r.Status != types.ReportStatus_REPORT_STATUS_ESCALATED {
				broken++
				msg += fmt.Sprintf("  escalated-queue entry report %d is not ESCALATED\n", key.K2())
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("escalated-report-queue-consistency", err), true
		}

		err = k.Reports.Walk(ctx, nil, func(_ uint64, r types.Report) (bool, error) {
			if r.Status != types.ReportStatus_REPORT_STATUS_ESCALATED {
				return false, nil
			}
			if r.EscalatedAt > 0 {
				has, _ := k.EscalatedReportsQueue.Has(ctx, collections.Join(r.EscalatedAt, r.ReportId))
				if !has {
					broken++
					msg += fmt.Sprintf("  ESCALATED report %d missing queue entry\n", r.ReportId)
				}
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("escalated-report-queue-consistency", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "escalated-report-queue-consistency",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// Tier1EscrowQueueConsistencyInvariant — every Tier1EscrowReleaseQueue
// entry resolves to a Tier1Escrow row.
func Tier1EscrowQueueConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string
		err := k.Tier1EscrowReleaseQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			if _, err := k.Tier1Escrow.Get(ctx, key.K2()); err != nil {
				broken++
				msg += fmt.Sprintf("  release-queue entry escrow %d has no Tier1Escrow row\n", key.K2())
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("tier1-escrow-queue-consistency", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "tier1-escrow-queue-consistency",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// ReportStateMachineSanityInvariant — every Report has a documented
// status; ESCALATED requires jury_case_id != 0 AND escalated_at != 0
// (with the standalone-mode caveat that jury_case_id may be a placeholder);
// RESOLVED_T2 requires slash_amount set.
func ReportStateMachineSanityInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.Reports.Walk(ctx, nil, func(_ uint64, r types.Report) (bool, error) {
			switch r.Status {
			case types.ReportStatus_REPORT_STATUS_PENDING,
				types.ReportStatus_REPORT_STATUS_RESOLVED_T1,
				types.ReportStatus_REPORT_STATUS_ESCALATED,
				types.ReportStatus_REPORT_STATUS_RESOLVED_T2,
				types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED,
				types.ReportStatus_REPORT_STATUS_AUTO_TIMEOUT,
				types.ReportStatus_REPORT_STATUS_CLOSED_OPERATOR_DISSOLVED:
				// OK.
			default:
				broken++
				msg += fmt.Sprintf("  report %d has invalid status %s\n", r.ReportId, r.Status)
			}

			if r.Status == types.ReportStatus_REPORT_STATUS_ESCALATED && r.EscalatedAt == 0 {
				broken++
				msg += fmt.Sprintf("  ESCALATED report %d has zero escalated_at\n", r.ReportId)
			}
			if r.Status == types.ReportStatus_REPORT_STATUS_RESOLVED_T2 && r.SlashAmount.Amount.IsNil() {
				broken++
				msg += fmt.Sprintf("  RESOLVED_T2 report %d has nil slash_amount\n", r.ReportId)
			}
			return false, nil
		})
		if err != nil {
			return invariantErr("report-state-machine-sanity", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "report-state-machine-sanity",
			fmt.Sprintf("found %d violations\n%s", broken, msg)), broken > 0
	}
}

// invariantErr is a helper for formatting an invariant-aborting error.
func invariantErr(name string, err error) string {
	return sdk.FormatInvariant(types.ModuleName, name,
		fmt.Sprintf("error: %v", err))
}
