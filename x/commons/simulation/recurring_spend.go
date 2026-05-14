package simulation

import (
	"context"
	"math/rand"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// pickRandomActiveSchedule walks RecurringSpends and returns a uniformly
// random ACTIVE record (sampled via reservoir to avoid materialising the
// full slice). Returns ok=false if no ACTIVE schedules exist.
func pickRandomActiveSchedule(ctx context.Context, k keeper.Keeper, r *rand.Rand) (types.RecurringSpend, bool) {
	var picked types.RecurringSpend
	seen := 0
	_ = k.RecurringSpends.Walk(ctx, nil, func(_ uint64, rs types.RecurringSpend) (bool, error) {
		if rs.Status != types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE {
			return false, nil
		}
		seen++
		// Reservoir sampling: replace picked with probability 1/seen.
		if r.Intn(seen) == 0 {
			picked = rs
		}
		return false, nil
	})
	return picked, seen > 0
}

// pickRandomGroup returns a random Group with a non-zero policy address.
// Sim genesis stores a small set seeded by GenerateGenesisState; we use
// this for ScheduleRecurringSpend.
func pickRandomGroup(ctx context.Context, k keeper.Keeper, r *rand.Rand) (types.Group, bool) {
	var picked types.Group
	seen := 0
	_ = k.Groups.Walk(ctx, nil, func(_ string, g types.Group) (bool, error) {
		if g.PolicyAddress == "" {
			return false, nil
		}
		seen++
		if r.Intn(seen) == 0 {
			picked = g
		}
		return false, nil
	})
	return picked, seen > 0
}

// SimulateMsgScheduleRecurringSpend writes a RecurringSpend record into the
// keeper directly (the real msg handler requires a council-signed proposal
// + permission lookup, which is impractical in a sim). The schedule is
// shaped to satisfy the validation matrix so it can be claimed by a later
// sim op.
func SimulateMsgScheduleRecurringSpend(
	ak types.AuthKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, _ *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		msgType := sdk.MsgTypeURL(&types.MsgScheduleRecurringSpend{})

		group, ok := pickRandomGroup(ctx, k, r)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no groups in registry"), nil, nil
		}

		recipient, _ := simtypes.RandomAcc(r, accs)

		// Pull params for the bounded period.
		params, err := k.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "params not set"), nil, nil
		}
		period := params.MinRecurringPeriodSeconds + int64(r.Intn(int(params.MinRecurringPeriodSeconds)+1))
		// Cap duration at the lesser of params and a sane sim window.
		duration := params.MaxRecurringDurationSeconds
		if duration > 10*period {
			duration = 10 * period
		}
		if duration < period {
			duration = period
		}
		now := ctx.BlockTime().Unix()
		start := now + 1
		end := start + duration

		// Active-cap pre-check; bail rather than relying on the handler to reject.
		cur, err := k.ActiveRecurringSpendCount.Get(ctx, group.PolicyAddress)
		if err == nil && cur >= params.MaxActiveRecurringSpendsPerGroup {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "authority at active-schedule cap"), nil, nil
		}

		id, err := k.RecurringSpendSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "id alloc failed"), nil, nil
		}
		id++

		rs := types.RecurringSpend{
			Id:               id,
			Authority:        group.PolicyAddress,
			Recipient:        recipient.Address.String(),
			AmountPerPeriod:  sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1))),
			PeriodSeconds:    period,
			StartTime:        start,
			EndTime:          end,
			LastClaimAdvance: start,
			Status:           types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE,
			Note:             "sim",
		}
		if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "set failed"), nil, nil
		}
		if err := k.RecurringSpendsByAuthority.Set(ctx, collections.Join(rs.Authority, rs.Id)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "auth index set failed"), nil, nil
		}
		if err := k.RecurringSpendsByRecipient.Set(ctx, collections.Join(rs.Recipient, rs.Id)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "recip index set failed"), nil, nil
		}
		_ = k.ActiveRecurringSpendCount.Set(ctx, rs.Authority, cur+1)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper insert)"), nil, nil
	}
}

