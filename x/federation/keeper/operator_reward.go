package keeper

import (
	"context"
	"fmt"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"

	"sparkdream/x/federation/types"
)

// Bridge operator compensation.
//
// Operators run the infrastructure that pulls content off ActivityPub / AT
// Protocol and pay SPARK gas to submit it. Until this existed the role was
// pure cost: a SPARK bond posted on x/service, gas spent per submission, and
// nothing coming back — while the verifier on the other side of the same
// exchange now earns from x/rep's pool. An unpaid role does not hold a
// roster, and a peer with no operator is a dead peer.
//
// FUNDING. Federation takes ONE capped claim on the community pool per UTC
// day, the shape x/shield uses for its gas reserve and x/rep for the
// bonded-role pools. It is deliberately NOT the x/split "Federation
// Operations" allocation the spec originally described: that would have made
// x/split — whose job is dividing whatever REMAINS among the councils —
// import x/federation, and it would have put the split module in the business
// of per-role scoring. See the spec's Section 15.6.
//
// WEIGHTING. Unlike the verifier's flat base, operator pay IS proportional to
// verified submissions. The asymmetry is the point: a verifier self-certifies,
// so paying per item funds rubber-stamping, whereas an operator's verified
// count was confirmed by an independent second party who staked their own bond
// on being right. Volume an operator cannot unilaterally manufacture is a fair
// thing to pay for, and it is the only signal that distinguishes an operator
// actually bridging content from one sitting idle on a bond.

const subAddrKeyOperatorRewards = "operator_rewards"

// OperatorRewardPoolAddress returns the deterministic address holding the
// bridge-operator reward pool's bond-denom balance. An ordinary bank account,
// so a council can top it up with a plain send.
func OperatorRewardPoolAddress() sdk.AccAddress {
	return sdkaddress.Module(types.ModuleName, []byte(subAddrKeyOperatorRewards))
}

// GetOperatorRewardPool returns the pool's current balance.
func (k Keeper) GetOperatorRewardPool(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, OperatorRewardPoolAddress(), k.BondDenom(ctx)).Amount
}

// utcDayOf buckets a block time into a UTC day index.
//
// Guards the pre-epoch case explicitly: a zero or pre-1970 block time makes
// Unix() negative, and converting that to uint64 wraps to an enormous bucket.
// Every node computes the same wrapped value so consensus holds, but the cap
// would effectively restart on the first real block.
func utcDayOf(t time.Time) uint64 {
	secs := t.Unix()
	if secs < 0 {
		return 0
	}
	return uint64(secs) / 86400
}

// sdkBlockTime is a small helper so query handlers can bucket the current UTC
// day without unwrapping the context at each call site.
func sdkBlockTime(ctx context.Context) time.Time {
	return sdk.UnwrapSDKContext(ctx).BlockTime()
}

// GetOperatorRewardDayFunding reports how much was drawn on a UTC day.
func (k Keeper) GetOperatorRewardDayFunding(ctx context.Context, day uint64) math.Int {
	raw, err := k.OperatorRewardDayFunding.Get(ctx, day)
	if err != nil {
		return math.ZeroInt()
	}
	v, ok := math.NewIntFromString(raw)
	if !ok {
		return math.ZeroInt()
	}
	return v
}

// OperatorRewardDailyAllowance is the amount federation may draw from the
// community pool on one UTC day:
//
//	annual_provisions * community_tax * operator_reward_inflation_share / 365
//
// Returns zero when the share is unset, when either keeper is unwired, or when
// anything is unreadable — funding is best-effort and must never fail a block.
func (k Keeper) OperatorRewardDailyAllowance(ctx context.Context, params types.Params) math.Int {
	share := params.OperatorRewardInflationShare
	if share.IsNil() || !share.IsPositive() {
		return math.ZeroInt()
	}
	if k.late.mintKeeper == nil || k.late.distrKeeper == nil {
		return math.ZeroInt()
	}
	provisions, err := k.late.mintKeeper.AnnualProvisions(ctx)
	if err != nil || provisions.IsNil() || !provisions.IsPositive() {
		return math.ZeroInt()
	}
	tax, err := k.late.distrKeeper.GetCommunityTax(ctx)
	if err != nil || tax.IsNil() || !tax.IsPositive() {
		return math.ZeroInt()
	}
	return provisions.Mul(tax).Mul(share).QuoInt64(365).TruncateInt()
}

