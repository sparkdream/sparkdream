package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// Regression tests for the 2026-10 x/service review (SVC-02 .. SVC-10).

// SVC-05: every jury case is opened with its jury seated, the parties
// excluded, and a voting deadline in the first half of max_escalated_blocks
// so a tallied verdict can still be relayed before the case times out.
func TestJuryCasesAreSeatedWithPartiesExcluded(t *testing.T) {
	f := initFixture(t)
	params, _ := f.keeper.Params.Get(f.ctx)
	start := f.sdkCtx().BlockHeight()

	reportID := f.seedEscalatedReport(t, 300)
	require.Len(t, f.repKeeper.SeatedExcludes, 1)
	require.ElementsMatch(t, []string{testOperator1, testReporter, testController}, f.repKeeper.SeatedExcludes[0])
	require.Equal(t, start+params.MaxEscalatedBlocks/2, f.repKeeper.SeatedDeadlines[0])
	r, _ := f.keeper.Reports.Get(f.ctx, reportID)
	require.NotZero(t, r.JuryCaseId)

	g := initFixture(t)
	contested := g.seedResolvedT1ReportAndSlash(t, 200)
	_, err := g.msgServer.ContestSlash(g.ctx, &types.MsgContestSlash{Operator: testOperator1, ServiceType: testServiceType, ReportId: contested})
	require.NoError(t, err)
	require.Len(t, g.repKeeper.SeatedExcludes, 1)
	require.ElementsMatch(t, []string{testOperator1, testReporter, testController}, g.repKeeper.SeatedExcludes[0])
}

// SVC-02: SystemReportDedup is pruned once its report is terminal and the
// retention has passed, and kept (re-queued) while the report is open; the
// map and the rate-limit buffers round-trip through genesis.
func TestSystemReportDedupPrunedAndExported(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount.MulRaw(2))
	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)

	reportID, _, err := f.keeper.OpenSystemReport(f.ctx, fedAddr, testOperator1Addr, testServiceType, 0, "uri", []byte("challenge-1"))
	require.NoError(t, err)
	pk := collections.Join(types.FederationModuleName, []byte("challenge-1"))

	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gs.SystemReportDedup, 1)
	require.Len(t, gs.SystemReportRateLimits, 1)
	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	has, _ := g.keeper.SystemReportDedup.Has(g.ctx, pk)
	require.True(t, has)

	params, _ := f.keeper.Params.Get(f.ctx)
	retention := params.MaxPendingBlocks + params.MaxEscalatedBlocks
	filed := f.sdkCtx().BlockHeight()

	// Still PENDING at the retention mark: kept. (Make the sweep see it
	// without auto-dismissing it first by keeping it PENDING by hand.)
	f.withBlockHeight(filed + retention)
	r, _ := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, f.keeper.PendingReportsQueue.Remove(f.ctx, collections.Join(r.FiledAt, reportID)))
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	has, _ = f.keeper.SystemReportDedup.Has(f.ctx, pk)
	require.True(t, has, "open report keeps its dedup entry")

	// Terminal: pruned at the next mark.
	r.Status = types.ReportStatus_REPORT_STATUS_AUTO_DISMISSED
	require.NoError(t, f.keeper.Reports.Set(f.ctx, reportID, r))
	f.withBlockHeight(filed + 2*retention)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	has, _ = f.keeper.SystemReportDedup.Has(f.ctx, pk)
	require.False(t, has, "terminal report's dedup entry is pruned")
}

// SVC-03: x/service's own liveness reports do not consume, and are not
// blocked by, the per-caller system-report rate limit.
func TestServiceLivenessReportsNotRateLimited(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBondAmount.MulRaw(2))
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxSystemReportsPerCallerPerWindow = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	svcAddr := authtypes.NewModuleAddress(types.ModuleName)
	for i := 0; i < 3; i++ {
		_, _, err := f.keeper.OpenSystemReportAsServiceForTest(f.ctx, svcAddr, testOperator1Addr, testServiceType, []byte{byte(i)})
		require.NoError(t, err, "liveness report %d", i)
	}
	has, _ := f.keeper.SystemReportRateLimit.Has(f.ctx, types.ModuleName)
	require.False(t, has)
}

