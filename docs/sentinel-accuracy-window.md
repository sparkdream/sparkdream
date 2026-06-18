# Sentinel Accuracy — Rolling Window (design + sketch)

Status: **proposed / not yet implemented.** Replaces the deferred "accuracy
decay over time" design in [docs/x-forum-spec.md](x-forum-spec.md) (the
`ResetEpochMetrics` / `EventSentinelAccuracyDecay` section, ~L3821-3895).

## Problem

A sentinel's reward score is gated and weighted by an **accuracy rate** —
`upheld / total_decided` over resolved appeals. Today that ratio is computed
from **lifetime** `upheld_*` / `overturned_*` counters on the forum
`SentinelActivity` record ([sentinel_reward_distribution.go:95-126](../x/rep/keeper/sentinel_reward_distribution.go#L95)).

Two problems with lifetime accuracy:

1. **Stickiness.** A long-tenured sentinel accumulates a huge `total_decided`
   denominator, so recent overturns barely move the ratio. Accuracy stops
   reflecting *current* behavior — a "too-big-to-fail" softness in the reward
   signal.
2. **No staleness pressure.** A sentinel who built a great record long ago
   keeps a pristine ratio forever, even after going inactive.

The spec's original fix was **symmetric decay** of the lifetime counters during
inactivity. That design has a fatal quirk: decaying upheld and overturned at the
*same* rate **preserves the ratio** it feeds, so it does nothing to the score
until an abrupt full reset at `max_decay_epochs`. It is also destructive (mutates
the audit record in place with lossy `Dec` truncation).

## Design: epoch-stamped ring buffer

Compute the reward accuracy over a **rolling window of the last `W` reward
epochs** instead of lifetime. Recent overturns then genuinely lower the ratio,
and inactivity naturally ages a sentinel out (old buckets fall outside the
window → `total_decided` drops below `MinAppealsForAccuracy` → ineligible until
they rebuild recent history).

Storage is a **fixed-size ring of `C` slots, each stamped with the epoch it
belongs to** — the same self-expiring-bucket trick x/session uses for UTC-day
epochs. No eviction logic, no per-epoch sweep, no unbounded growth, and gaps
(inactive epochs) are handled for free by the read-side stamp filter.

- Ring size `C` is a **forum constant** (size it generously, e.g. 24).
- Window `W` is an **x/rep param** (`SentinelAccuracyWindowEpochs`, default 6),
  bounded `1 <= W <= C`. Tunable up to `C` with no migration.
- Slot index for an epoch `e` is `e % C`.
- **Write:** when an appeal resolves, if `slot[e%C].epoch != e`, reset that slot
  to `{epoch: e, upheld: 0, overturned: 0}`, then bump upheld/overturned.
  Because `C >= W`, any slot being overwritten is necessarily older than the
  window, so the overwrite is always safe.
- **Read:** sum upheld/overturned over slots whose `epoch >= currentEpoch-W+1`.
  Stale slots from >`W` epochs ago are filtered out by the stamp check.

Reward epoch is the existing `blockHeight / SentinelRewardEpochBlocks`
([sentinel_reward_distribution.go:62](../x/rep/keeper/sentinel_reward_distribution.go#L62)).
x/rep owns that concept, so x/rep computes `currentEpoch` and passes it down to
forum on both write and read — forum never needs the x/rep param.

## What does NOT change

- **Lifetime `upheld_*` / `overturned_*` counters** stay on the proto, untouched,
  for `SentinelStatus` display and audit. Only the *reward accuracy* computation
  switches to the window.
- **Demotion** still runs off `consecutive_overturns`
  ([forum_keeper_adapter.go:290](../x/forum/keeper/forum_keeper_adapter.go#L290)),
  independent of the accuracy ratio. The hard accountability path is unaffected.
- **Per-epoch activity bonuses** (`epoch_hides`/`epoch_locks`/`epoch_moves`/
  `epoch_pins`/`epoch_curations`) and `ResetSentinelEpochCounters` are unchanged.
- `consecutive_inactive_epochs` on the x/rep `BondedRole` becomes unnecessary
  (it is only ever reset to 0 today anyway) — leave dormant, no removal needed.

## Proto sketch — `proto/sparkdream/forum/v1/sentinel_activity.proto`

```proto
// One reward-epoch's resolved-appeal tally. Slot index in the ring is
// epoch % SentinelAccuracyRingSize; `epoch` disambiguates a live slot from a
// stale one left by an inactive epoch.
message AccuracyEpochBucket {
  uint64 epoch      = 1;
  uint64 upheld     = 2;   // upheld_hides + upheld_locks + upheld_moves this epoch
  uint64 overturned = 3;   // overturned_* this epoch
}

message SentinelActivity {
  // ... existing fields 1-28 ...

  // Rolling-window accuracy ring. Fixed length SentinelAccuracyRingSize once
  // first written; slot e%C holds epoch e's resolved-appeal tally. Lifetime
  // upheld_*/overturned_* above are retained for display/audit and are NO
  // longer used for reward accuracy. Append-only field — empty on existing
  // records (see Migration).
  repeated AccuracyEpochBucket accuracy_window = 29;
}
```

`SentinelAccuracyRingSize = 24` as a `const` in `x/forum/types` (pairs with
`reptypes` `MaxSentinelAccuracyWindowEpochs = 24` — keep the two in sync; the
rep param validator caps `W` at that value).

## Keeper sketch — forum side

Bucket bump helper, called from both record paths:

```go
// x/forum/keeper/forum_keeper_adapter.go

func bumpAccuracyWindow(local *types.SentinelActivity, epoch uint64, upheld bool) {
	if len(local.AccuracyWindow) != types.SentinelAccuracyRingSize {
		local.AccuracyWindow = make([]*types.AccuracyEpochBucket, types.SentinelAccuracyRingSize)
		for i := range local.AccuracyWindow {
			local.AccuracyWindow[i] = &types.AccuracyEpochBucket{}
		}
	}
	slot := local.AccuracyWindow[epoch%types.SentinelAccuracyRingSize]
	if slot.Epoch != epoch {
		slot.Epoch, slot.Upheld, slot.Overturned = epoch, 0, 0 // overwrite stale slot
	}
	if upheld {
		slot.Upheld++
	} else {
		slot.Overturned++
	}
}
```

Thread `epoch` into the two recorders (interface change below) and call the
helper next to the existing lifetime bumps:

```go
func (k Keeper) RecordSentinelActionUpheld(ctx context.Context, epoch uint64,
	actionType reptypes.GovActionType, actionTarget string) error {
	// ... unchanged: resolve sentinel, load local, bump lifetime upheld_*,
	//     consecutive_upheld++, consecutive_overturns = 0, epoch_appeals_resolved++ ...
	bumpAccuracyWindow(&local, epoch, true)
	return k.SentinelActivity.Set(ctx, sentinel, local)
}
// RecordSentinelActionOverturned: same, bumpAccuracyWindow(&local, epoch, false)
// (demotion check on consecutive_overturns stays exactly as-is)
```

Windowed read, used only by reward distribution:

```go
// GetSentinelWindowedAccuracy returns (upheld, overturned) summed over the last
// `window` reward epochs ending at currentEpoch. Missing record -> (0,0).
func (k Keeper) GetSentinelWindowedAccuracy(ctx context.Context, addr string,
	currentEpoch, window uint64) (uint64, uint64, error) {
	local, err := k.SentinelActivity.Get(ctx, addr)
	if err != nil {
		return 0, 0, nil
	}
	var lo uint64
	if currentEpoch+1 > window {
		lo = currentEpoch - window + 1
	}
	var up, ov uint64
	for _, b := range local.AccuracyWindow {
		if b != nil && b.Epoch >= lo && b.Epoch <= currentEpoch {
			up, ov = up+b.Upheld, ov+b.Overturned
		}
	}
	return up, ov, nil
}
```

## Keeper sketch — x/rep side

Interface ([x/rep/types/expected_keepers.go](../x/rep/types/expected_keepers.go)):

```go
RecordSentinelActionUpheld(ctx context.Context, epoch uint64, actionType GovActionType, actionTarget string) error
RecordSentinelActionOverturned(ctx context.Context, epoch uint64, actionType GovActionType, actionTarget string) error
GetSentinelWindowedAccuracy(ctx context.Context, addr string, currentEpoch, window uint64) (upheld, overturned uint64, err error)
```

Appeal resolver ([msg_server_resolve_gov_action_appeal.go:138,176](../x/rep/keeper/msg_server_resolve_gov_action_appeal.go#L138)) —
compute the epoch once and pass it:

```go
epoch := uint64(sdkCtx.BlockHeight()) / params.SentinelRewardEpochBlocks
// UPHELD branch:
fk.RecordSentinelActionUpheld(ctx, epoch, appeal.ActionType, appeal.ActionTarget)
// OVERTURNED branch:
fk.RecordSentinelActionOverturned(ctx, epoch, appeal.ActionType, appeal.ActionTarget)
```

Distribution ([sentinel_reward_distribution.go:94-126](../x/rep/keeper/sentinel_reward_distribution.go#L94)) —
replace lifetime `totalDecided`/`totalUpheld` with the windowed read; Gates 2
and 5 keep the same param thresholds:

```go
up, ov, _ := k.late.forumKeeper.GetSentinelWindowedAccuracy(ctx, addr, epochNum, params.SentinelAccuracyWindowEpochs)
totalDecided := up + ov
if totalDecided < params.MinAppealsForAccuracy {          // Gate 2 (unchanged threshold)
	return false, nil
}
// ... Gates 3, 4 unchanged ...
accuracyRate := math.LegacyNewDec(int64(up)).Quo(math.LegacyNewDec(int64(totalDecided)))
if accuracyRate.LT(params.MinSentinelAccuracy) {          // Gate 5 (unchanged threshold)
	return false, nil
}
```

(`SentinelActivityCounters` can drop the six lifetime upheld/overturned fields
once distribution no longer reads them, or keep them for other callers — none
today.)

New param ([x/rep/types/params.go](../x/rep/types/params.go)), Operations-Committee
or governance tunable like the other sentinel reward params:

```go
SentinelAccuracyWindowEpochs: 6,  // rolling window for reward accuracy
// validate: 1 <= W <= MaxSentinelAccuracyWindowEpochs (== forum ring size)
```

## Migration & transition

- **Proto migration-free:** `accuracy_window` is an append-only field; existing
  records deserialize with it empty.
- **Transition behavior:** at cutover, windowed `total_decided` is 0 for everyone,
  so all sentinels are reward-ineligible until they accrue `MinAppealsForAccuracy`
  resolved appeals *within the window*. This is the intended "everyone rebuilds
  recent history" reset — but it is a real ramp. Tune `MinAppealsForAccuracy`
  and `W` together so the ramp is reasonable (e.g. W=6, MinAppeals=10 is ~2
  resolved appeals/epoch). Do **not** seed the ring from lifetime totals — that
  reintroduces exactly the stickiness this change removes.

## Test plan

- Window arithmetic: bump across `> C` epochs, assert stale slots are
  overwritten and reads exclude out-of-window epochs (table-driven on the ring).
- Gap handling: inactive epochs in the middle of the window don't leak stale
  tallies into the current read.
- Accuracy responsiveness: a fresh overturn measurably drops the windowed
  ratio where the same overturn against a large lifetime denominator would not.
- Distribution: a sentinel with a great lifetime record but no in-window
  resolved appeals is ineligible; demotion via `consecutive_overturns` still
  fires regardless of the window.
- E2E: [test/forum/sentinel_accuracy_window_test.sh](../test/forum/sentinel_accuracy_window_test.sh)
  resolves hide appeals UPHELD/OVERTURNED via the Operations Committee and
  asserts the verdict lands in `SentinelActivity.accuracy_window`, bucketed per
  reward epoch, with a fresh bucket opening across an epoch boundary; plus the
  rep ops-params round-trip (`test/rep/operational_params_test.sh` + the
  update/reset proposal JSONs) carries `sentinel_accuracy_window_epochs` through
  the full-replacement builder (per the OperationalParams full-replacement gotcha).

## Spec-section replacement

In [docs/x-forum-spec.md](x-forum-spec.md), replace the `ResetEpochMetrics` /
`EventSentinelAccuracyDecay` design (~L3821-3895 and the matching event at
~L5216-5223) with a pointer to this rolling-window design, and update the
EndBlocker table row (L15) to drop "accuracy decay" (the ring is self-expiring;
no decay pass exists). Update the "not yet implemented" note at L3476 to mark
accuracy freshness as handled by the window rather than by decay.
```