// FundOperatorRewardPool tops the pool up from the community pool, bounded by
// the daily allowance, the pool's own headroom under its cap, and what the
// community pool actually holds.
//
// Runs in BeginBlock and never returns an error that could fail a block.
func (k Keeper) FundOperatorRewardPool(ctx context.Context) error {
	if k.late.distrKeeper == nil {
		return nil // not wired (tests, or a chain without distribution)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}
	dailyCap := k.OperatorRewardDailyAllowance(ctx, params)
	if !dailyCap.IsPositive() {
		return nil // automatic funding disabled, or nothing being minted yet
	}

	maxPool := params.MaxOperatorRewardPool
	if maxPool.IsNil() || !maxPool.IsPositive() {
		return nil
	}
	headroom := maxPool.Sub(k.GetOperatorRewardPool(ctx))
	if !headroom.IsPositive() {
		return nil // pool full: an idle role costs the community pool nothing
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	day := utcDayOf(sdkCtx.BlockTime())
	alreadyDrawn := k.GetOperatorRewardDayFunding(ctx, day)
	remaining := dailyCap.Sub(alreadyDrawn)
	if !remaining.IsPositive() {
		return nil
	}

	draw := math.MinInt(headroom, remaining)

	// Never ask for more than the pool holds: DistributeFromFeePool would
	// error, and in BeginBlock that error would take the block with it.
	denom := k.BondDenom(ctx)
	pool, err := k.late.distrKeeper.GetCommunityPool(ctx)
	if err != nil {
		return nil
	}
	available := pool.AmountOf(denom).TruncateInt()
	if !available.IsPositive() {
		return nil
	}
	draw = math.MinInt(draw, available)
	if !draw.IsPositive() {
		return nil
	}

	coins := sdk.NewCoins(sdk.NewCoin(denom, draw))
	if err := k.late.distrKeeper.DistributeFromFeePool(ctx, coins, OperatorRewardPoolAddress()); err != nil {
		sdkCtx.Logger().Info("federation: operator reward pool funding failed",
			"requested", draw.String(), "error", err)
		return nil
	}
	if err := k.OperatorRewardDayFunding.Set(ctx, day, alreadyDrawn.Add(draw).String()); err != nil {
		// The SPARK already moved. Losing the ledger entry would let the next
		// block draw a second full allowance on the same day, so this is
		// logged loudly rather than swallowed silently.
		sdkCtx.Logger().Error("federation: failed to record operator funding draw",
			"day", day, "amount", draw.String(), "error", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeOperatorRewardPoolFunded,
		sdk.NewAttribute(types.AttributeKeyAmount, draw.String()),
		sdk.NewAttribute("day", fmt.Sprintf("%d", day)),
	))
	return nil
}

// IsOperatorRewardEpoch reports whether this block closes a reward epoch.
func (k Keeper) IsOperatorRewardEpoch(ctx context.Context) bool {
	params, err := k.Params.Get(ctx)
	if err != nil || params.OperatorRewardEpochBlocks == 0 {
		return false
	}
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	return h > 0 && uint64(h)%params.OperatorRewardEpochBlocks == 0
}

// CurrentOperatorRewardEpoch is the epoch number this block falls in.
func (k Keeper) CurrentOperatorRewardEpoch(ctx context.Context) uint64 {
	params, err := k.Params.Get(ctx)
	if err != nil || params.OperatorRewardEpochBlocks == 0 {
		return 0
	}
	h := sdk.UnwrapSDKContext(ctx).BlockHeight()
	if h <= 0 {
		return 0
	}
	return uint64(h) / params.OperatorRewardEpochBlocks
}

// operatorRewardCandidate is one eligible binding and its weight.
type operatorRewardCandidate struct {
	key      collections.Pair[string, string]
	binding  types.BridgeBinding
	verified uint64
}

// DistributeOperatorRewards splits the pool across eligible bindings in
// proportion to their independently-verified submissions this epoch, then
// resets the per-epoch counters on every binding regardless of eligibility.
//
// Idempotency: a double invocation on the same block would distribute twice;
// the EndBlocker guarantees a single call per boundary.
func (k Keeper) DistributeOperatorRewards(ctx context.Context) error {
	if !k.IsOperatorRewardEpoch(ctx) {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	epochNum := k.CurrentOperatorRewardEpoch(ctx)

	var (
		eligible      []operatorRewardCandidate
		allKeys       []collections.Pair[string, string]
		totalVerified uint64
	)

	if err := k.BridgeBindings.Walk(ctx, nil, func(key collections.Pair[string, string], b types.BridgeBinding) (bool, error) {
		allKeys = append(allKeys, key)

		// Gate 1: not suspended. AfterOperatorUnderfunded sets this when a
		// slash drops the bond below min_bond, so an operator whose stake no
		// longer backs their submissions earns nothing.
		if b.Suspended {
			return false, nil
		}

		// Gate 2: real verified work this epoch. This is also what makes the
		// weight safe — the count is gated by an independent verifier.
		if uint32(b.EpochVerified) < params.MinEpochVerifiedSubmissions {
			return false, nil
		}

		// Gate 3: nothing of theirs was rejected this epoch.
		//
		// The spec words this gate as "no slashing events within the current
		// epoch". Federation cannot see a partial slash — x/service fires a
		// hook only on the terminal transitions — but it CAN see the thing
		// the gate is actually about: a challenge upheld against this
		// operator's content, which is what opens the report in the first
		// place. Gating on the federation-visible cause rather than the
		// x/service-visible effect keeps the check honest and needs no new
		// cross-module surface.
		if b.EpochRejected > 0 {
			return false, nil
		}

		// Gate 4: unverified rate. An operator flooding the queue with content
		// no verifier will confirm is spending verifier attention, not
		// producing value — and their submissions are the denominator of
		// every verifier's workload.
		if b.EpochSubmitted > 0 && !params.MaxUnverifiedRate.IsNil() {
			rate := math.LegacyNewDec(int64(b.EpochUnverified)).
				Quo(math.LegacyNewDec(int64(b.EpochSubmitted)))
			if rate.GT(params.MaxUnverifiedRate) {
				return false, nil
			}
		}

		eligible = append(eligible, operatorRewardCandidate{key: key, binding: b, verified: b.EpochVerified})
		totalVerified += b.EpochVerified
		return false, nil
	}); err != nil {
		return fmt.Errorf("walk bridge bindings: %w", err)
	}

	pool := k.GetOperatorRewardPool(ctx)
	switch {
	case len(eligible) == 0:
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeOperatorRewardEpochSkipped,
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "no_eligible_operators"),
		))
	case !pool.IsPositive() || totalVerified == 0:
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeOperatorRewardEpochSkipped,
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
			sdk.NewAttribute("reason", "empty_pool"),
		))
	default:
		totalDec := math.LegacyNewDec(int64(totalVerified))
		for _, c := range eligible {
			share := math.LegacyNewDec(int64(c.verified)).Quo(totalDec)
			allocation := share.MulInt(pool).TruncateInt()
			if !allocation.IsPositive() {
				continue
			}
			if err := k.payoutOperatorReward(ctx, c, allocation, epochNum); err != nil {
				// One bad address must not abort the distribution for the rest.
				sdkCtx.Logger().Error("federation: operator reward payout failed",
					"operator", c.binding.Address, "peer", c.binding.PeerId,
					"amount", allocation.String(), "error", err)
			}
		}
	}

	// Reset per-epoch counters on EVERY binding, eligible or not, or an
	// ineligible one carries stale epoch activity into the next window.
	for _, key := range allKeys {
		b, err := k.BridgeBindings.Get(ctx, key)
		if err != nil {
			continue
		}
		if b.EpochSubmitted == 0 && b.EpochVerified == 0 && b.EpochRejected == 0 && b.EpochUnverified == 0 {
			continue
		}
		b.EpochSubmitted, b.EpochVerified, b.EpochRejected, b.EpochUnverified = 0, 0, 0, 0
		if err := k.BridgeBindings.Set(ctx, key, b); err != nil {
			sdkCtx.Logger().Warn("federation: reset operator epoch counters failed",
				"operator", b.Address, "peer", b.PeerId, "error", err)
		}
	}
	return nil
}

