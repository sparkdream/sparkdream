package keeper

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// maxNoteLen is the cap on Grant.note. Kept tight to bound on-chain
// string bloat — UIs that need more can store off-chain.
const maxNoteLen = 256

// validateGrantCommon runs the cross-type checks that every payload
// variant shares: self-delegation, expiration sanity, note length, and
// the universal max_grant_lifetime_seconds cap. Per-payload validators
// run after this and assume these checks have passed.
func (k Keeper) validateGrantCommon(
	ctx context.Context,
	params types.Params,
	blockTime time.Time,
	granter, grantee string,
	expiresAt time.Time,
	note string,
) error {
	if granter == grantee {
		return types.ErrSelfDelegation
	}
	if len(note) > maxNoteLen {
		return types.ErrNoteTooLong
	}
	if !expiresAt.After(blockTime) {
		return types.ErrInvalidExpiration
	}
	if expiresAt.Sub(blockTime) > time.Duration(params.MaxGrantLifetimeSeconds)*time.Second {
		return types.ErrGrantLifetimeTooLong
	}
	return nil
}

// validateRecurringPullPayload runs RECURRING_PULL-specific checks plus
// the per-granter active-grant cap. Returns the (possibly normalized)
// payload that the handler should persist — e.g. start_time defaulted to
// block time when zero, last_claim_advance pinned to start_time.
func (k Keeper) validateRecurringPullPayload(
	ctx context.Context,
	params types.Params,
	blockTime time.Time,
	granter string,
	expiresAt time.Time,
	p *types.RecurringPullPayload,
) (*types.RecurringPullPayload, error) {
	if p == nil {
		return nil, types.ErrInvalidPayload
	}

	// Coin sanity.
	if !p.AmountPerPeriod.IsValid() || !p.AmountPerPeriod.IsPositive() {
		return nil, types.ErrAmountNotPositive
	}

	// Denom must be the chain's bond denom or in the additional
	// allowed_denoms list. DREAM is permanently rejected regardless of
	// params contents (defense-in-depth at the handler level).
	if p.AmountPerPeriod.Denom == k.DreamDenom(ctx) {
		return nil, types.ErrDreamDenomForbidden
	}
	if !k.denomAllowed(ctx, params.AllowedDenoms, p.AmountPerPeriod.Denom) {
		return nil, types.ErrDenomNotAllowed.Wrapf("denom: %s", p.AmountPerPeriod.Denom)
	}

	// Period bounds.
	if p.PeriodSeconds < params.MinRecurringPeriodSeconds {
		return nil, types.ErrPeriodTooShort.Wrapf("period_seconds=%d min=%d",
			p.PeriodSeconds, params.MinRecurringPeriodSeconds)
	}

	// Normalize start_time. Zero means "start at block_time".
	startTime := p.StartTime
	if startTime == 0 {
		startTime = blockTime.Unix()
	}
	if startTime < blockTime.Unix()-int64(time.Hour.Seconds()) {
		// Allow a small backstop (1h) for clock-skew tolerance but reject
		// arbitrarily backdated start times that would let a grantee claim
		// instantly across many periods.
		return nil, types.ErrInvalidExpiration.Wrapf("start_time is too far in the past: %d", startTime)
	}

	// Duration cap. Sub last_claim_advance baseline (which we pin to
	// start_time) and verify the (expires_at - start_time) span doesn't
	// exceed max_recurring_duration_seconds.
	if expiresAt.Unix()-startTime > params.MaxRecurringDurationSeconds {
		return nil, types.ErrDurationTooLong
	}

	// max_per_epoch parses as a non-negative sdk.Int.
	if p.MaxPerEpoch == "" {
		// Default: 10x amount_per_period if grantor didn't specify — a sane
		// daily ceiling that allows catch-up on missed claims but bounds
		// abuse.
		def := p.AmountPerPeriod.Amount.MulRaw(10)
		p.MaxPerEpoch = def.String()
	}
	maxPerEpoch, ok := sdkmath.NewIntFromString(p.MaxPerEpoch)
	if !ok || maxPerEpoch.IsNegative() {
		return nil, types.ErrInvalidMaxPerEpoch
	}
	if maxPerEpoch.LT(p.AmountPerPeriod.Amount) {
		return nil, types.ErrMaxPerEpochBelowAmount
	}

	// Per-granter cap on active RECURRING_PULL grants.
	count, err := k.CountActiveGrants(ctx, granter, types.GrantType_GRANT_TYPE_RECURRING_PULL)
	if err != nil {
		return nil, err
	}
	if count >= params.MaxRecurringPullsPerGranter {
		return nil, types.ErrMaxRecurringPullsExceeded
	}

	// Return normalized payload.
	out := *p
	out.StartTime = startTime
	out.LastClaimAdvance = startTime
	out.ClaimsMade = 0
	return &out, nil
}

