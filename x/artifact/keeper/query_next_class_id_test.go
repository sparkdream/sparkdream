package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func TestNextClassIdAndExpectedClassId(t *testing.T) {
	f := initFixture(t)

	next, err := f.query.NextClassId(f.ctx, &types.QueryNextClassIdRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(1), next.NextClassId)

	// The id the query promised is the id the class gets.
	id := f.createClass(f.alice, func(m *types.MsgCreateClass) { m.ExpectedClassId = next.NextClassId })
	require.Equal(t, next.NextClassId, id)

	// Someone else created a class in between: a stale expectation fails
	// instead of creating a class under another id, and consumes nothing.
	f.createClass(f.bob)
	_, err = f.msg.CreateClass(f.ctx, &types.MsgCreateClass{
		Creator: f.alice.String(), Name: "Late", AcceptedContentLicense: types.ContentLicense, ExpectedClassId: 2,
	})
	require.ErrorIs(t, err, types.ErrClassIdMismatch)
	next, err = f.query.NextClassId(f.ctx, &types.QueryNextClassIdRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(3), next.NextClassId)
}
