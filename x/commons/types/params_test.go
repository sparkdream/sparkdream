package types_test

import (
	"testing"

	"sparkdream/x/commons/types"

	"github.com/stretchr/testify/require"
)

func TestDefaultParams_Valid(t *testing.T) {
	p := types.DefaultParams()
	require.Equal(t, types.DefaultProposalFee, p.ProposalFee)
	require.NoError(t, p.Validate())
}

func TestNewParams(t *testing.T) {
	p := types.NewParams("100uspark")
	require.Equal(t, "100uspark", p.ProposalFee)
	require.NoError(t, p.Validate())
}

func TestParams_Validate_EmptyAllowed(t *testing.T) {
	// Empty string means "fee disabled"
	require.NoError(t, types.NewParams("").Validate())
}

func TestParams_Validate_InvalidCoin(t *testing.T) {
	require.Error(t, types.NewParams("not-a-coin-format").Validate())
}

func TestParams_Validate_MultipleCoins(t *testing.T) {
	require.NoError(t, types.NewParams("100uspark,50udream").Validate())
}

func TestParams_ParamSetPairs(t *testing.T) {
	p := types.DefaultParams()
	pairs := p.ParamSetPairs()
	// M10 removed the 3 recurring-spend params; only proposal_fee remains
	// on the commons-side ParamSet.
	require.Len(t, pairs, 1)
	require.Equal(t, types.KeyProposalFee, pairs[0].Key)
}

// The 3 recurring-spend params (MinRecurringPeriodSeconds,
// MaxRecurringDurationSeconds, MaxActiveRecurringSpendsPerGroup) and
// their validators live on x/session; the legacy tests for those
// validators are gone along with the params themselves.

func TestForbiddenMessages_RecursionGuards(t *testing.T) {
	require.True(t, types.ForbiddenMessages["/cosmos.authz.v1beta1.MsgExec"])
	require.True(t, types.ForbiddenMessages["/cosmos.authz.v1beta1.MsgGrant"])
}

func TestForbiddenMessages_RootKeyAttacks(t *testing.T) {
	require.True(t, types.ForbiddenMessages["/cosmos.group.v1.MsgCreateGroup"])
	require.True(t, types.ForbiddenMessages["/cosmos.group.v1.MsgUpdateGroupAdmin"])
}

func TestForbiddenMessages_ConsensusAttacks(t *testing.T) {
	require.True(t, types.ForbiddenMessages["/cosmos.slashing.v1beta1.MsgUnjail"])
	require.True(t, types.ForbiddenMessages["/cosmos.distribution.v1beta1.MsgSetWithdrawAddress"])
}

func TestForbiddenMessages_AllowsOrdinary(t *testing.T) {
	require.False(t, types.ForbiddenMessages["/cosmos.bank.v1beta1.MsgSend"])
}
