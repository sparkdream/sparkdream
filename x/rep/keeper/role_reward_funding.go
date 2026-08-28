package keeper

import (
	"context"
	"fmt"
	"time"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Automatic funding for the bonded-role reward pools.
//
// Role pay used to depend on somebody transferring SPARK into a pool. That is
// friction in the worst place: pay that arrives only when a committee remembers
// to send it arrives unpredictably, and unpredictable pay does not hold a
// roster — which matters most for initiative reviewers, who are load-bearing
// for completion since the staker veto was retired.
//
// So x/rep takes one capped claim on the community pool, the way x/shield funds
// its gas reserve, and divides it internally. One intake rather than a skim per
// role: the community pool should see a single claim from this module, and
// adding a fourth bonded role must not mean adding a fourth funding line.

// roleRewardPool pairs a pool's address with the cap that defines its headroom.
type roleRewardPool struct {
	name    string
	addr    sdk.AccAddress
	maxPool math.Int
}

// fundedRolePools lists every bonded-role pool the intake divides across. A new
// bonded role is added here and inherits funding with no new parameter.
func (k Keeper) fundedRolePools(params types.Params) []roleRewardPool {
	return []roleRewardPool{
		{name: "content_sentinel", addr: SentinelRewardPoolAddress(), maxPool: params.MaxSentinelRewardPool},
		{name: "initiative_reviewer", addr: ReviewerRewardPoolAddress(), maxPool: params.MaxReviewerRewardPool},
		{name: "collect_curator", addr: CuratorRewardPoolAddress(), maxPool: params.MaxCuratorRewardPool},
		{name: "federation_verifier", addr: VerifierRewardPoolAddress(), maxPool: params.MaxVerifierRewardPool},
	}
}

// utcDayOf buckets a block time into a UTC day index.
//
// Guards the pre-epoch case explicitly: a zero or pre-1970 block time makes
// Unix() negative, and converting that to uint64 wraps to an enormous bucket.
// Every node computes the same wrapped value so consensus holds, but the day
// would be nonsense and the cap would effectively restart on the first real
// block. Clamping to day 0 keeps the ledger meaningful from genesis onward.
func utcDayOf(t time.Time) uint64 {
	secs := t.Unix()
	if secs < 0 {
		return 0
	}
	return uint64(secs) / 86400
}

// GetRoleRewardDayFunding reports how much has been skimmed on a UTC day.
func (k Keeper) GetRoleRewardDayFunding(ctx context.Context, day uint64) math.Int {
	raw, err := k.RoleRewardDayFunding.Get(ctx, day)
	if err != nil {
		return math.ZeroInt()
	}
	v, ok := math.NewIntFromString(raw)
	if !ok {
		return math.ZeroInt()
	}
	return v
}

// roleRewardDailyAllowance is the SPARK x/rep may draw from the community pool
// on one UTC day:
//
//	annual_provisions * community_tax * role_reward_inflation_share / 365
//
// A share of inflation rather than a fixed amount because a fixed amount takes
// its LARGEST share of the pool exactly when the pool is poorest: inflation
// floats 2–5%, so a constant draw is roughly half the pool's income at the top
// of that range and more than all of it at the bottom — and x/rep skims before
// x/split, so at the bottom the councils get nothing. A share is
// counter-cyclical, and tracks supply growth without periodic retuning.
//
// The base is deliberately the inflation RATE, not the community pool balance.
// The balance holds the 95M SPARK genesis allocation that x/split exists to
// hand to the councils, plus any direct fund-community-pool deposit; neither is
// income, and a share of the balance would raid both.
//
// Returns zero when the share is unset/zero (automatic funding off) or when
// either keeper is unwired or unreadable — funding is best-effort and must
// never be able to fail a block.
func (k Keeper) roleRewardDailyAllowance(ctx context.Context, params types.Params) math.Int {
	share := params.RoleRewardInflationShare
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

// FundRoleRewardPools tops the bonded-role pools up from the community pool,
// bounded by the daily allowance (a share of inflation) per UTC day, then
// divides the intake
// across the pools in proportion to how far each is below its own cap.
//
// Headroom-proportional rather than a fixed per-role share: it needs no
// per-role parameter, it self-balances as roles are added, and a pool already
// at its cap draws nothing — so an idle role costs the community pool nothing.
func (k Keeper) FundRoleRewardPools(ctx context.Context) error {
	if k.late.distrKeeper == nil {
		return nil // not wired (tests, or a chain without distribution)
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}
	dailyCap := k.roleRewardDailyAllowance(ctx, params)
	if !dailyCap.IsPositive() {
		return nil // automatic funding disabled, or nothing being minted yet
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	denom := k.BondDenom(ctx)
	pools := k.fundedRolePools(params)

	// Total headroom decides both how much to draw and how to split it.
	type share struct {
		pool     roleRewardPool
		headroom math.Int
	}
	shares := make([]share, 0, len(pools))
	totalHeadroom := math.ZeroInt()
	for _, p := range pools {
		if p.maxPool.IsNil() || !p.maxPool.IsPositive() {
			continue
		}
		balance := k.bankKeeper.GetBalance(ctx, p.addr, denom).Amount
		headroom := p.maxPool.Sub(balance)
		if !headroom.IsPositive() {
			continue // at or above its cap; the overflow burn handles the rest
		}
		// Clamp before the headroom reaches the multiplication below. Params
		// validation already bounds these caps, but validation is not the only
		// way state gets written, and math.Int panics past 256 bits — in
		// BeginBlock that is a chain halt. The clamp is far above any real cap,
		// so it never changes a legitimate division: when a pool's headroom
		// exceeds what could conceivably be placed, its exact size stops
		// mattering to the proportions.
		if ceiling := types.RoleRewardPoolCeiling(); headroom.GT(ceiling) {
			headroom = ceiling
		}
		shares = append(shares, share{pool: p, headroom: headroom})
		totalHeadroom = totalHeadroom.Add(headroom)
	}
	if !totalHeadroom.IsPositive() {
		return nil // every pool is full
	}

	day := utcDayOf(sdkCtx.BlockTime())
	fundedToday := k.GetRoleRewardDayFunding(ctx, day)
	remainingToday := dailyCap.Sub(fundedToday)
	if !remainingToday.IsPositive() {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"role_reward_funding_cap_reached",
			sdk.NewAttribute("day", fmt.Sprintf("%d", day)),
			sdk.NewAttribute("funded", fundedToday.String()),
			sdk.NewAttribute("cap", dailyCap.String()),
		))
		return nil
	}

	draw := math.MinInt(totalHeadroom, remainingToday)

	// Never draw more than the community pool actually holds: DistributeFromFeePool
	// would fail the whole block otherwise.
	cp, cpErr := k.late.distrKeeper.GetCommunityPool(ctx)
	if cpErr != nil {
		return nil
	}
	available := cp.AmountOf(denom).TruncateInt()
	if !available.IsPositive() {
		return nil
	}
	if available.LT(draw) {
		draw = available
	}
	if !draw.IsPositive() {
		return nil
	}

	// Draw once into the intake, then place it. Two steps so the claim on the
	// community pool is a single auditable transfer regardless of how many
	// pools it ends up split across.
	intake := RoleRewardIntakeAddress()
	if err := k.late.distrKeeper.DistributeFromFeePool(ctx,
		sdk.NewCoins(sdk.NewCoin(denom, draw)), intake); err != nil {
		sdkCtx.Logger().Info("rep: community pool funding failed", "requested", draw.String(), "err", err)
		return nil
	}
	if err := k.RoleRewardDayFunding.Set(ctx, day, fundedToday.Add(draw).String()); err != nil {
		return err
	}

	// Place the intake's whole balance, not just this block's draw. The two are
	// normally identical, but a placement that failed on an earlier block would
	// otherwise strand SPARK here permanently; sweeping the balance makes the
	// intake self-healing. Never place more than the pools can absorb.
	toPlace := k.bankKeeper.GetBalance(ctx, intake, denom).Amount
	if toPlace.GT(totalHeadroom) {
		toPlace = totalHeadroom
	}
	if !toPlace.IsPositive() {
		return nil
	}

	// Divide by headroom. The last pool takes the remainder so integer
	// truncation cannot strand dust in the intake indefinitely.
	placed := math.ZeroInt()
	for i, sh := range shares {
		amount := toPlace.Mul(sh.headroom).Quo(totalHeadroom)
		if i == len(shares)-1 {
			amount = toPlace.Sub(placed)
		}
		if !amount.IsPositive() {
			continue
		}
		if err := k.bankKeeper.SendCoins(ctx, intake, sh.pool.addr,
			sdk.NewCoins(sdk.NewCoin(denom, amount))); err != nil {
			return fmt.Errorf("place role reward funding into %s: %w", sh.pool.name, err)
		}
		placed = placed.Add(amount)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"role_reward_pool_funded",
			sdk.NewAttribute("role", sh.pool.name),
			sdk.NewAttribute("amount", amount.String()),
			sdk.NewAttribute("day", fmt.Sprintf("%d", day)),
		))
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"role_reward_funding_drawn",
		sdk.NewAttribute("amount", draw.String()),
		sdk.NewAttribute("day", fmt.Sprintf("%d", day)),
		sdk.NewAttribute("day_total", fundedToday.Add(draw).String()),
	))
	return nil
}