// validateSpendingAllowancePayload runs SPENDING_ALLOWANCE-specific
// checks plus the per-granter active-grant cap. Returns the normalized
// payload (current_period_start anchored to block_time, spent_in_period
// zeroed) for the handler to persist.
func (k Keeper) validateSpendingAllowancePayload(
	ctx context.Context,
	params types.Params,
	blockTime time.Time,
	granter string,
	p *types.SpendingAllowancePayload,
) (*types.SpendingAllowancePayload, error) {
	if p == nil {
		return nil, types.ErrInvalidPayload
	}

	// Coin sanity.
	if !p.MaxPerPeriod.IsValid() || !p.MaxPerPeriod.IsPositive() {
		return nil, types.ErrAmountNotPositive.Wrap("max_per_period must be positive")
	}

	// Denom: hard reject DREAM regardless of params; otherwise must be
	// the chain's bond denom or in allowed_denoms; and must match
	// grant.denom field.
	if p.MaxPerPeriod.Denom == k.DreamDenom(ctx) {
		return nil, types.ErrDreamDenomForbidden
	}
	if !k.denomAllowed(ctx, params.AllowedDenoms, p.MaxPerPeriod.Denom) {
		return nil, types.ErrDenomNotAllowed.Wrapf("denom: %s", p.MaxPerPeriod.Denom)
	}
	if p.Denom == "" {
		p.Denom = p.MaxPerPeriod.Denom
	}
	if p.Denom != p.MaxPerPeriod.Denom {
		return nil, types.ErrAllowanceDenomMismatch.Wrapf("denom field %q vs max_per_period denom %q", p.Denom, p.MaxPerPeriod.Denom)
	}

	// Period bounds.
	if p.PeriodSeconds < params.MinAllowancePeriodSeconds {
		return nil, types.ErrAllowancePeriodTooShort.Wrapf("period_seconds=%d min=%d",
			p.PeriodSeconds, params.MinAllowancePeriodSeconds)
	}

	// Recipient list length cap.
	if uint32(len(p.AllowedRecipients)) > params.MaxAllowanceRecipientList {
		return nil, types.ErrRecipientListTooLong.Wrapf("len=%d max=%d",
			len(p.AllowedRecipients), params.MaxAllowanceRecipientList)
	}

	// Per-granter cap on active SPENDING_ALLOWANCE grants.
	count, err := k.CountActiveGrants(ctx, granter, types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE)
	if err != nil {
		return nil, err
	}
	if count >= params.MaxAllowancesPerGranter {
		return nil, types.ErrMaxAllowancesExceeded
	}

	// Return normalized payload. current_period_start anchors the rolling
	// window to block_time; spent_in_current_period zeroed; payload denom
	// locked to max_per_period.Denom.
	out := *p
	out.CurrentPeriodStart = blockTime.Unix()
	out.SpentInCurrentPeriod = sdk.NewCoin(p.MaxPerPeriod.Denom, sdkmath.ZeroInt())
	return &out, nil
}

// denomAllowed reports whether denom is permitted for grant payloads:
// the chain's bond denom is always allowed; additional denoms (e.g. IBC
// vouchers) are sourced from params.allowed_denoms.
func (k Keeper) denomAllowed(ctx context.Context, allowed []string, denom string) bool {
	if denom == k.BondDenom(ctx) {
		return true
	}
	for _, d := range allowed {
		if d == denom {
			return true
		}
	}
	return false
}

// utcDayIndex returns the UTC-day bucket for a block time, used by the
// per-grant max_per_epoch self-throttle. floor(block_time / 86400)
// is independent of any other module's epoch concept.
func utcDayIndex(t time.Time) int64 {
	return t.Unix() / 86_400
}

