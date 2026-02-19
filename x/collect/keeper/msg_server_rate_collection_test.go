package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestRateCollection(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(f *testFixture) uint64
		msg            func(f *testFixture, collID uint64) *types.MsgRateCollection
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID uint64)
	}{
		{
			name: "success: curator rates UP",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				// Advance past min_curator_age_blocks (14400)
				f.advanceBlockHeight(14401)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
					Tags:         []string{"quality"},
					Comment:      "great collection",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID uint64) {
				summary, err := f.keeper.CurationSummary.Get(f.ctx, collID)
				require.NoError(t, err)
				require.Equal(t, uint32(1), summary.UpCount)
				require.Equal(t, uint32(0), summary.DownCount)

				curator, err := f.keeper.Curator.Get(f.ctx, f.member)
				require.NoError(t, err)
				require.Equal(t, uint64(1), curator.TotalReviews)
			},
		},
		{
			name: "error: not a curator",
			setup: func(f *testFixture) uint64 {
				return f.createCollection(t, f.owner)
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				}
			},
			expErr:         true,
			expErrContains: "not a registered active curator",
		},
		{
			name: "error: curator is collection owner",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.owner, 500)
				f.advanceBlockHeight(14401)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.owner,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				}
			},
			expErr:         true,
			expErrContains: "curator is collection owner",
		},
		{
			name: "error: already reviewed this collection",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				// First review
				_, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				})
				require.NoError(t, err)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_DOWN,
				}
			},
			expErr:         true,
			expErrContains: "one active review per curator",
		},
		{
			name: "error: max reviews reached",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				// Set params to allow only 0 reviews (already at max)
				params, _ := f.keeper.Params.Get(f.ctx)
				params.MaxReviewsPerCollection = 0
				f.keeper.Params.Set(f.ctx, params)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				}
			},
			expErr:         true,
			expErrContains: "max reviews",
		},
		{
			name: "error: collection not public/active",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				// Set visibility to private
				coll, _ := f.keeper.Collection.Get(f.ctx, collID)
				coll.Visibility = types.Visibility_VISIBILITY_PRIVATE
				f.keeper.Collection.Set(f.ctx, collID, coll)
				f.registerCurator(t, f.member, 500)
				f.advanceBlockHeight(14401)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				}
			},
			expErr:         true,
			expErrContains: "not public",
		},
		{
			name: "error: curator too new",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				// Do NOT advance block height past min_curator_age_blocks
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				}
			},
			expErr:         true,
			expErrContains: "curator registered less than min age blocks ago",
		},
		{
			name: "error: curator bond insufficient",
			setup: func(f *testFixture) uint64 {
				collID := f.createCollection(t, f.owner)
				f.registerCurator(t, f.member, 500)
				// Lower bond below min after registration
				curator, _ := f.keeper.Curator.Get(f.ctx, f.member)
				curator.BondAmount = math.NewInt(100)
				f.keeper.Curator.Set(f.ctx, f.member, curator)
				f.advanceBlockHeight(14401)
				return collID
			},
			msg: func(f *testFixture, collID uint64) *types.MsgRateCollection {
				return &types.MsgRateCollection{
					Creator:      f.member,
					CollectionId: collID,
					Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
				}
			},
			expErr:         true,
			expErrContains: "curator bond dropped below min",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID := tc.setup(f)
			msg := tc.msg(f, collID)
			resp, err := f.msgServer.RateCollection(f.ctx, msg)
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
