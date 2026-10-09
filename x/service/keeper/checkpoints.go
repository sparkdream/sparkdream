package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/service/types"
)

// Checkpoints and liveness (docs/content-scanning.md §8.2).
//
// An operator records how far its off-chain work has progressed with
// MsgSubmitCheckpoint. When its service type sets checkpoint_max_lag_blocks,
// every tracked operator has exactly one CheckpointDeadlines entry: the
// block by which its checkpoint height must have advanced. The EndBlocker
// liveness sweep files a system report (caller = x/service itself) against
// an ACTIVE or UNDERFUNDED operator whose deadline passes, then reschedules
// it one full lag window later, so a worker that stays down is reported
// once per window, never once per block.

// GetCheckpoint returns the operator's latest checkpoint under serviceType.
func (k Keeper) GetCheckpoint(ctx context.Context, opBytes []byte, serviceType string) (types.Checkpoint, bool) {
	cp, err := k.Checkpoints.Get(ctx, collections.Join(serviceType, opBytes))
	if err != nil {
		return types.Checkpoint{}, false
	}
	return cp, true
}

// livenessBase is the height an operator's liveness is measured from: its
// latest checkpoint height, or its registration height if it has never
// checkpointed.
func (k Keeper) livenessBase(ctx context.Context, op types.Operator, opBytes []byte) int64 {
	if cp, ok := k.GetCheckpoint(ctx, opBytes, op.ServiceType); ok {
		return cp.Height
	}
	return op.RegisteredAt
}

// clearCheckpointDeadline drops an operator's liveness queue entry, if any.
func (k Keeper) clearCheckpointDeadline(ctx context.Context, serviceType string, opBytes []byte) error {
	pk := collections.Join(serviceType, opBytes)
	old, err := k.CheckpointDeadlineByOperator.Get(ctx, pk)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := k.CheckpointDeadlines.Remove(ctx, collections.Join3(old, serviceType, opBytes)); err != nil {
		return err
	}
	return k.CheckpointDeadlineByOperator.Remove(ctx, pk)
}

// setCheckpointDeadline replaces an operator's liveness queue entry.
func (k Keeper) setCheckpointDeadline(ctx context.Context, serviceType string, opBytes []byte, deadline int64) error {
	if err := k.clearCheckpointDeadline(ctx, serviceType, opBytes); err != nil {
		return err
	}
	if err := k.CheckpointDeadlines.Set(ctx, collections.Join3(deadline, serviceType, opBytes)); err != nil {
		return err
	}
	return k.CheckpointDeadlineByOperator.Set(ctx, collections.Join(serviceType, opBytes), deadline)
}

// scheduleLiveness (re)computes an operator's liveness deadline from its
// current checkpoint and its service type's lag, or clears it when the type
// does not track liveness.
func (k Keeper) scheduleLiveness(ctx context.Context, op types.Operator, opBytes []byte, lag int64) error {
	if lag <= 0 {
		return k.clearCheckpointDeadline(ctx, op.ServiceType, opBytes)
	}
	return k.setCheckpointDeadline(ctx, op.ServiceType, opBytes, k.livenessBase(ctx, op, opBytes)+lag)
}

// rescheduleServiceTypeLiveness applies a changed checkpoint_max_lag_blocks
// to every live operator of the type. Called from MsgUpdateServiceTypeConfig
// and genesis import.
func (k Keeper) rescheduleServiceTypeLiveness(ctx context.Context, serviceType string, lag int64) error {
	var addrs [][]byte
	rng := collections.NewPrefixedPairRange[string, []byte](serviceType)
	if err := k.OperatorsByServiceType.Walk(ctx, rng, func(key collections.Pair[string, []byte]) (bool, error) {
		addrs = append(addrs, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, addr := range addrs {
		op, ok := k.GetOperator(ctx, addr, serviceType)
		if !ok {
			continue
		}
		if err := k.scheduleLiveness(ctx, op, addr, lag); err != nil {
			return err
		}
	}
	return nil
}

// livenessTracked reports whether an operator status is expected to keep
// checkpointing. UNBONDING / terminal operators no longer count toward a
// quorum (§8.1), so they are not held to liveness.
func livenessTracked(status types.OperatorStatus) bool {
	return status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE ||
		status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
}

// sweepCheckpointLiveness is EndBlocker sweep 5: process up to limit
// liveness deadlines at or before currentHeight.
func (k Keeper) sweepCheckpointLiveness(ctx context.Context, currentHeight int64, limit int) (int, error) {
	var due []collections.Triple[int64, string, []byte]
	rng := new(collections.Range[collections.Triple[int64, string, []byte]]).
		EndInclusive(collections.Join3(currentHeight, string([]byte{0xff}), []byte{0xff}))
	if err := k.CheckpointDeadlines.Walk(ctx, rng, func(key collections.Triple[int64, string, []byte]) (bool, error) {
		if key.K1() > currentHeight {
			return true, nil
		}
		due = append(due, key)
		return len(due) >= limit, nil
	}); err != nil {
		return 0, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
	for _, key := range due {
		deadline, serviceType, opBytes := key.K1(), key.K2(), key.K3()
		if err := k.clearCheckpointDeadline(ctx, serviceType, opBytes); err != nil {
			return len(due), err
		}

		op, ok := k.GetOperator(ctx, opBytes, serviceType)
		if !ok || !livenessTracked(op.Status) {
			continue // archived or winding down: stop tracking
		}
		cfg, err := k.resolveServiceTypeConfig(ctx, serviceType)
		if err != nil || cfg.CheckpointMaxLagBlocks <= 0 {
			continue // liveness tracking turned off since scheduling
		}
		lag := cfg.CheckpointMaxLagBlocks
		base := k.livenessBase(ctx, op, opBytes)
		if base+lag > currentHeight {
			// Caught up after this entry was written; reschedule.
			if err := k.setCheckpointDeadline(ctx, serviceType, opBytes, base+lag); err != nil {
				return len(due), err
			}
			continue
		}

		dedupe := []byte(fmt.Sprintf("liveness|%s|%x|%d", serviceType, opBytes, deadline))
		evidence := fmt.Sprintf("liveness:checkpoint_height=%d,block=%d,max_lag=%d", base, currentHeight, lag)
		reportID, _, rerr := k.openSystemReport(ctx, types.ModuleName, moduleAddr, opBytes, serviceType, 0, evidence, dedupe)
		if rerr != nil {
			sdkCtx.Logger().Info("liveness report not filed", "operator", op.Address, "service_type", serviceType, "error", rerr)
			reportID = 0
		}
		sdkCtx.EventManager().EmitEvent(types.NewLivenessLapseEvent(op.Address, serviceType, base, lag, reportID))

		// Next report no sooner than one full window from now.
		if err := k.setCheckpointDeadline(ctx, serviceType, opBytes, currentHeight+lag); err != nil {
			return len(due), err
		}
	}
	return len(due), nil
}