// SVC-07: an expired short-grace operator is forced into UNBONDING even
// when not-yet-expired long-grace operators sit ahead of it in the queue.
func TestUnderfundedSweepSkipsInGraceEntries(t *testing.T) {
	f := initFixture(t)
	short := f.seedServiceType(t)
	short.UnderfundedGraceBlocks = 10
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, short.ServiceType, short))
	long := short
	long.ServiceType = "long-grace"
	long.UnderfundedGraceBlocks = 1_000_000
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, long.ServiceType, long))
	params, _ := f.keeper.Params.Get(f.ctx)
	params.EndblockerSweepLimit = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	h := f.sdkCtx().BlockHeight()
	// Long-grace operator queued first (earlier underfunded_since).
	mk := func(addr sdk.AccAddress, st string, since int64) {
		a, _ := testAddrCodec.BytesToString(addr)
		op := types.Operator{Address: a, ServiceType: st, Controller: testController,
			BondAmount: math.NewInt(1), Status: types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, UnderfundedSince: since}
		require.NoError(t, f.keeper.PutOperator(f.ctx, op))
		require.NoError(t, f.keeper.UnderfundedQueue.Set(f.ctx, collections.Join3(since, addr.Bytes(), st)))
	}
	mk(testRandomAddr, long.ServiceType, h)
	mk(testOperator1Addr, short.ServiceType, h+1)

	f.withBlockHeight(h + 100)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	op, ok := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), short.ServiceType)
	require.True(t, ok)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNBONDING, op.Status)
	op, _ = f.keeper.GetOperator(f.ctx, testRandomAddr.Bytes(), long.ServiceType)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, op.Status)

	// SVC-07: the queue is keyed by grace deadline, so the in-grace entry is
	// not even inside the swept range, and the unbonded one left the index.
	due, err := f.keeper.UnderfundedDueAt.Get(f.ctx, collections.Join(testRandomAddr.Bytes(), long.ServiceType))
	require.NoError(t, err)
	require.Equal(t, h+1_000_000, due)
	has, err := f.keeper.UnderfundedDueAt.Has(f.ctx, collections.Join(testOperator1Addr.Bytes(), short.ServiceType))
	require.NoError(t, err)
	require.False(t, has)

	// A longer grace set after filing re-files the entry at its deadline
	// instead of unbonding early.
	other := sdk.AccAddress([]byte("svc07_regrace_______"))
	mk(other, short.ServiceType, h+100)
	short.UnderfundedGraceBlocks = 500
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, short.ServiceType, short))
	f.withBlockHeight(h + 111)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	op, _ = f.keeper.GetOperator(f.ctx, other.Bytes(), short.ServiceType)
	require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED, op.Status)
	due, err = f.keeper.UnderfundedDueAt.Get(f.ctx, collections.Join(other.Bytes(), short.ServiceType))
	require.NoError(t, err)
	require.Equal(t, h+600, due)
}

// SVC-09: list queries apply default_pagination_limit and reject a limit
// above max_pagination_limit (§9, ErrPaginationLimitExceeded).
func TestListQueriesEnforcePagination(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	params, _ := f.keeper.Params.Get(f.ctx)
	params.DefaultPaginationLimit = 1
	params.MaxPaginationLimit = 2
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))
	for _, a := range []string{testOperator1, testRandom, testReporter} {
		f.seedActiveOperator(t, a, testController, math.NewInt(2_000_000))
	}
	resp, err := f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Operators, 1)
	resp, err = f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{Pagination: &query.PageRequest{Limit: 2}})
	require.NoError(t, err)
	require.Len(t, resp.Operators, 2)
	_, err = f.queryServer.Operators(f.ctx, &types.QueryOperatorsRequest{Pagination: &query.PageRequest{Limit: 50}})
	require.ErrorContains(t, err, "pagination limit exceeded")
}

// SVC-10 (latent): when a contested escrow is still held at jury time,
// ACCEPT takes the escrow as the slash (not bps of the escrow), and REJECT
// returns it and reverts UNDERFUNDED to ACTIVE.
func TestJuryResolutionWithHeldEscrow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict types.JuryVerdict
		bps     uint32
	}{
		{"accept", types.JuryVerdict_JURY_VERDICT_ACCEPT, 300},
		{"reject", types.JuryVerdict_JURY_VERDICT_REJECT, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)
			reportID := f.seedEscalatedReport(t, 300)
			op, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
			escrowAmt := op.BondAmount.MulRaw(300).QuoRaw(10_000)
			op.BondAmount = op.BondAmount.Sub(escrowAmt)
			op.Status = types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
			op.UnderfundedSince = f.sdkCtx().BlockHeight()
			cfg, _ := f.keeper.ServiceTypes.Get(f.ctx, testServiceType)
			cfg.MinBondAmount = op.BondAmount.AddRaw(1) // underfunded by the escrow
			require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, testServiceType, cfg))
			require.NoError(t, f.keeper.PutOperator(f.ctx, op))
			require.NoError(t, f.keeper.Tier1Escrow.Set(f.ctx, 77, types.Tier1EscrowEntry{
				EscrowId: 77, ReportId: reportID, OperatorAddress: testOperator1, ServiceType: testServiceType,
				Amount: escrowAmt, ReleaseAt: f.sdkCtx().BlockHeight() + 1000,
			}))
			require.NoError(t, f.keeper.Tier1EscrowByOperator.Set(f.ctx, collections.Join3(testOperator1Addr.Bytes(), testServiceType, uint64(77))))

			if tc.verdict == types.JuryVerdict_JURY_VERDICT_REJECT {
				f.repKeeper.GetJuryVerdictNameFn = func(context.Context, uint64) (string, error) {
					return types.JuryVerdictRejectChallenge, nil
				}
			}
			_, err := f.msgServer.ResolveReportByJury(f.ctx, &types.MsgResolveReportByJury{
				JuryAuthority: testCouncil, ReportId: reportID, Verdict: tc.verdict, SlashBps: tc.bps,
			})
			require.NoError(t, err)
			r, _ := f.keeper.Reports.Get(f.ctx, reportID)
			after, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr.Bytes(), testServiceType)
			if tc.verdict == types.JuryVerdict_JURY_VERDICT_ACCEPT {
				require.Equal(t, escrowAmt, r.SlashAmount, "ACCEPT consumes the escrow as-is")
				require.Equal(t, op.BondAmount, after.BondAmount)
			} else {
				require.True(t, r.SlashAmount.IsZero())
				require.Equal(t, op.BondAmount.Add(escrowAmt), after.BondAmount)
				require.Equal(t, types.OperatorStatus_OPERATOR_STATUS_ACTIVE, after.Status)
			}
		})
	}
}
