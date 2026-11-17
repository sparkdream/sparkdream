package keeper

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/storage/types"
)

// EndBlocker makes one pass over the timers due at this height, in key
// order, at most max_deadlines_per_block of them; the rest carry to later
// blocks. Each entry runs on its own branch of state and never fails the
// block. An entry that fails is moved to now + deadline_retry_blocks; it is
// never dropped, because a dropped settlement would leave a request
// COMPLETED for ever with its fee in escrow and its hash blocked.
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	p, err := k.Params.Get(ctx)
	if err != nil {
		// No params: the module's genesis has not run (a genesis file
		// written before the module existed). There is nothing to do, and
		// an error here would halt the chain.
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	if k.wired() != nil {
		return nil
	}
	now := sdkCtx.BlockHeight()
	var due []collections.Triple[int64, uint64, uint32]
	rng := new(collections.Range[collections.Triple[int64, uint64, uint32]]).
		EndExclusive(collections.Join3[int64, uint64, uint32](now+1, 0, 0))
	if err := k.Deadlines.Walk(ctx, rng, func(key collections.Triple[int64, uint64, uint32]) (bool, error) {
		due = append(due, key)
		return uint64(len(due)) >= uint64(p.MaxDeadlinesPerBlock), nil
	}); err != nil {
		return err
	}
	for _, key := range due {
		if err := k.runDeadline(sdkCtx, key, now, p); err != nil {
			sdkCtx.Logger().Error("storage: deadline failed", "request_id", key.K2(), "kind", key.K3(), "err", err)
			if rerr := k.retryDeadline(ctx, key, now, p, err); rerr != nil {
				sdkCtx.Logger().Error("storage: deadline retry failed", "request_id", key.K2(), "err", rerr)
			}
		}
	}
	return nil
}

// runDeadline handles one entry on a branch and commits it only on success.
func (k Keeper) runDeadline(sdkCtx sdk.Context, key collections.Triple[int64, uint64, uint32], now int64, p types.Params) (err error) {
	cacheCtx, write := sdkCtx.CacheContext()
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic: %v", rec)
		}
	}()
	if err := k.handleDeadline(cacheCtx, key, now, p); err != nil {
		return err
	}
	write()
	return nil
}

// retryDeadline moves a failed entry forward.
func (k Keeper) retryDeadline(ctx context.Context, key collections.Triple[int64, uint64, uint32], now int64, p types.Params, cause error) error {
	if err := k.Deadlines.Remove(ctx, key); err != nil {
		return err
	}
	retryAt := now + p.DeadlineRetryBlocks
	if err := k.Deadlines.Set(ctx, collections.Join3(retryAt, key.K2(), key.K3())); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("storage.deadline_failed",
		sdk.NewAttribute("request_id", u64(key.K2())),
		sdk.NewAttribute("kind", u64(uint64(key.K3()))),
		sdk.NewAttribute("retry_at", i64(retryAt)),
		sdk.NewAttribute("error", cause.Error()),
	))
	return nil
}

// handleDeadline applies one timer. The entry is always consumed: the
// request's own fields, not the entry's height, decide whether it is due, so
// an entry left behind by a retry or a moved timer is dropped as stale.
func (k Keeper) handleDeadline(ctx context.Context, key collections.Triple[int64, uint64, uint32], now int64, p types.Params) error {
	if err := k.Deadlines.Remove(ctx, key); err != nil {
		return err
	}
	r, err := k.Requests.Get(ctx, key.K2())
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	old := r
	switch key.K3() {
	case types.DeadlineExpiry:
		if r.Status != types.RequestStatus_REQUEST_STATUS_PENDING || r.Deadline > now {
			return nil
		}
		if err := k.expire(ctx, &r, now); err != nil {
			return err
		}

	case types.DeadlineClaim:
		if r.Status != types.RequestStatus_REQUEST_STATUS_CLAIMED || r.ClaimExpiresAt > now {
			return nil
		}
		if err := k.lapse(ctx, &r, now, p); err != nil {
			return err
		}

	case types.DeadlineSettle:
		if r.Status != types.RequestStatus_REQUEST_STATUS_COMPLETED || r.SettleAt > now {
			return nil
		}
		if err := k.settle(ctx, &r, now, p); err != nil {
			return err
		}

	case types.DeadlineForfeit:
		if r.Status != types.RequestStatus_REQUEST_STATUS_REJECTED || r.ForfeitAt == 0 || r.ForfeitAt > now {
			return nil
		}
		if err := k.forfeit(ctx, &r); err != nil {
			return err
		}

	case types.DeadlinePrune:
		if !r.Status.IsTerminal() || r.ForfeitAt != 0 {
			return nil
		}
		// The prune time is the one timer derived from a param. If
		// terminal_retention_blocks was raised after this entry was
		// written, no entry exists at the new time: write it, or the
		// request would never be pruned.
		if due := r.ClosedAt + p.TerminalRetentionBlocks; due > now {
			return k.Deadlines.Set(ctx, collections.Join3(due, r.Id, types.DeadlinePrune))
		}
		return k.deleteRequest(ctx, r, p)

	default:
		return nil
	}
	return k.saveRequest(ctx, &old, r, p)
}