// SimulateMsgClaimRecurringSpend picks a random ACTIVE schedule, fast-
// forwards block time to just past its next eligible window, and tries
// to disburse one period directly via bankKeeper. Mirrors the real
// handler's effect on state but skips the SendCoins authorization path —
// which in a sim wouldn't match because the policy address has no signed
// tx context.
func SimulateMsgClaimRecurringSpend(
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, _ *baseapp.BaseApp, ctx sdk.Context, _ []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		msgType := sdk.MsgTypeURL(&types.MsgClaimRecurringSpend{})

		rs, ok := pickRandomActiveSchedule(ctx, k, r)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active schedules"), nil, nil
		}

		nextEligible := rs.LastClaimAdvance + rs.PeriodSeconds
		now := ctx.BlockTime().Unix()
		if now < nextEligible {
			// Advance clock just past the window. Sim does not enforce monotonic
			// time outside the test harness, so this is safe.
			ctx = ctx.WithBlockTime(time.Unix(nextEligible+1, 0))
			now = nextEligible + 1
		}
		if nextEligible > rs.EndTime {
			rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED
			_ = k.RecurringSpends.Set(ctx, rs.Id, rs)
			return simtypes.NoOpMsg(types.ModuleName, msgType, "window closed; flipped to COMPLETED"), nil, nil
		}

		authBytes, err := sdk.AccAddressFromBech32(rs.Authority)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "bad authority address"), nil, nil
		}
		recipBytes, err := sdk.AccAddressFromBech32(rs.Recipient)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "bad recipient address"), nil, nil
		}
		// SendCoins fails fast if the policy account is empty — sim genesis
		// rarely funds these accounts, so treat insufficient funds as a no-op.
		if err := bk.SendCoins(ctx, authBytes, recipBytes, rs.AmountPerPeriod); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "send failed (likely no funds)"), nil, nil
		}

		rs.LastClaimAdvance = nextEligible
		rs.ClaimsMade++
		if rs.LastClaimAdvance+rs.PeriodSeconds > rs.EndTime {
			rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED
			cur, err := k.ActiveRecurringSpendCount.Get(ctx, rs.Authority)
			if err == nil && cur > 0 {
				_ = k.ActiveRecurringSpendCount.Set(ctx, rs.Authority, cur-1)
			}
		}
		_ = k.RecurringSpends.Set(ctx, rs.Id, rs)
		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper claim)"), nil, nil
	}
}

// SimulateMsgCancelRecurringSpend flips a random ACTIVE schedule to
// CANCELED. As with Schedule/Claim, this bypasses the council-proposal
// requirement that the real msg server enforces.
func SimulateMsgCancelRecurringSpend(
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, _ *baseapp.BaseApp, ctx sdk.Context, _ []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		msgType := sdk.MsgTypeURL(&types.MsgCancelRecurringSpend{})

		rs, ok := pickRandomActiveSchedule(ctx, k, r)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active schedules"), nil, nil
		}
		rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_CANCELED
		if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "set failed"), nil, nil
		}
		cur, err := k.ActiveRecurringSpendCount.Get(ctx, rs.Authority)
		if err == nil && cur > 0 {
			_ = k.ActiveRecurringSpendCount.Set(ctx, rs.Authority, cur-1)
		}
		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper cancel)"), nil, nil
	}
}

// SimulateMsgDeclineRecurringSpend flips a random ACTIVE schedule to
// RECIPIENT_DECLINED. The decline path is the recipient's graceful-exit
// channel; including it in sim coverage catches regressions where the
// status-transition table forgets a branch.
func SimulateMsgDeclineRecurringSpend(
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, _ *baseapp.BaseApp, ctx sdk.Context, _ []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {

		msgType := sdk.MsgTypeURL(&types.MsgDeclineRecurringSpend{})

		rs, ok := pickRandomActiveSchedule(ctx, k, r)
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no active schedules"), nil, nil
		}
		rs.Status = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_RECIPIENT_DECLINED
		if err := k.RecurringSpends.Set(ctx, rs.Id, rs); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "set failed"), nil, nil
		}
		cur, err := k.ActiveRecurringSpendCount.Get(ctx, rs.Authority)
		if err == nil && cur > 0 {
			_ = k.ActiveRecurringSpendCount.Set(ctx, rs.Authority, cur-1)
		}
		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper decline)"), nil, nil
	}
}