// payoutOperatorReward moves SPARK from the pool to the operator and records
// it on the binding.
func (k Keeper) payoutOperatorReward(
	ctx context.Context,
	c operatorRewardCandidate,
	amount math.Int,
	epochNum uint64,
) error {
	addr, err := sdk.AccAddressFromBech32(c.binding.Address)
	if err != nil {
		return fmt.Errorf("invalid operator address %q: %w", c.binding.Address, err)
	}
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), amount))
	if err := k.bankKeeper.SendCoins(ctx, OperatorRewardPoolAddress(), addr, coins); err != nil {
		return err
	}

	b := c.binding
	prev := b.CumulativeRewards
	if prev.IsNil() {
		prev = math.ZeroInt()
	}
	b.CumulativeRewards = prev.Add(amount)
	b.LastRewardEpoch = int64(epochNum)
	if err := k.BridgeBindings.Set(ctx, c.key, b); err != nil {
		return err
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeOperatorRewardPaid,
		sdk.NewAttribute(types.AttributeKeyOperator, b.Address),
		sdk.NewAttribute(types.AttributeKeyPeerID, b.PeerId),
		sdk.NewAttribute(types.AttributeKeyAmount, amount.String()),
		sdk.NewAttribute("verified", fmt.Sprintf("%d", c.verified)),
		sdk.NewAttribute("epoch", fmt.Sprintf("%d", epochNum)),
	))
	return nil
}

