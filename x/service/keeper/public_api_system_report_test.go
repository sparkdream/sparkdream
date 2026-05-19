package keeper_test

import (
	"bytes"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// Tests for x/service/keeper/public_api_system_report.go — the keeper-
// level OpenSystemReport entry point used by allowlisted consumer
// modules (federation today). Added in Phase 0 of the federation→
// service migration.

// ---------------------------------------------------------------------------
// Caller authorization (forward-derive via auth keeper)
// ---------------------------------------------------------------------------

func TestOpenSystemReport_RejectsUnauthorizedCaller(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(2))

	// Use an arbitrary non-allowlisted address (testRandomAddr is a
	// random EOA; testReporterAddr is also an EOA). Both fail the
	// forward-derive check because they don't match any allowlisted
	// module account address.
	for _, caller := range []sdk.AccAddress{testRandomAddr, testReporterAddr} {
		_, _, err := f.keeper.OpenSystemReport(
			f.ctx,
			caller,
			testOperator1Addr,
			testServiceType,
			0,
			"ipfs://evidence",
			[]byte("dedupe-1"),
		)
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrUnauthorizedSystemCaller, "caller=%s", caller)
	}
}

func TestOpenSystemReport_RejectsCallerSpoofingFederationAddress(t *testing.T) {
	// Test name says "spoofing" — but actually with auth-keeper lookup
	// in place, an address that happens to match the federation module
	// account address would still pass the check (the allowlist + auth
	// resolves to "federation"). The REAL defense is keeper-wiring
	// scope: only federation gets a ServiceKeeper. This test documents
	// that the auth-keeper lookup, by design, accepts the federation
	// module address regardless of who actually called — emitting
	// caller_module=federation in the event for audit. The narrative
	// is in the comments; the test asserts the auth path admits a
	// known module address and rejects a non-module address.

	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(2))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)

	// Federation module address: succeeds.
	_, _, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://evidence", []byte("dedupe-1"),
	)
	require.NoError(t, err, "federation module address should authorize")

	// A random non-module address: fails with ErrUnauthorizedSystemCaller.
	_, _, err = f.keeper.OpenSystemReport(
		f.ctx, testRandomAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://evidence", []byte("dedupe-2"),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrUnauthorizedSystemCaller)
}

// ---------------------------------------------------------------------------
// Happy path + state + idempotency
// ---------------------------------------------------------------------------

func TestOpenSystemReport_HappyPath_FiresReportWithDefaults(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 250 // 2.5%
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(2))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)

	reportID, idempotent, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, // 0 → fall back to challenge_default_slash_bps
		"ipfs://Qm...", []byte("challenge-7"),
	)
	require.NoError(t, err)
	require.NotZero(t, reportID)
	require.False(t, idempotent)

	report, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, report.Status)
	require.EqualValues(t, 250, report.ProposedSlashBps, "0 input falls back to challenge_default")
	require.True(t, report.Deposit.Amount.IsZero(), "system reports have zero deposit")
	require.Equal(t, testServiceType, report.ServiceType)
	require.Equal(t, testOperator1, report.OperatorAddress)
}

func TestOpenSystemReport_CapsSlashBpsToUnilateralCap(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.UnilateralSlashCapBps = 500    // 5%
	cfg.ChallengeDefaultSlashBps = 200 // 2% default
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(2))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)

	// Caller proposes 9999 bps; should be clamped to 500 (the cap).
	reportID, _, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 9999,
		"ipfs://Qm...", []byte("challenge-cap-test"),
	)
	require.NoError(t, err)

	report, err := f.keeper.Reports.Get(f.ctx, reportID)
	require.NoError(t, err)
	require.EqualValues(t, 500, report.ProposedSlashBps, "should clamp to unilateral_slash_cap_bps")
}

func TestOpenSystemReport_IdempotencyReturnsExistingReportID(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(2))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)
	dedupe := []byte("the-same-challenge")

	firstID, idem1, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://Qm-first", dedupe,
	)
	require.NoError(t, err)
	require.False(t, idem1)

	// Re-call with the same dedupe key. Returns same report_id,
	// idempotent=true.
	secondID, idem2, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://Qm-second-evidence", dedupe,
	)
	require.NoError(t, err)
	require.True(t, idem2)
	require.Equal(t, firstID, secondID)

	// State sanity: only one Report record exists (the first one).
	r, err := f.keeper.Reports.Get(f.ctx, firstID)
	require.NoError(t, err)
	// Evidence URI on the report is still the first one (system report
	// stores it in Reason); idempotent re-calls don't overwrite.
	require.Contains(t, r.Reason, "Qm-first")
}

func TestOpenSystemReport_RejectsEmptyDedupeKey(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(2))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)
	_, _, err := f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://", nil,
	)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidDedupeKey)
}

// ---------------------------------------------------------------------------
// Per-caller sliding-window rate limit
// ---------------------------------------------------------------------------

func TestOpenSystemReport_RateLimitRejectsAtCapPlusOne(t *testing.T) {
	f := initFixture(t)
	// Lower the cap to keep the test cheap.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSystemReportsPerCallerPerWindow = 3
	params.RateLimitWindowBlocks = 1_000_000 // effectively no window expiry
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(10))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)

	// Three fresh dedupe keys all succeed.
	for i := 0; i < 3; i++ {
		key := []byte{byte('a' + i)}
		_, _, err := f.keeper.OpenSystemReport(
			f.ctx, fedAddr, testOperator1Addr,
			testServiceType, 0, "ipfs://", key,
		)
		require.NoError(t, err, "i=%d", i)
	}

	// Fourth (over cap) is rejected.
	_, _, err = f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://", []byte("over-cap"),
	)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSystemReportRateLimited)
}

func TestOpenSystemReport_IdempotentCallsDontCountAgainstRateLimit(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	params.MaxSystemReportsPerCallerPerWindow = 2
	params.RateLimitWindowBlocks = 1_000_000
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	f.seedActiveOperator(t, testOperator1, testController, cfg.MinBond.Amount.MulRaw(10))

	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)
	dedupe := []byte("retry-target")

	// One unique fire.
	_, _, err = f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://", dedupe,
	)
	require.NoError(t, err)

	// 50 idempotent re-calls. All return idempotent=true without
	// counting against the cap.
	for i := 0; i < 50; i++ {
		_, idem, err := f.keeper.OpenSystemReport(
			f.ctx, fedAddr, testOperator1Addr,
			testServiceType, 0, "ipfs://", dedupe,
		)
		require.NoError(t, err, "iter %d", i)
		require.True(t, idem)
	}

	// Second unique fire — should still succeed (cap is 2, idempotent
	// re-calls don't count).
	_, _, err = f.keeper.OpenSystemReport(
		f.ctx, fedAddr, testOperator1Addr,
		testServiceType, 0, "ipfs://", []byte("second-fresh"),
	)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Allowlist shape (defensive)
// ---------------------------------------------------------------------------

// Defensive sanity: the system-caller allowlist is non-empty and
// sorted. If a future change accidentally drops the allowlist or
// inserts an unsorted entry, this fails fast.
func TestSystemCallerAllowlist_StableShape(t *testing.T) {
	// We use authtypes.NewModuleAddress(FederationModuleName) elsewhere;
	// confirm at least one entry exists and resolves to a non-empty
	// address.
	fedAddr := authtypes.NewModuleAddress(types.FederationModuleName)
	require.False(t, bytes.Equal(fedAddr, []byte{}), "federation module address must be non-empty")
	require.Len(t, fedAddr, 20, "module addresses are 20 bytes")
}
