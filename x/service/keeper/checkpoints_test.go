package keeper_test

import (
	"bytes"
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

var testRoot = bytes.Repeat([]byte{0xab}, 32)

// seedLivenessType seeds testServiceType with liveness tracking on.
func (f *fixture) seedLivenessType(t *testing.T, lag int64) types.ServiceTypeConfig {
	t.Helper()
	cfg := f.seedServiceType(t)
	cfg.ChallengeDefaultSlashBps = 100
	cfg.CheckpointMaxLagBlocks = lag
	cfg.AttestationQuorum = 1
	require.NoError(t, f.keeper.ServiceTypes.Set(f.ctx, cfg.ServiceType, cfg))
	return cfg
}

func (f *fixture) registerOperator(t *testing.T) {
	t.Helper()
	_, err := f.msgServer.RegisterOperator(f.ctx, &types.MsgRegisterOperator{
		Creator:     testOperator1,
		ServiceType: testServiceType,
		Controller:  testController,
		BondAmount:  math.NewInt(1_000_000),
		Metadata:    []byte(`{"v":1}`),
	})
	require.NoError(t, err)
}

func TestSubmitCheckpoint(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)
	f.registerOperator(t)
	f.withBlockHeight(200)

	submit := func(h int64, root []byte) error {
		_, err := f.msgServer.SubmitCheckpoint(f.ctx, &types.MsgSubmitCheckpoint{
			Operator: testOperator1, ServiceType: testServiceType, Height: h, Root: root,
		})
		return err
	}

	require.ErrorIs(t, submit(150, []byte{1}), types.ErrInvalidCheckpoint, "short root")
	require.ErrorIs(t, submit(201, testRoot), types.ErrInvalidCheckpoint, "height above the current block")
	require.ErrorIs(t, submit(0, testRoot), types.ErrInvalidCheckpoint, "zero height")
	_, err := f.msgServer.SubmitCheckpoint(f.ctx, &types.MsgSubmitCheckpoint{
		Operator: testOperator2, ServiceType: testServiceType, Height: 150, Root: testRoot,
	})
	require.ErrorIs(t, err, types.ErrOperatorNotFound)

	require.NoError(t, submit(150, testRoot))
	require.ErrorIs(t, submit(150, testRoot), types.ErrInvalidCheckpoint, "height must advance")
	require.ErrorIs(t, submit(149, testRoot), types.ErrInvalidCheckpoint, "height must advance")
	require.NoError(t, submit(180, testRoot))

	resp, err := f.queryServer.Checkpoint(f.ctx, &types.QueryCheckpointRequest{Operator: testOperator1, ServiceType: testServiceType})
	require.NoError(t, err)
	require.Equal(t, int64(180), resp.Checkpoint.Height)
	require.Equal(t, int64(200), resp.Checkpoint.SubmittedAt)
	require.Equal(t, testRoot, resp.Checkpoint.Root)

	list, err := f.queryServer.CheckpointsByServiceType(f.ctx, &types.QueryCheckpointsByServiceTypeRequest{ServiceType: testServiceType})
	require.NoError(t, err)
	require.Len(t, list.Checkpoints, 1)
	other, err := f.queryServer.CheckpointsByServiceType(f.ctx, &types.QueryCheckpointsByServiceTypeRequest{ServiceType: "other-type"})
	require.NoError(t, err)
	require.Empty(t, other.Checkpoints)

	_, err = f.queryServer.Checkpoint(f.ctx, &types.QueryCheckpointRequest{Operator: testOperator2, ServiceType: testServiceType})
	require.Error(t, err)

	// An unbonding operator no longer checkpoints.
	op, _ := f.keeper.GetOperator(f.ctx, testOperator1Addr, testServiceType)
	op.Status = types.OperatorStatus_OPERATOR_STATUS_UNBONDING
	require.NoError(t, f.keeper.PutOperator(f.ctx, op))
	require.ErrorIs(t, submit(190, testRoot), types.ErrOperatorNotActive)
}

func TestLivenessSweepFilesReportAndReschedules(t *testing.T) {
	f := initFixture(t) // block 100
	f.seedLivenessType(t, 50)
	f.registerOperator(t) // deadline 150

	countReports := func() int {
		n := 0
		require.NoError(t, f.keeper.Reports.Walk(f.ctx, nil, func(_ uint64, r types.Report) (bool, error) {
			if r.OperatorAddress == testOperator1 {
				n++
			}
			return false, nil
		}))
		return n
	}

	// Before the deadline nothing happens.
	f.withBlockHeight(149)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Zero(t, countReports())

	// A checkpoint moves the deadline: height 140 -> deadline 190.
	_, err := f.msgServer.SubmitCheckpoint(f.ctx, &types.MsgSubmitCheckpoint{
		Operator: testOperator1, ServiceType: testServiceType, Height: 140, Root: testRoot,
	})
	require.NoError(t, err)
	f.withBlockHeight(160)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Zero(t, countReports(), "caught up: no report")

	// The worker stalls: at the deadline a system report is filed by
	// x/service itself, with the type's default slash proposal.
	f.withBlockHeight(190)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Equal(t, 1, countReports())
	var report types.Report
	require.NoError(t, f.keeper.Reports.Walk(f.ctx, nil, func(_ uint64, r types.Report) (bool, error) {
		report = r
		return true, nil
	}))
	require.Equal(t, types.ReportStatus_REPORT_STATUS_PENDING, report.Status)
	require.Equal(t, uint32(100), report.ProposedSlashBps)
	require.Contains(t, report.Reason, "system:service:liveness:")

	// Rescheduled one full window later, not every block.
	f.withBlockHeight(191)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Equal(t, 1, countReports())
	f.withBlockHeight(240)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	require.Equal(t, 2, countReports())
}