// BurnOperatorRewardPoolOverflow caps the pool the way x/rep caps its role
// pools: above max_operator_reward_pool a fraction of the excess is burned
// each epoch and the rest stays to be distributed. The cap stops an
// over-funded pool sitting as a standing prize that makes the role worth
// farming rather than doing; the burn is partial so a temporary spike is not
// destroyed outright.
func (k Keeper) BurnOperatorRewardPoolOverflow(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}
	maxPool := params.MaxOperatorRewardPool
	burnRatio := params.OperatorRewardPoolOverflowBurnRatio
	if maxPool.IsNil() || burnRatio.IsNil() {
		return nil
	}
	current := k.GetOperatorRewardPool(ctx)
	if !current.GT(maxPool) {
		return nil
	}
	burnAmount := burnRatio.MulInt(current.Sub(maxPool)).TruncateInt()
	if !burnAmount.IsPositive() {
		return nil
	}

	// Module-aware send: a plain SendCoins to the raw module address creates a
	// BaseAccount there and the BurnCoins below then panics resolving it as a
	// module account.
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), burnAmount))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, OperatorRewardPoolAddress(),
		types.ModuleName, coins); err != nil {
		return fmt.Errorf("move operator overflow to module account: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("burn operator reward pool overflow: %w", err)
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeOperatorRewardPoolOverflowBurned,
		sdk.NewAttribute(types.AttributeKeyAmount, burnAmount.String()),
	))
	return nil
}
