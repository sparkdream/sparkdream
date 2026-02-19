package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestQuerySponsorshipRequest(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(f *testFixture) uint64
		req    func(collID uint64) *types.QuerySponsorshipRequestRequest
		expErr bool
		check  func(t *testing.T, resp *types.QuerySponsorshipRequestResponse, f *testFixture, collID uint64)
	}{
		{
			name: "found after requesting sponsorship",
			setup: func(f *testFixture) uint64 {
				// Create a TTL collection owned by nonMember
				// nonMember is not a member, so this creates a PENDING collection
				// But RequestSponsorship requires a TTL collection (ExpiresAt > 0) that is NOT pending...
				// Actually, RequestSponsorship checks: coll.Owner == msg.Creator, coll.ExpiresAt > 0, !coll.DepositBurned
				// and coll.Owner must NOT be a member.
				// Let's create a TTL collection owned by owner (who is a member) and then
				// we need a non-member owner. Use nonMember but they create PENDING collections.
				// Actually, let's seed the data directly via the keeper store.
				sdkCtx := sdk.UnwrapSDKContext(f.ctx)
				blockHeight := sdkCtx.BlockHeight()
				expiresAt := blockHeight + 50000

				collID := f.createPendingCollection(t)

				// For RequestSponsorship, the nonMember owner requests it.
				// But RequestSponsorship checks status isn't checked, it checks ExpiresAt > 0 and !DepositBurned.
				// Let's try requesting sponsorship on the pending collection.
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)
				_ = expiresAt
				return collID
			},
			req: func(collID uint64) *types.QuerySponsorshipRequestRequest {
				return &types.QuerySponsorshipRequestRequest{CollectionId: collID}
			},
			expErr: false,
			check: func(t *testing.T, resp *types.QuerySponsorshipRequestResponse, f *testFixture, collID uint64) {
				require.Equal(t, collID, resp.SponsorshipRequest.CollectionId)
				require.Equal(t, f.nonMember, resp.SponsorshipRequest.Requester)
				require.True(t, resp.SponsorshipRequest.CollectionDeposit.GT(math.ZeroInt()))
			},
		},
		{
			name:  "not found",
			setup: nil,
			req: func(_ uint64) *types.QuerySponsorshipRequestRequest {
				return &types.QuerySponsorshipRequestRequest{CollectionId: 999}
			},
			expErr: true,
		},
		{
			name:  "nil request",
			setup: nil,
			req: func(_ uint64) *types.QuerySponsorshipRequestRequest {
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
			resp, err := f.queryServer.SponsorshipRequest(f.ctx, tc.req(collID))
			if tc.expErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, resp, f, collID)
			}
		})
	}
}
