package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/session/types"
)

// The v1 -> v2 migration admits the x/rep work messages and the x/storage
// worker messages on a chain whose allowlist predates them, keeps what was
// there, and is harmless to run on a list that already holds them.
func TestMigrateV1ToV2AdmitsRepAndStorageMessages(t *testing.T) {
	f := initFixture(t)
	added := map[string]bool{}
	for _, list := range [][]string{types.RepWorkMsgTypes, types.StorageWorkerMsgTypes} {
		for _, m := range list {
			added[m] = true
		}
	}
	require.Len(t, types.StorageWorkerMsgTypes, 5)

	// A pre-upgrade chain: the defaults without the new entries, and an
	// active list the Operations Committee had narrowed.
	params := types.DefaultParams()
	var old []string
	for _, m := range params.MaxAllowedMsgTypes {
		if !added[m] {
			old = append(old, m)
		}
	}
	params.MaxAllowedMsgTypes = old
	params.AllowedMsgTypes = append([]string(nil), old[:3]...)
	require.NoError(t, params.Validate())
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	require.NoError(t, f.keeper.MigrateV1ToV2(f.ctx))
	got, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Len(t, got.MaxAllowedMsgTypes, len(old)+len(added))
	require.Len(t, got.AllowedMsgTypes, 3+len(added))
	require.Equal(t, old, got.MaxAllowedMsgTypes[:len(old)])
	for m := range added {
		require.Contains(t, got.MaxAllowedMsgTypes, m)
		require.Contains(t, got.AllowedMsgTypes, m)
	}

	require.NoError(t, f.keeper.MigrateV1ToV2(f.ctx))
	again, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	require.Equal(t, got, again)
}