// lapse returns a claim that ran out to PENDING, bars the operator and, if
// it is still a live operator, counts the lapse.
func (k Keeper) lapse(ctx context.Context, r *types.Request, now int64, p types.Params) error {
	operator := r.Operator
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return err
	}
	releaseClaim(r)
	emit(ctx, requestEvent("claim_lapsed", *r, sdk.NewAttribute("operator", operator)))
	if _, live := k.liveOperator(ctx, operator, b.ServiceType); live {
		c, err := k.bump(ctx, operator, types.CounterKind_COUNTER_KIND_LAPSE, now, p)
		if err != nil {
			return err
		}
		// One system report per window, when the limit is first passed.
		if c.Count == uint64(p.MaxLapsesPerWindow)+1 {
			k.reportLapses(ctx, operator, b.ServiceType, c.WindowStart)
		}
	}
	if now >= r.Deadline {
		return k.expire(ctx, r, now)
	}
	return nil
}

// reportLapses files a system report against an operator that let too many
// claims lapse. Best effort: a failure is logged and never undoes the lapse.
func (k Keeper) reportLapses(ctx context.Context, operator, serviceType string, windowStart int64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	addr, err := k.accAddr(operator)
	if err != nil {
		return
	}
	dedupe := sha256.Sum256([]byte(fmt.Sprintf("storage/lapses/%s/%s/%d", serviceType, operator, windowStart)))
	cacheCtx, write := sdkCtx.CacheContext()
	if _, _, err := k.late.service.OpenSystemReport(cacheCtx, k.moduleAddress(), addr, serviceType, 0,
		fmt.Sprintf("storage: more than the allowed claim lapses in the window starting at height %d", windowStart), dedupe[:]); err != nil {
		sdkCtx.Logger().Error("storage: lapse report failed", "operator", operator, "err", err)
		return
	}
	write()
}

// settle pays a completion whose settle time has come, or voids it when its
// operator was slashed out of x/service in the meantime.
func (k Keeper) settle(ctx context.Context, r *types.Request, now int64, p types.Params) error {
	b, err := k.Backends.Get(ctx, r.Backend)
	if err != nil {
		return err
	}
	if _, live := k.liveOperator(ctx, r.Operator, b.ServiceType); !live {
		addr, err := k.accAddr(r.Operator)
		if err != nil {
			return err
		}
		slashed, err := k.late.service.HasSlashedRecord(ctx, addr.Bytes(), b.ServiceType)
		if err != nil {
			return err
		}
		if slashed {
			k.emitVoided(ctx, *r, "", "operator_slashed")
			voidToPending(r, now, p)
			return nil
		}
	}
	if err := k.payout(ctx, r); err != nil {
		return err
	}
	r.Status = types.RequestStatus_REQUEST_STATUS_SETTLED
	// Added sources have done their work: the copy is found by hash, and a
	// settled request is kept for good.
	if len(r.Sources) > 1 {
		r.Sources = r.Sources[:1]
	}
	emit(ctx, requestEvent("request_settled", *r,
		sdk.NewAttribute("operator", r.Operator),
		sdk.NewAttribute("locator", r.Locator),
		sdk.NewAttribute("paid_to", r.PaidTo),
		sdk.NewAttribute("paid_amount", r.PaidAmount.String()),
	))
	return nil
}

// forfeit burns the held fee of a request that stayed flagged through the
// hold, and takes it off the hash's held list.
func (k Keeper) forfeit(ctx context.Context, r *types.Request) error {
	amount := r.CopyFee
	if err := k.burn(ctx, amount); err != nil {
		return err
	}
	r.CopyFee = math.ZeroInt()
	r.ForfeitAt = 0
	rec, err := k.RemovedHashes.Get(ctx, r.Sha256)
	if err == nil {
		kept := rec.Held[:0:0]
		for _, id := range rec.Held {
			if id != r.Id {
				kept = append(kept, id)
			}
		}
		rec.Held = kept
		if err := k.RemovedHashes.Set(ctx, r.Sha256, rec); err != nil {
			return err
		}
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	emit(ctx, requestEvent("fee_forfeited", *r, sdk.NewAttribute("amount", amount.String())))
	return nil
}