// validateScheduledOneshotPayload runs SCHEDULED_ONESHOT-specific
// checks for both Transfer and Exec variants. Returns the normalized
// payload (with fire_error cleared) and the action variant for the
// handler to dispatch on. Also returns the inner-msg type URL when
// applicable, so the caller can verify allow-list membership.
func (k Keeper) validateScheduledOneshotPayload(
	ctx context.Context,
	params types.Params,
	blockTime time.Time,
	granter string,
	expiresAt time.Time,
	p *types.ScheduledOneshotPayload,
) (*types.ScheduledOneshotPayload, error) {
	if p == nil {
		return nil, types.ErrInvalidPayload
	}
	if p.Action == nil {
		return nil, types.ErrOneshotActionMissing
	}

	// Scheduling invariants.
	if p.FireAt < blockTime.Unix()+params.MinScheduleDelaySeconds {
		return nil, types.ErrFireAtTooSoon.Wrapf("fire_at=%d need >= %d",
			p.FireAt, blockTime.Unix()+params.MinScheduleDelaySeconds)
	}
	if p.FireAt > blockTime.Unix()+params.MaxScheduleHorizonSeconds {
		return nil, types.ErrFireAtTooFar.Wrapf("fire_at=%d max=%d",
			p.FireAt, blockTime.Unix()+params.MaxScheduleHorizonSeconds)
	}
	if p.FireAt+params.FireToExpiryBufferSeconds > expiresAt.Unix() {
		return nil, types.ErrFireToExpiryBufferTooSmall.Wrapf(
			"fire_at=%d + buffer=%d > expires_at=%d",
			p.FireAt, params.FireToExpiryBufferSeconds, expiresAt.Unix())
	}

	// Per-granter cap on active oneshots.
	pendingCount, err := k.CountActiveGrants(ctx, granter, types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT)
	if err != nil {
		return nil, err
	}
	// pendingCount includes PAUSED. To distinguish, we'd need a separate
	// counter; for V1 we apply the more restrictive sum-of-both check —
	// any (active + paused) above max_pending is rejected. Once P5 ships
	// the explicit split, this will tighten.
	if pendingCount >= params.MaxPendingOneshotsPerGranter+params.MaxPausedOneshotsPerGranter {
		return nil, types.ErrMaxPendingOneshotsExceeded
	}

	// Variant-specific validation.
	switch a := p.Action.(type) {
	case *types.ScheduledOneshotPayload_Transfer:
		if a.Transfer == nil {
			return nil, types.ErrOneshotActionMissing
		}
		if !a.Transfer.Amount.IsValid() || !a.Transfer.Amount.IsPositive() {
			return nil, types.ErrAmountNotPositive.Wrap("transfer.amount must be positive")
		}
		if a.Transfer.Amount.Denom == k.DreamDenom(ctx) {
			return nil, types.ErrDreamDenomForbidden
		}
		if !k.denomAllowed(ctx, params.AllowedDenoms, a.Transfer.Amount.Denom) {
			return nil, types.ErrDenomNotAllowed.Wrapf("denom: %s", a.Transfer.Amount.Denom)
		}

	case *types.ScheduledOneshotPayload_Exec:
		if a.Exec == nil {
			return nil, types.ErrOneshotActionMissing
		}
		if a.Exec.Msg == nil {
			return nil, types.ErrOneshotInnerMsgMissing
		}
		// Inner msg must be in active allowlist AND not in the hard
		// anti-recursion denylist.
		typeURL := a.Exec.Msg.TypeUrl
		if types.NonDelegableSessionMsgs[typeURL] {
			return nil, types.ErrOneshotMsgForbidden.Wrapf("type: %s", typeURL)
		}
		// Defense-in-depth: reject targeting the legacy MsgRevokeSession too.
		if typeURL == "/sparkdream.session.v1.MsgRevokeSession" {
			return nil, types.ErrOneshotTargetRevokeGrant
		}
		allowSet := make(map[string]bool, len(params.AllowedMsgTypes))
		for _, t := range params.AllowedMsgTypes {
			allowSet[t] = true
		}
		if !allowSet[typeURL] {
			return nil, types.ErrOneshotMsgNotAllowed.Wrapf("type: %s", typeURL)
		}
		// Gas limit bounds.
		if a.Exec.GasLimit < params.MinOneshotExecGas || a.Exec.GasLimit > params.MaxOneshotExecGas {
			return nil, types.ErrOneshotGasLimitOutOfRange.Wrapf("gas_limit=%d min=%d max=%d",
				a.Exec.GasLimit, params.MinOneshotExecGas, params.MaxOneshotExecGas)
		}
		// Router must be wired so the fire path actually has a handler
		// to dispatch through. Fail loud at admin time, not at fire time.
		if k.late.router == nil {
			return nil, types.ErrRouterUnwired
		}

	default:
		return nil, types.ErrOneshotActionMissing
	}

	// Return a normalized copy. Clear any incoming fire_error
	// (caller can't pre-populate failure reasons).
	out := *p
	out.FireError = ""
	return &out, nil
}

