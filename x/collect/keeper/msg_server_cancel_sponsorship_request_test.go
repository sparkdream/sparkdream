package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestCancelSponsorshipRequest(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgCancelSponsorshipRequest
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: owner cancels",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgCancelSponsorshipRequest {
				return &types.MsgCancelSponsorshipRequest{
					Creator:      f.nonMember,
					CollectionId: collID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				_, err := f.keeper.SponsorshipRequest.Get(f.ctx, collID)
				require.Error(t, err) // should be removed
			},
		},
		{
			name: "error: not owner",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgCancelSponsorshipRequest {
				return &types.MsgCancelSponsorshipRequest{
					Creator:      f.owner, // not the collection owner (nonMember owns it)
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "only collection owner",
		},
		{
			name: "error: request not found",
			setup: func(f *testFixture) uint64 {
				return f.createPendingCollection(t) // no sponsorship request created
			},
			msg: func(f *testFixture, collID uint64) *types.MsgCancelSponsorshipRequest {
				return &types.MsgCancelSponsorshipRequest{
					Creator:      f.nonMember,
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "no pending sponsorship request",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			msg := tc.msg(f, collID)
			resp, err := f.msgServer.CancelSponsorshipRequest(f.ctx, msg)
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
