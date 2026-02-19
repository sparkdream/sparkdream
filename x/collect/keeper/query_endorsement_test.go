package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQueryEndorsement(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(collID uint64) *types.QueryEndorsementRequest
		expErr bool
		check  func(t *testing.T, resp *types.QueryEndorsementResponse, f *testFixture)
	}{
		{
			name: "found after seeding endorsement directly",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Seed endorsement directly via keeper store
				endorsement := types.Endorsement{
					CollectionId:   collID,
					Endorser:       f.member,
					DreamStake:     math.NewInt(100),
					EndorsedAt:     1,
					StakeReleaseAt: 100000,
					StakeReleased:  false,
				}
				err := f.keeper.Endorsement.Set(f.ctx, collID, endorsement)
				require.NoError(t, err)
				return collID
			},
			req: func(collID uint64) *types.QueryEndorsementRequest {
				return &types.QueryEndorsementRequest{CollectionId: collID}
			},
			expErr: false,
			check: func(t *testing.T, resp *types.QueryEndorsementResponse, f *testFixture) {
				require.Equal(t, f.member, resp.Endorsement.Endorser)
				require.True(t, resp.Endorsement.DreamStake.GT(math.ZeroInt()))
			},
		},
		{
			name:  "not found",
			setup: nil,
			req: func(_ uint64) *types.QueryEndorsementRequest {
				return &types.QueryEndorsementRequest{CollectionId: 999}
			},
			expErr: true,
		},
		{
			name:  "nil request",
			setup: nil,
			req: func(_ uint64) *types.QueryEndorsementRequest {
				return nil
			},
			expErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			var collID uint64
			if tc.setup != nil {
				collID = tc.setup(f)
			}
			resp, err := f.queryServer.Endorsement(f.ctx, tc.req(collID))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, f)
			}
		})
	}
}