// computeOneshotDeposit computes the SPARK deposit a granter must post
// at MsgCreateGrant time per the Rev 4 formula.
//
//	OneshotExec:     max(ceil(gas_limit * gas_price) + creation_fee, min_deposit)
//	OneshotTransfer: max(creation_fee, min_deposit)
//
// Always rounded up at each layer; flooring would create a sub-uspark
// slot exploitable at gas_limit=1.
func computeOneshotDeposit(p types.Params, action interface{}) (sdkmath.Int, error) {
	creationFee := sdkmath.NewIntFromUint64(p.OneshotCreationFee)
	minDeposit := sdkmath.NewIntFromUint64(p.MinOneshotDeposit)

	switch a := action.(type) {
	case *types.ScheduledOneshotPayload_Transfer:
		_ = a
		// Transfer variant: flat slot fee with the min-deposit floor.
		deposit := creationFee
		if minDeposit.GT(deposit) {
			deposit = minDeposit
		}
		return deposit, nil

	case *types.ScheduledOneshotPayload_Exec:
		// Exec variant: gas portion (ceiling-rounded) + creation fee, floor at min_deposit.
		gasPrice, err := sdkmath.LegacyNewDecFromStr(p.OneshotGasPrice)
		if err != nil {
			return sdkmath.ZeroInt(), types.ErrInvalidOneshotGasPrice
		}
		gasLimit := sdkmath.NewIntFromUint64(a.Exec.GasLimit)
		gasPortion := gasPrice.MulInt(gasLimit).Ceil().TruncateInt()
		deposit := gasPortion.Add(creationFee)
		if minDeposit.GT(deposit) {
			deposit = minDeposit
		}
		return deposit, nil

	default:
		return sdkmath.ZeroInt(), types.ErrOneshotActionMissing
	}
}

// addEpochSpend records a successful claim against the current UTC-day
// bucket. Returns the new bucket total, or an error if the result would
// breach max_per_epoch.
func (k Keeper) addEpochSpend(
	ctx context.Context,
	grantID uint64,
	day int64,
	amount sdkmath.Int,
	maxPerEpoch sdkmath.Int,
) (sdkmath.Int, error) {
	key := collections.Join(grantID, day)
	cur := sdkmath.ZeroInt()
	stored, err := k.EpochSpendByGrant.Get(ctx, key)
	if err == nil {
		v, ok := sdkmath.NewIntFromString(stored)
		if ok {
			cur = v
		}
	}
	next := cur.Add(amount)
	if next.GT(maxPerEpoch) {
		return cur, types.ErrEpochCeilingExceeded
	}
	if err := k.EpochSpendByGrant.Set(ctx, key, next.String()); err != nil {
		return cur, err
	}
	return next, nil
}

// pruneStaleEpochBuckets drops EpochSpendByGrant entries older than 7
// days relative to `currentDay`. Bounded work — at most O(23) probes per
// claim — so the steady-state state cost per active grant is ≤7 buckets.
func (k Keeper) pruneStaleEpochBuckets(ctx context.Context, grantID uint64, currentDay int64) {
	// Probe a small fixed window backward; collections doesn't expose
	// per-prefix walking cheaply enough here to be worth the complexity.
	for d := currentDay - 30; d < currentDay-7; d++ {
		if d <= 0 {
			continue
		}
		_ = k.EpochSpendByGrant.Remove(ctx, collections.Join(grantID, d))
	}
}
