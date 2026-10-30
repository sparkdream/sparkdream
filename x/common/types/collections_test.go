package types_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	"github.com/stretchr/testify/require"

	"sparkdream/x/common/types"
)

type byValue struct {
	Value *indexes.Multi[uint64, string, uint64]
}

func (b byValue) IndexesList() []collections.Index[string, uint64] {
	return []collections.Index[string, uint64]{b.Value}
}

func TestRemoveIndexed(t *testing.T) {
	key := storetypes.NewKVStoreKey("phoenix")
	ctx := testutil.DefaultContext(key, storetypes.NewTransientStoreKey("t_phoenix"))
	sb := collections.NewSchemaBuilder(runtime.NewKVStoreService(key))
	m := collections.NewIndexedMap(sb, collections.NewPrefix(0), "aurora", collections.StringKey, collections.Uint64Value,
		byValue{Value: indexes.NewMulti(sb, collections.NewPrefix(1), "by_value", collections.Uint64Key, collections.StringKey,
			func(_ string, v uint64) (uint64, error) { return v, nil })})
	_, err := sb.Build()
	require.NoError(t, err)

	// IndexedMap.Remove itself fails on a missing key.
	require.ErrorIs(t, m.Remove(ctx, "zenith"), collections.ErrNotFound)
	require.NoError(t, types.RemoveIndexed(ctx, m, "zenith"))

	require.NoError(t, m.Set(ctx, "zenith", 7))
	require.NoError(t, types.RemoveIndexed(ctx, m, "zenith"))
	has, err := m.Has(ctx, "zenith")
	require.NoError(t, err)
	require.False(t, has)
	iter, err := m.Indexes.Value.MatchExact(ctx, 7)
	require.NoError(t, err)
	defer iter.Close()
	require.False(t, iter.Valid(), "index entry removed with the record")
}
