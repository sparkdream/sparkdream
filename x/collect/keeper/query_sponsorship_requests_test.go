package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQuerySponsorshipRequests(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture)
		expLen int
	}{
		{
			name:   "empty",
			setup:  nil,
			expLen: 0,
		},
		{
			name: "returns sponsorship requests",
			setup: func(f *testFixture) {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)
			},
			expLen: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			resp, err := f.queryServer.SponsorshipRequests(f.ctx, &types.QuerySponsorshipRequestsRequest{})
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Len(t, resp.SponsorshipRequests, tc.expLen)
		})
	}
}

func TestQuerySponsorshipRequests_NilRequest(t *testing.T) {
	f := initTestFixture(t)
	_, err := f.queryServer.SponsorshipRequests(f.ctx, nil)
	require.Error(t, err)
}
