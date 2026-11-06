package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

func validSimCfg() types.ServiceTypeConfig {
	return types.ServiceTypeConfig{
		ServiceType:            testServiceType,
		Description:            "unit-test config",
		MinBondAmount:          math.NewInt(1_000_000),
		UnbondingPeriodBlocks:  20,
		UnilateralSlashCapBps:  500,
		Tier1WindowBlocks:      1000,
		Tier1AggregateCapBps:   1500,
		Tier1CooldownBlocks:    10,
		UnderfundedGraceBlocks: 10,
		Enabled:                true,
	}
}

func TestMsgUpdateServiceTypeConfig_Create(t *testing.T) {
	f := initFixture(t)

	_, err := f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: f.authorityStr,
		Config:    validSimCfg(),
	})
	require.NoError(t, err)

	stored, err := f.keeper.ServiceTypes.Get(f.ctx, testServiceType)
	require.NoError(t, err)
	require.Equal(t, testServiceType, stored.ServiceType)
	require.True(t, stored.Enabled)
}

func TestMsgUpdateServiceTypeConfig_Update(t *testing.T) {
	f := initFixture(t)
	f.seedServiceType(t)

	updated := validSimCfg()
	updated.Enabled = false
	updated.Description = "disabled-for-test"

	_, err := f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: f.authorityStr,
		Config:    updated,
	})
	require.NoError(t, err)

	stored, err := f.keeper.ServiceTypes.Get(f.ctx, testServiceType)
	require.NoError(t, err)
	require.False(t, stored.Enabled)
	require.Equal(t, "disabled-for-test", stored.Description)
}

func TestMsgUpdateServiceTypeConfig_UnauthorizedSigner(t *testing.T) {
	f := initFixture(t)

	_, err := f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: testRandom,
		Config:    validSimCfg(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnauthorizedGovAuthority.Error())
}

func TestMsgUpdateServiceTypeConfig_InvalidAuthorityAddress(t *testing.T) {
	f := initFixture(t)

	_, err := f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: "not-bech32",
		Config:    validSimCfg(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), types.ErrUnauthorizedGovAuthority.Error())
}

func TestMsgUpdateServiceTypeConfig_InvalidConfig(t *testing.T) {
	f := initFixture(t)

	bad := validSimCfg()
	bad.UnilateralSlashCapBps = 0 // out of (0, 10000]

	_, err := f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: f.authorityStr,
		Config:    bad,
	})
	require.Error(t, err)
}

// SVC-104: a per-type unbonding period shorter than the report contest
// window is refused, in the handler and in genesis.
func TestMsgUpdateServiceTypeConfig_UnbondingShorterThanContestWindow(t *testing.T) {
	f := initFixture(t)
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)

	cfg := validSimCfg()
	cfg.UnbondingPeriodBlocks = params.ReportContestWindowBlocks - 1
	_, err = f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: f.authorityStr,
		Config:    cfg,
	})
	require.ErrorIs(t, err, types.ErrInvalidServiceTypeConfig)

	cfg.UnbondingPeriodBlocks = params.ReportContestWindowBlocks
	_, err = f.msgServer.UpdateServiceTypeConfig(f.ctx, &types.MsgUpdateServiceTypeConfig{
		Authority: f.authorityStr,
		Config:    cfg,
	})
	require.NoError(t, err)

	gs := types.DefaultGenesis()
	gs.ServiceTypes[0].UnbondingPeriodBlocks = gs.Params.ReportContestWindowBlocks - 1
	require.ErrorContains(t, gs.Validate(), "report_contest_window_blocks")
}
