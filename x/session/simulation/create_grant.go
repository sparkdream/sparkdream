package simulation

import (
	"math/rand"
	"time"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/session/keeper"
	"sparkdream/x/session/types"
)

// SimulateMsgCreateGrant exercises the unified `MsgCreateGrant`
// umbrella by randomly picking one of three payload variants
// (RECURRING_PULL, SPENDING_ALLOWANCE, SCHEDULED_ONESHOT.Transfer) and
// constructing a payload that satisfies the per-payload validators in
// [x/session/keeper/grant_validation.go](../keeper/grant_validation.go).
//
// SESSION_KEY is excluded: the existing `SimulateMsgCreateSession`
// already covers it via the wire-compat facade, and re-creating session
// keys through the umbrella would just duplicate coverage. ScheduledOneshot's
// Exec variant is also excluded — it needs Any-encoded inner messages
// that are tricky to construct in a randomized harness; the e2e suite
// and unit tests carry that coverage.
//
// On each call:
//  1. Pick a random granter under the per-(granter, type) cap.
//  2. Pick a different grantee (different from granter to satisfy
//     `validateGrantCommon`'s self-delegation rejection).
//  3. Pick a payload variant uniformly among the three covered types.
//  4. Construct a per-variant payload with random-but-valid amounts /
//     periods / fire times bounded by `session.Params`.
//  5. Deliver via the standard SimOp pipeline.
//
// Failure modes that return `NoOpMsg` (rather than failing the sim):
//   - No spare account (not enough sim accounts).
//   - Per-granter cap reached for the chosen type.
//   - Granter balance too low to cover gas + (for oneshots) the gas
//     deposit escrow. We bail rather than risk a flaky bank-failure
//     branch in the harness.
func SimulateMsgCreateGrant(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateGrant{})

		if len(accs) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "need at least 2 accounts"), nil, nil
		}

		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get params"), nil, nil
		}

		// Pick the payload variant first so we know which per-granter
		// cap to consult when picking the granter.
		variant := r.Intn(3) // 0=RecurringPull, 1=SpendingAllowance, 2=Oneshot.Transfer

		var grantType types.GrantType
		var typeCap uint32
		switch variant {
		case 0:
			grantType = types.GrantType_GRANT_TYPE_RECURRING_PULL
			typeCap = params.MaxRecurringPullsPerGranter
		case 1:
			grantType = types.GrantType_GRANT_TYPE_SPENDING_ALLOWANCE
			typeCap = params.MaxAllowancesPerGranter
		case 2:
			grantType = types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT
			typeCap = params.MaxPendingOneshotsPerGranter
		}

		// Find a granter under the per-type cap.
		perm := r.Perm(len(accs))
		var granterAcc simtypes.Account
		granterFound := false
		for _, idx := range perm {
			acc := accs[idx]
			if countGranterGrantsByType(ctx, k, acc.Address.String(), grantType) < uint64(typeCap) {
				granterAcc = acc
				granterFound = true
				break
			}
		}
		if !granterFound {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "all accounts at per-type cap"), nil, nil
		}

		// Pick a different grantee. validateGrantCommon rejects
		// granter == grantee.
		var granteeAcc simtypes.Account
		granteeFound := false
		for _, idx := range r.Perm(len(accs)) {
			if accs[idx].Address.Equals(granterAcc.Address) {
				continue
			}
			granteeAcc = accs[idx]
			granteeFound = true
			break
		}
		if !granteeFound {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no spare grantee"), nil, nil
		}

		// Common ExpiresAt: pick within [now + min, now + max_grant_lifetime/2]
		// so even the longest grant-type-specific durations fit.
		expiresAt := ctx.BlockTime().Add(2 * 24 * time.Hour)
		if grantType == types.GrantType_GRANT_TYPE_RECURRING_PULL {
			// Need expires_at - start_time >= period_seconds at minimum
			// AND <= MaxRecurringDurationSeconds. With min period = 5s
			// (testparams) and a 2-day expires, plenty of headroom.
			expiresAt = ctx.BlockTime().Add(time.Duration(params.MaxRecurringDurationSeconds/4) * time.Second)
		}

		msg := &types.MsgCreateGrant{
			Granter:   granterAcc.Address.String(),
			Grantee:   granteeAcc.Address.String(),
			ExpiresAt: expiresAt,
			Note:      "sim grant",
		}

		switch variant {
		case 0:
			rp, ok := buildRecurringPullPayload(r, ctx, params)
			if !ok {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "could not build recurring_pull payload"), nil, nil
			}
			msg.Payload = &types.MsgCreateGrant_RecurringPull{RecurringPull: rp}
		case 1:
			sa, ok := buildSpendingAllowancePayload(r, ctx, params)
			if !ok {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "could not build spending_allowance payload"), nil, nil
			}
			msg.Payload = &types.MsgCreateGrant_SpendingAllowance{SpendingAllowance: sa}
		case 2:
			so, ok := buildOneshotTransferPayload(r, ctx, params, granteeAcc.Address.String())
			if !ok {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "could not build scheduled_oneshot payload"), nil, nil
			}
			msg.Payload = &types.MsgCreateGrant_ScheduledOneshot{ScheduledOneshot: so}
		}

		// Granter must cover gas + (oneshots only) the deposit escrow.
		// The deposit is ceil(gas_limit * gas_price) + creation_fee, but
		// for sim we approximate generously: a few million uspark
		// minimum.
		balance := bk.SpendableCoins(ctx, granterAcc.Address)
		minBalance := math.NewInt(10_000) // gas headroom
		if variant == 2 {
			// Add deposit headroom (gas_limit * gas_price + fee).
			minBalance = minBalance.Add(math.NewIntFromUint64(params.MaxOneshotExecGas)).
				Add(math.NewIntFromUint64(params.MinOneshotDepositUspark))
		}
		if balance.AmountOf("uspark").LT(minBalance) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "granter has insufficient funds"), nil, nil
		}

		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: sdk.NewCoins(),
			Context:         ctx,
			SimAccount:      granterAcc,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}

