package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/ante"
	"sparkdream/x/rep/types"
)

// TestTombstoneAllowed pins the tombstone allowlist: undelegation and reward
// and commission withdrawals always; MsgSend, redelegation and unbonding
// cancellation only after a KEY migration (REP-R-06: a thief holding a
// recovered member's old key must not steer the victim's delegations);
// nothing else.
func TestTombstoneAllowed(t *testing.T) {
	key, rec, council := types.MigrationPath_MIGRATION_PATH_KEY, types.MigrationPath_MIGRATION_PATH_RECOVERY, types.MigrationPath_MIGRATION_PATH_COUNCIL
	cases := []struct {
		url  string
		path types.MigrationPath
		want bool
	}{
		{"/cosmos.staking.v1beta1.MsgUndelegate", rec, true},
		{"/cosmos.staking.v1beta1.MsgBeginRedelegate", key, true},
		{"/cosmos.staking.v1beta1.MsgBeginRedelegate", council, false},
		{"/cosmos.staking.v1beta1.MsgBeginRedelegate", rec, false},
		{"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation", key, true},
		{"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation", rec, false},
		{"/cosmos.staking.v1beta1.MsgCancelUnbondingDelegation", council, false},
		{"/cosmos.staking.v1beta1.MsgUndelegate", council, true},
		{"/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission", rec, true},
		{"/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward", rec, true},
		{"/cosmos.bank.v1beta1.MsgSend", key, true},
		{"/cosmos.bank.v1beta1.MsgSend", rec, false},
		{"/cosmos.bank.v1beta1.MsgSend", council, false},
		{"/sparkdream.session.v1.MsgExecSession", key, false},
		{"/sparkdream.rep.v1.MsgRequestMigration", key, false},
		{"/sparkdream.forum.v1.MsgCreatePost", key, false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, ante.TombstoneAllowed(tc.url, tc.path), "%s on %s", tc.url, tc.path)
	}
}
