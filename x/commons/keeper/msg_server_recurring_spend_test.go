package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// recurringTestFixture provisions a council with a generous epoch ceiling
// and one recipient. The council balance is funded so claims have coins
// to move; the time origin is fixed so windows compare cleanly.
type recurringTestFixture struct {
	k         keeper.Keeper
	ctx       sdk.Context
	bk        *mockBankKeeperCommons
	ms        types.MsgServer
	council   sdk.AccAddress
	recipient sdk.AccAddress
	now       int64
}

func setupRecurringFixture(t *testing.T) *recurringTestFixture {
	t.Helper()

	k, ctx, bk := setupCommonsKeeper(t)

	// Default params include the recurring-spend defaults; persist them
	// because the bare setupCommonsKeeper does not.
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))

	council := sdk.AccAddress([]byte("council_recurring___"))
	recipient := sdk.AccAddress([]byte("recipient_recurring_"))

	bk.balance[council.String()] = sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000_000_000)))

	maxSpendPerEpoch := math.NewInt(500_000_000)
	now := ctx.BlockTime().Unix()
	group := types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &maxSpendPerEpoch,
		ActivationTime:        0,
		CurrentTermExpiration: now + 365*86400, // ~1 year
	}
	require.NoError(t, k.Groups.Set(ctx, "TestCouncil", group))
	require.NoError(t, k.PolicyToName.Set(ctx, council.String(), "TestCouncil"))

	return &recurringTestFixture{
		k:         k,
		ctx:       ctx,
		bk:        bk,
		ms:        keeper.NewMsgServerImpl(k),
		council:   council,
		recipient: recipient,
		now:       now,
	}
}

// scheduleDefault registers a schedule paying 100 uspark every day for ~30
// days, starting at now+10s so the first claim is admissible at
// now+10s+86400s.
func (f *recurringTestFixture) scheduleDefault(t *testing.T) *types.MsgScheduleRecurringSpendResponse {
	t.Helper()
	resp, err := f.ms.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient.String(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
		PeriodSeconds:   86400,
		StartTime:       f.now + 10,
		EndTime:         f.now + 10 + 30*86400,
		Note:            "monthly stipend",
	})
	require.NoError(t, err)
	require.NotZero(t, resp.Id)
	return resp
}

func TestScheduleRecurringSpend_Success(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, f.council.String(), rs.Authority)
	require.Equal(t, f.recipient.String(), rs.Recipient)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE, rs.Status)
	require.Equal(t, int64(86400), rs.PeriodSeconds)
	// last_claim_advance is anchored to start_time so first claim happens
	// at start + period, not at scheduling time.
	require.Equal(t, rs.StartTime, rs.LastClaimAdvance)

	count, err := f.k.ActiveRecurringSpendCount.Get(f.ctx, f.council.String())
	require.NoError(t, err)
	require.Equal(t, uint32(1), count)
}

func TestScheduleRecurringSpend_Validation(t *testing.T) {
	tests := []struct {
		name string
		mut  func(m *types.MsgScheduleRecurringSpend)
		err  error
	}{
		{
			name: "period below minimum",
			mut:  func(m *types.MsgScheduleRecurringSpend) { m.PeriodSeconds = 30 },
			err:  types.ErrRecurringSpendInvalidPeriod,
		},
		{
			name: "end before start",
			mut:  func(m *types.MsgScheduleRecurringSpend) { m.EndTime = m.StartTime - 1 },
			err:  types.ErrRecurringSpendInvalidWindow,
		},
		{
			name: "duration over cap",
			mut: func(m *types.MsgScheduleRecurringSpend) {
				m.EndTime = m.StartTime + 10*types.DefaultMaxRecurringDurationSeconds
			},
			err: types.ErrRecurringSpendInvalidWindow,
		},
		{
			name: "window shorter than one period",
			mut: func(m *types.MsgScheduleRecurringSpend) {
				m.EndTime = m.StartTime + m.PeriodSeconds - 1
			},
			err: types.ErrRecurringSpendInvalidWindow,
		},
		{
			name: "start in the past",
			mut: func(m *types.MsgScheduleRecurringSpend) {
				m.StartTime = m.StartTime - 1_000_000
			},
			err: types.ErrRecurringSpendInvalidWindow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := setupRecurringFixture(t)
			msg := &types.MsgScheduleRecurringSpend{
				Authority:       f.council.String(),
				Recipient:       f.recipient.String(),
				AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
				PeriodSeconds:   86400,
				StartTime:       f.now + 10,
				EndTime:         f.now + 10 + 30*86400,
			}
			tc.mut(msg)
			_, err := f.ms.ScheduleRecurringSpend(f.ctx, msg)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestScheduleRecurringSpend_UnknownAuthority(t *testing.T) {
	f := setupRecurringFixture(t)
	attacker := sdk.AccAddress([]byte("attacker_recurring__"))
	_, err := f.ms.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       attacker.String(),
		Recipient:       f.recipient.String(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
		PeriodSeconds:   86400,
		StartTime:       f.now + 10,
		EndTime:         f.now + 10 + 30*86400,
	})
	require.ErrorIs(t, err, types.ErrGroupNotFound)
}

func TestClaimRecurringSpend_HappyPath(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	// Advance past the first claim window.
	f.ctx = f.ctx.WithBlockTime(time.Unix(f.now+10+86400+1, 0))

	claimResp, err := f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.NoError(t, err)
	require.Equal(t, uint64(1), claimResp.ClaimNumber)

	bal := f.bk.balance[f.recipient.String()]
	require.Equal(t, math.NewInt(100), bal.AmountOf("uspark"))

	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, uint64(1), rs.ClaimsMade)
	// Logical clock advanced by exactly one period from start_time.
	require.Equal(t, rs.StartTime+rs.PeriodSeconds, rs.LastClaimAdvance)
}

func TestClaimRecurringSpend_NotDue(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	// Block time has not yet reached start_time + period_seconds.
	f.ctx = f.ctx.WithBlockTime(time.Unix(f.now+5, 0))

	_, err := f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendNotDue)
}

