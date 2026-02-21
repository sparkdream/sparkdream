package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/types"
)

func TestQueryVoterRegistrationQuery(t *testing.T) {
	t.Run("happy: get registration by address", func(t *testing.T) {
		f := initTestFixture(t)

		reg := types.VoterRegistration{
			Address:     f.member,
			ZkPublicKey: genZkPubKey(1),
			Active:      true,
		}
		require.NoError(t, f.keeper.VoterRegistration.Set(f.ctx, f.member, reg))

		resp, err := f.queryServer.VoterRegistrationQuery(f.ctx, &types.QueryVoterRegistrationQueryRequest{
			Address: f.member,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, f.member, resp.Registration.Address)
		require.Equal(t, genZkPubKey(1), resp.Registration.ZkPublicKey)
		require.True(t, resp.Registration.Active)
	})

	t.Run("not found: non-existent address", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.queryServer.VoterRegistrationQuery(f.ctx, &types.QueryVoterRegistrationQueryRequest{
			Address: "nonexistent_address",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("nil request", func(t *testing.T) {
		f := initTestFixture(t)

		_, err := f.queryServer.VoterRegistrationQuery(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})
}
