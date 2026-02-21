package keeper_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
	reptypes "sparkdream/x/rep/types"
)

func TestSponsorCollection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgSponsorCollection
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: member sponsors",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t) // owned by nonMember
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSponsorCollection {
				return &types.MsgSponsorCollection{
					Creator:      f.member,
					CollectionId: collID,
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				coll, err := f.keeper.Collection.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, int64(0), coll.ExpiresAt) // now permanent
				require.True(t, coll.DepositBurned)
				require.Equal(t, f.member, coll.SponsoredBy)

				// Sponsorship request should be removed
				_, err = f.keeper.SponsorshipRequest.Get(f.ctx, collID)
				require.Error(t, err)
			},
		},
		{
			name: "error: trust level too low",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t)
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)

				// Set trust level too low for sponsoring
				f.repKeeper.getTrustLevelFn = func(_ context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
					if addr.Equals(f.memberAddr) {
						return reptypes.TrustLevel_TRUST_LEVEL_NEW, nil
					}
					return reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED, nil
				}
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSponsorCollection {
				return &types.MsgSponsorCollection{
					Creator:      f.member,
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "sponsor below min sponsor trust level",
		},
		{
			name: "error: owner cannot self-sponsor",
			setup: func(f *testFixture) uint64 {
				collID := f.createPendingCollection(t) // owned by nonMember
				_, err := f.msgServer.RequestSponsorship(f.ctx, &types.MsgRequestSponsorship{
					Creator:      f.nonMember,
					CollectionId: collID,
				})
				require.NoError(t, err)

				// Make nonMember a member for the sponsor call
				f.repKeeper.isMemberFn = func(_ context.Context, addr sdk.AccAddress) bool {
					return true
				}
				f.repKeeper.getTrustLevelFn = func(_ context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
					return reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED, nil
				}
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSponsorCollection {
				return &types.MsgSponsorCollection{
					Creator:      f.nonMember, // nonMember is the owner
					CollectionId: collID,
				}
			},
			expErr:         true,
			expErrContains: "sponsor cannot be the collection owner",
		},
		{
			name: "error: no pending sponsorship request",
			setup: func(f *testFixture) uint64 {
				return f.createPendingCollection(t) // no sponsorship request
			},
			msg: func(f *testFixture, collID uint64) *types.MsgSponsorCollection {
				return &types.MsgSponsorCollection{
					Creator:      f.member,
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
			resp, err := f.msgServer.SponsorCollection(f.ctx, msg)
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