func TestClaimRecurringSpend_WrongRecipient(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)
	stranger := sdk.AccAddress([]byte("stranger_recurring__"))

	f.ctx = f.ctx.WithBlockTime(time.Unix(f.now+10+86400+1, 0))

	_, err := f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: stranger.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendUnauthorized)
}

func TestClaimRecurringSpend_TermExpiredAutoPauses(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	// Expire the council's term BEFORE the claim is attempted. The
	// schedule remains ACTIVE in storage; CheckSpendPreconditions returns
	// ErrGroupExpired so the claim is rejected. When the term is renewed
	// the same path succeeds again — that's the "auto-pause / auto-resume"
	// semantic.
	group, err := f.k.Groups.Get(f.ctx, "TestCouncil")
	require.NoError(t, err)
	group.CurrentTermExpiration = f.now + 1
	require.NoError(t, f.k.Groups.Set(f.ctx, "TestCouncil", group))

	f.ctx = f.ctx.WithBlockTime(time.Unix(f.now+10+86400+1, 0))

	_, err = f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrGroupExpired)

	// Schedule is still ACTIVE in storage — not auto-canceled.
	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE, rs.Status)

	// Renew the term and a claim now succeeds.
	group.CurrentTermExpiration = f.now + 365*86400
	require.NoError(t, f.k.Groups.Set(f.ctx, "TestCouncil", group))

	_, err = f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.NoError(t, err)
}

func TestClaimRecurringSpend_CatchUpRespectsRateLimit(t *testing.T) {
	// Schedule pays 200 uspark/day for many days; the council's
	// max_spend_per_epoch is 300 uspark. Two catch-up claims in the same
	// epoch should succeed (200 + 200 > 300? yes — so the *second* must
	// fail with rate-limit). This proves recurring claims don't bypass
	// the per-epoch ceiling.
	k, ctx, bk := setupCommonsKeeper(t)
	require.NoError(t, k.Params.Set(ctx, types.DefaultParams()))
	ms := keeper.NewMsgServerImpl(k)

	council := sdk.AccAddress([]byte("rl_council__________"))
	recipient := sdk.AccAddress([]byte("rl_recipient________"))
	bk.balance[council.String()] = sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(1_000_000)))

	limit := math.NewInt(300)
	now := ctx.BlockTime().Unix()
	require.NoError(t, k.Groups.Set(ctx, "RLCouncil", types.Group{
		PolicyAddress:         council.String(),
		MaxSpendPerEpoch:      &limit,
		CurrentTermExpiration: now + 365*86400,
	}))
	require.NoError(t, k.PolicyToName.Set(ctx, council.String(), "RLCouncil"))

	resp, err := ms.ScheduleRecurringSpend(ctx, &types.MsgScheduleRecurringSpend{
		Authority:       council.String(),
		Recipient:       recipient.String(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(200))),
		PeriodSeconds:   86400,
		StartTime:       now + 1,
		EndTime:         now + 1 + 30*86400,
	})
	require.NoError(t, err)

	// Time-travel three periods so the recipient could catch up three
	// claims, all landing in the same epoch.
	ctx = ctx.WithBlockTime(time.Unix(now+1+3*86400+1, 0))

	_, err = ms.ClaimRecurringSpend(ctx, &types.MsgClaimRecurringSpend{Recipient: recipient.String(), Id: resp.Id})
	require.NoError(t, err)

	// Second claim in the same epoch tips cumulative past 300 → reject.
	_, err = ms.ClaimRecurringSpend(ctx, &types.MsgClaimRecurringSpend{Recipient: recipient.String(), Id: resp.Id})
	require.ErrorIs(t, err, types.ErrRateLimitExceeded)
}

