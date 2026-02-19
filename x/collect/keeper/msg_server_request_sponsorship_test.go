package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestRequestSponsorship(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgRequestSponsorship
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: non-member requests sponsorship",
			setup: func(f *testFixture) uint64 {
				return f.createPendingCollection(t)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRequestSponsorship {
				return &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				req, err := f.keeper.SponsorshipRequest.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, f.nonMember, req.Requester)
				require.True(t, req.CollectionDeposit.IsPositive())
			},
		},
		{
			name: "error: not owner",
			setup: func(f *testFixture) uint64 {
				return f.createPendingCollection(t) // owned by nonMember
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRequestSponsorship {
				return &types.MsgRequestSponsorship{
					Creator:      f.owner, // owner is not the collection owner
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "only collection owner",
		},
		{
			name: "error: already has pending request",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRequestSponsorship {
				return &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "sponsorship request already exists",
		},
		{
			name: "error: member cannot request",
			setup: func(f *testFixture) uint64 {
				// Create a TTL collection as a member (owner)
				sdkCtx := sdk.UnwrapSDKContext(f.ctx)
				expiresAt := sdkCtx.BlockHeight() + 100000
				collID := f.createCollection(t, f.owner, withTTL(expiresAt))
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRequestSponsorship {
				return &types.MsgRequestSponsorship{
					Creator:      f.owner,
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "members can convert to permanent directly",
		},
		{
			name: "error: collection already permanent",
			setup: func(f *testFixture) uint64 {
				// nonMember creates a TTL collection. We need to make it permanent by removing expires.
				collID := f.createPendingCollection(t)
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.ExpiresAt = 0
				f.keeper.Collection.Set(f.ctx, collID, coll)
				// Temporarily make nonMember not a member for the check
				f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
					return addr.Equals(f.ownerAddr) || addr.Equals(f.memberAddr) || addr.Equals(f.sentinelAddr)
				}
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRequestSponsorship {
				return &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "already permanent",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			msg := tc.msg(f, collID)
			resp, err := f.msgServer.RequestSponsorship(f.ctx, msg)
			if tc.expErr {
				require.Error(t, err)
				if tc.expErrContains != "" {
					require.Contains(t, err.Error(), tc.expErrContains)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp)
			if tc.check != nil {
				tc.check(t, f, collID)
			}
		})
	}
}
