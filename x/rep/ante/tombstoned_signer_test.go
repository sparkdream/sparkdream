package ante_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/ante"
	"sparkdream/x/rep/types"
)

// TestTombstoneAllowed pins the tombstone allowlist: staking and reward
// withdrawals always, MsgSend only after a KEY migration, nothing else.
func TestTombstoneAllowed(t *testing.T) {
	key, rec, council := types.MigrationPath_MIGRATION_PATH_KEY, types.MigrationPath_MIGRATION_PATH_RECOVERY, types.MigrationPath_MIGRATION_PATH_COUNCIL
	cases := []struct {
		url  string
		path types.MigrationPath
		want bool
	}{
		{"/cosmos.staking.v1beta1.MsgUndelegate", rec, true},
		{"/cosmos.staking.v1beta1.MsgBeginRedelegate", council, true},
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