// buildRecurringPullPayload constructs a valid RecurringPullPayload.
// Amount per period is small (1k-100k uspark) so the per-grant
// max_per_epoch default (10x amount) easily covers normal cadence and
// the granter's balance survives many sim ticks.
func buildRecurringPullPayload(
	r *rand.Rand, ctx sdk.Context, params types.Params,
) (*types.RecurringPullPayload, bool) {
	if params.MinRecurringPeriodSeconds <= 0 {
		return nil, false
	}
	amount := math.NewInt(int64(r.Intn(99_000) + 1_000))
	// Period: at least min, at most 2x min (keeps the simulation lively).
	period := params.MinRecurringPeriodSeconds + r.Int63n(params.MinRecurringPeriodSeconds+1)
	return &types.RecurringPullPayload{
		AmountPerPeriod: sdk.NewCoin("uspark", amount),
		PeriodSeconds:   period,
		StartTime:       0, // defaults to block time
	}, true
}

// buildSpendingAllowancePayload constructs a valid SpendingAllowancePayload.
// Picks max_per_period generously so multiple pulls fit, and a period
// at or above the param floor.
func buildSpendingAllowancePayload(
	r *rand.Rand, ctx sdk.Context, params types.Params,
) (*types.SpendingAllowancePayload, bool) {
	if params.MinAllowancePeriodSeconds <= 0 {
		return nil, false
	}
	maxPerPeriod := math.NewInt(int64(r.Intn(900_000) + 100_000))
	period := params.MinAllowancePeriodSeconds + r.Int63n(params.MinAllowancePeriodSeconds*2+1)
	return &types.SpendingAllowancePayload{
		Denom:        "uspark",
		MaxPerPeriod: sdk.NewCoin("uspark", maxPerPeriod),
		PeriodSeconds: period,
	}, true
}

// buildOneshotTransferPayload constructs a valid ScheduledOneshot.Transfer
// payload. fire_at sits between min_schedule_delay and a small window
// thereafter so the sim sees the grant fire during a typical run.
func buildOneshotTransferPayload(
	r *rand.Rand, ctx sdk.Context, params types.Params, recipient string,
) (*types.ScheduledOneshotPayload, bool) {
	if params.MinScheduleDelaySeconds <= 0 || params.FireToExpiryBufferSeconds <= 0 {
		return nil, false
	}
	// fire_at: min_delay + jitter, well below max_schedule_horizon.
	jitter := r.Int63n(3600) // up to 1h jitter
	fireAt := ctx.BlockTime().Add(time.Duration(params.MinScheduleDelaySeconds+jitter) * time.Second).Unix()
	amount := math.NewInt(int64(r.Intn(99_000) + 1_000))
	return &types.ScheduledOneshotPayload{
		FireAt: fireAt,
		// Transfer variant has no gas_limit — the OneshotTransfer
		// action just runs a bank.SendCoins at fire time. The deposit
		// formula in computeOneshotDeposit returns the
		// min_oneshot_deposit_uspark floor for Transfer.
		Action: &types.ScheduledOneshotPayload_Transfer{
			Transfer: &types.OneshotTransfer{
				Recipient: recipient,
				Amount:    sdk.NewCoin("uspark", amount),
			},
		},
	}, true
}