func TestCancelRecurringSpend(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	_, err := f.ms.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: f.council.String(),
		Id:        resp.Id,
	})
	require.NoError(t, err)

	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_CANCELED, rs.Status)

	// Active count decremented.
	_, err = f.k.ActiveRecurringSpendCount.Get(f.ctx, f.council.String())
	require.Error(t, err) // Map removes the key on drop-to-zero.

	// Subsequent claim refuses.
	f.ctx = f.ctx.WithBlockTime(time.Unix(f.now+10+86400+1, 0))
	_, err = f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive)
}

func TestCancelRecurringSpend_UnauthorizedAuthority(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	other := sdk.AccAddress([]byte("other_council_______"))
	_, err := f.ms.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: other.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendUnauthorized)
}

func TestDeclineRecurringSpend(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	_, err := f.ms.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.NoError(t, err)

	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_RECIPIENT_DECLINED, rs.Status)
}

func TestScheduleRecurringSpend_CapEnforcement(t *testing.T) {
	f := setupRecurringFixture(t)
	// Shrink the cap to make this cheap to test.
	params := types.DefaultParams()
	params.MaxActiveRecurringSpendsPerGroup = 2
	require.NoError(t, f.k.Params.Set(f.ctx, params))

	mkMsg := func() *types.MsgScheduleRecurringSpend {
		return &types.MsgScheduleRecurringSpend{
			Authority:       f.council.String(),
			Recipient:       f.recipient.String(),
			AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
			PeriodSeconds:   86400,
			StartTime:       f.now + 10,
			EndTime:         f.now + 10 + 30*86400,
		}
	}

	_, err := f.ms.ScheduleRecurringSpend(f.ctx, mkMsg())
	require.NoError(t, err)
	_, err = f.ms.ScheduleRecurringSpend(f.ctx, mkMsg())
	require.NoError(t, err)
	_, err = f.ms.ScheduleRecurringSpend(f.ctx, mkMsg())
	require.ErrorIs(t, err, types.ErrRecurringSpendCapReached)
}

// TestScheduleRecurringSpend_ZeroAmount — a schedule with a zero or empty
// amount_per_period is a logic bug (would commit infinitely many no-op
// claims) and must reject before any storage is touched.
func TestScheduleRecurringSpend_ZeroAmount(t *testing.T) {
	f := setupRecurringFixture(t)
	_, err := f.ms.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient.String(),
		AmountPerPeriod: sdk.NewCoins(), // empty
		PeriodSeconds:   86400,
		StartTime:       f.now + 10,
		EndTime:         f.now + 10 + 30*86400,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "amount_per_period")
}

// TestScheduleRecurringSpend_NoteOverCap — note >256 chars is rejected.
// The cap exists to bound state bloat; without it a council could store
// arbitrarily long payloads in the schedule note field.
func TestScheduleRecurringSpend_NoteOverCap(t *testing.T) {
	f := setupRecurringFixture(t)
	huge := make([]byte, keeper.MaxRecurringSpendNoteLen+1)
	for i := range huge {
		huge[i] = 'x'
	}
	_, err := f.ms.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient.String(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
		PeriodSeconds:   86400,
		StartTime:       f.now + 10,
		EndTime:         f.now + 10 + 30*86400,
		Note:            string(huge),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "note exceeds")
}