func TestLivenessFollowsConfigChanges(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t) // liveness off
	f.registerOperator(t)

	f.withBlockHeight(10_000)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	reports := 0
	_ = f.keeper.Reports.Walk(f.ctx, nil, func(uint64, types.Report) (bool, error) { reports++; return false, nil })
	require.Zero(t, reports, "liveness off: never reported")

	// Turning liveness on schedules existing operators from their base
	// (registration at 100): deadline 100+50 is already past.
	cfg.ChallengeDefaultSlashBps = 100
	cfg.CheckpointMaxLagBlocks = 50
	_, err := f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{Authority: f.authorityStr, Config: cfg})
	require.NoError(t, err)
	require.NoError(t, f.keeper.EndBlocker(f.ctx))
	_ = f.keeper.Reports.Walk(f.ctx, nil, func(uint64, types.Report) (bool, error) { reports++; return false, nil })
	require.Equal(t, 1, reports)

	// Turning it off drops the deadline.
	cfg.CheckpointMaxLagBlocks = 0
	_, err = f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{Authority: f.authorityStr, Config: cfg})
	require.NoError(t, err)
	n := 0
	require.NoError(t, f.keeper.CheckpointDeadlines.Walk(f.ctx, nil, func(_ collections.Triple[int64, string, []byte]) (bool, error) { n++; return false, nil }))
	require.Zero(t, n)
}

func TestServiceTypeConfigAttestationValidation(t *testing.T) {
	f := initFixture(t)
	cfg := f.seedServiceType(t)

	bad := cfg
	bad.AttestationQuorum, bad.ElevatedAttestationQuorum = 3, 2
	require.ErrorIs(t, bad.Validate(), types.ErrInvalidServiceTypeConfig)

	bad = cfg
	bad.CheckpointMaxLagBlocks = -1
	require.ErrorIs(t, bad.Validate(), types.ErrInvalidServiceTypeConfig)

	bad = cfg
	bad.CheckpointMaxLagBlocks = 10 // no default slash bps
	require.ErrorIs(t, bad.Validate(), types.ErrInvalidServiceTypeConfig)

	good := cfg
	good.AttestationQuorum, good.ElevatedAttestationQuorum = 2, 3
	good.CheckpointMaxLagBlocks, good.ChallengeDefaultSlashBps = 10, 100
	require.NoError(t, good.Validate())
}

func TestContentScannerSeed(t *testing.T) {
	gen := types.DefaultGenesis()
	require.NoError(t, gen.Validate())
	var found *types.ServiceTypeConfig
	for i := range gen.ServiceTypes {
		if gen.ServiceTypes[i].ServiceType == types.ContentScannerServiceType {
			found = &gen.ServiceTypes[i]
		}
	}
	require.NotNil(t, found)
	require.True(t, found.Enabled)
	require.Equal(t, uint32(1), found.AttestationQuorum)
	require.Positive(t, found.CheckpointMaxLagBlocks)
	require.NoError(t, found.Validate())
}

func TestCheckpointGenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	f.seedLivenessType(t, 50)
	f.registerOperator(t)
	f.withBlockHeight(130)
	_, err := f.msgServer.SubmitCheckpoint(f.ctx, &types.MsgSubmitCheckpoint{
		Operator: testOperator1, ServiceType: testServiceType, Height: 120, Root: testRoot,
	})
	require.NoError(t, err)

	gen, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Len(t, gen.Checkpoints, 1)
	require.NoError(t, gen.Validate())

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gen))
	cp, ok := g.keeper.GetCheckpoint(g.ctx, testOperator1Addr, testServiceType)
	require.True(t, ok)
	require.Equal(t, int64(120), cp.Height)
	// The liveness deadline is rebuilt from the checkpoint: 120 + 50.
	deadline, err := g.keeper.CheckpointDeadlineByOperator.Get(g.ctx, collections.Join(testServiceType, []byte(testOperator1Addr)))
	require.NoError(t, err)
	require.Equal(t, int64(170), deadline)

	// Malformed checkpoints are rejected at genesis validation.
	bad := *gen
	bad.Checkpoints = append([]types.Checkpoint(nil), gen.Checkpoints...)
	bad.Checkpoints[0].Root = []byte{1}
	require.Error(t, bad.Validate())
	bad.Checkpoints = append(append([]types.Checkpoint(nil), gen.Checkpoints...), gen.Checkpoints...)
	require.Error(t, bad.Validate(), "duplicate")
}
