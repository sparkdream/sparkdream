package types_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/service/types"
)

// Tests for ServiceTypeConfig.Validate (x/service/types/service_type_config_validation.go).

func TestServiceTypeConfigValidate_ChallengeDefaultBoundedByUnilateralCap(t *testing.T) {
	base := types.ServiceTypeConfig{
		ServiceType:            "valid-type",
		Description:            "x",
		MinBond:                sdk.NewCoin(types.BondDenom, math.NewInt(1_000_000)),
		UnbondingPeriodBlocks:  10,
		UnilateralSlashCapBps:  500, // 5%
		Tier1WindowBlocks:      1000,
		Tier1AggregateCapBps:   1500,
		Tier1CooldownBlocks:    5,
		UnderfundedGraceBlocks: 5,
		Enabled:                true,
		ReportTimeoutAction:    types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_DISMISS,
	}

	// Zero default is permitted.
	require.NoError(t, base.Validate())

	// Equal to cap is permitted.
	c := base
	c.ChallengeDefaultSlashBps = 500
	require.NoError(t, c.Validate())

	// One bps above cap is rejected.
	c.ChallengeDefaultSlashBps = 501
	err := c.Validate()
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidServiceTypeConfig)

	// > 10000 is rejected even when the cap is also > 10000 (which the
	// cap validator catches first; defensive).
	c.UnilateralSlashCapBps = 9000
	c.ChallengeDefaultSlashBps = 10001
	err = c.Validate()
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidServiceTypeConfig)
}

func TestServiceTypeConfigValidate_ReportTimeoutActionEnum(t *testing.T) {
	base := types.ServiceTypeConfig{
		ServiceType:            "valid-type",
		Description:            "x",
		MinBond:                sdk.NewCoin(types.BondDenom, math.NewInt(1_000_000)),
		UnbondingPeriodBlocks:  10,
		UnilateralSlashCapBps:  500,
		Tier1WindowBlocks:      1000,
		Tier1AggregateCapBps:   1500,
		Tier1CooldownBlocks:    5,
		UnderfundedGraceBlocks: 5,
		Enabled:                true,
	}

	for _, action := range []types.ReportTimeoutAction{
		types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_DISMISS,
		types.ReportTimeoutAction_REPORT_TIMEOUT_ACTION_ESCALATE,
	} {
		c := base
		c.ReportTimeoutAction = action
		require.NoError(t, c.Validate(), "action %v", action)
	}

	// Out-of-range value.
	c := base
	c.ReportTimeoutAction = types.ReportTimeoutAction(99)
	err := c.Validate()
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidServiceTypeConfig)
}