// TestClaimRecurringSpend_CompletionFlip — drain the schedule until the
// next advance would step past end_time, verify status flips to COMPLETED
// and ActiveRecurringSpendCount is dropped. This is the COMPLETED path
// that no other test exercises.
func TestClaimRecurringSpend_CompletionFlip(t *testing.T) {
	f := setupRecurringFixture(t)

	// 3-period schedule.
	resp, err := f.ms.ScheduleRecurringSpend(f.ctx, &types.MsgScheduleRecurringSpend{
		Authority:       f.council.String(),
		Recipient:       f.recipient.String(),
		AmountPerPeriod: sdk.NewCoins(sdk.NewCoin("uspark", math.NewInt(100))),
		PeriodSeconds:   86400,
		StartTime:       f.now + 10,
		EndTime:         f.now + 10 + 3*86400, // exactly 3 periods
	})
	require.NoError(t, err)

	// Advance well past the third claim window so all three are immediately admissible.
	f.ctx = f.ctx.WithBlockTime(time.Unix(f.now+10+3*86400+1, 0))

	for i := 1; i <= 3; i++ {
		_, err := f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
			Recipient: f.recipient.String(),
			Id:        resp.Id,
		})
		require.NoError(t, err, "claim %d failed", i)
	}

	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, uint64(3), rs.ClaimsMade)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED, rs.Status,
		"third claim consumes the last period — status must flip to COMPLETED")

	// Active counter dropped back to zero (and the key removed entirely).
	_, err = f.k.ActiveRecurringSpendCount.Get(f.ctx, f.council.String())
	require.Error(t, err, "active count should have been removed on completion")

	// A fourth claim must reject because the schedule is no longer ACTIVE.
	_, err = f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive)
}

// TestClaimRecurringSpend_WindowClosedDefense — the claim handler has a
// defensive branch: if a schedule's logical clock has already advanced
// past end_time - period_seconds (so the next claim would step past the
// window) but status is still ACTIVE, the handler self-heals by flipping
// to COMPLETED and rejecting with ErrRecurringSpendWindowClosed.
//
// This state is unreachable through normal use (the success path flips
// status on the final claim) but matters for migrations, malformed
// genesis imports, and any future code path that mutates last_claim_advance
// without going through the success branch. Constructing the bad state
// directly verifies the safety net.
func TestClaimRecurringSpend_WindowClosedDefense(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	rs, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	// Manually advance the logical clock past the last valid claim window
	// without flipping status — simulating a malformed import.
	rs.LastClaimAdvance = rs.EndTime - 1
	require.NoError(t, f.k.RecurringSpends.Set(f.ctx, rs.Id, rs))

	// Block time past nextEligible (= LastClaimAdvance + period_seconds)
	// so we don't trip the NotDue check before reaching WindowClosed.
	f.ctx = f.ctx.WithBlockTime(time.Unix(rs.LastClaimAdvance+rs.PeriodSeconds+10, 0))

	_, err = f.ms.ClaimRecurringSpend(f.ctx, &types.MsgClaimRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendWindowClosed)

	healed, err := f.k.GetRecurringSpend(f.ctx, resp.Id)
	require.NoError(t, err)
	require.Equal(t, types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED, healed.Status)
	_, err = f.k.ActiveRecurringSpendCount.Get(f.ctx, f.council.String())
	require.Error(t, err, "active count should drop on defensive self-heal")
}

// TestCancelRecurringSpend_DoubleCancel — cancelling an already-cancelled
// schedule must reject with ErrRecurringSpendInactive (not silently succeed).
// Catches a regression where cancel forgets to check status first and
// double-decrements the active counter, drifting it negative.
func TestCancelRecurringSpend_DoubleCancel(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	_, err := f.ms.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: f.council.String(),
		Id:        resp.Id,
	})
	require.NoError(t, err)

	_, err = f.ms.CancelRecurringSpend(f.ctx, &types.MsgCancelRecurringSpend{
		Authority: f.council.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive)
}

// TestDeclineRecurringSpend_UnauthorizedRecipient — only the recipient on
// file can decline; a stranger's MsgDeclineRecurringSpend is rejected with
// ErrRecurringSpendUnauthorized.
func TestDeclineRecurringSpend_UnauthorizedRecipient(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	stranger := sdk.AccAddress([]byte("stranger_decline____"))
	_, err := f.ms.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: stranger.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendUnauthorized)
}

// TestDeclineRecurringSpend_DoubleDecline — declining an already-declined
// schedule rejects.
func TestDeclineRecurringSpend_DoubleDecline(t *testing.T) {
	f := setupRecurringFixture(t)
	resp := f.scheduleDefault(t)

	_, err := f.ms.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.NoError(t, err)

	_, err = f.ms.DeclineRecurringSpend(f.ctx, &types.MsgDeclineRecurringSpend{
		Recipient: f.recipient.String(),
		Id:        resp.Id,
	})
	require.ErrorIs(t, err, types.ErrRecurringSpendInactive)
}

// TestGetRecurringSpend_NotFound — the keeper-level getter returns the
// typed ErrRecurringSpendNotFound, not raw collections.ErrNotFound. The
// query handler then maps that into codes.NotFound; client code relies on
// the distinction.
func TestGetRecurringSpend_NotFound(t *testing.T) {
	f := setupRecurringFixture(t)
	_, err := f.k.GetRecurringSpend(f.ctx, 9999)
	require.ErrorIs(t, err, types.ErrRecurringSpendNotFound)
}
