package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/types"
)

func TestChallengeReview(t *testing.T) {
	// Helper to create a collection, register curator, advance time, and rate it
	createReview := func(f *testFixture) (uint64, uint64) {
		collID := f.createCollection(t, f.owner)
		f.registerCurator(t, f.member, 500)
		f.advanceBlockHeight(14401)
		resp, err := f.msgServer.RateCollection(f.ctx, &types.MsgRateCollection{
			Creator:      f.member,
			CollectionId: collID,
			Verdict:      types.CurationVerdict_CURATION_VERDICT_UP,
		})
		require.NoError(t, err)
		return collID, resp.ReviewId
	}

	tests := []struct {
		name           string
		setup          func(f *testFixture) (collID uint64, reviewID uint64)
		msg            func(f *testFixture, collID, reviewID uint64) *types.MsgChallengeReview
		expErr         bool
		expErrContains string
		check          func(t *testing.T, f *testFixture, collID, reviewID uint64)
	}{
		{
			name: "success: challenge within window",
			setup: func(f *testFixture) (uint64, uint64) {
				return createReview(f)
			},
			msg: func(f *testFixture, collID, reviewID uint64) *types.MsgChallengeReview {
				return &types.MsgChallengeReview{
					Creator:  f.owner,
					ReviewId: reviewID,
					Reason:   "misleading review",
				}
			},
			expErr: false,
			check: func(t *testing.T, f *testFixture, collID, reviewID uint64) {
				review, err := f.keeper.CurationReview.Get(f.ctx, reviewID)
				require.NoError(t, err)
				require.True(t, review.Challenged)
				require.Equal(t, f.owner, review.Challenger)

				activity, err := f.keeper.CuratorActivity.Get(f.ctx, f.member)
				require.NoError(t, err)
				require.Equal(t, uint64(1), activity.ChallengedReviews)

				// Committed slash budget was reserved against the bonded role
				// (CuratorSlashFraction × MinCuratorBond).
				require.True(t, review.CommittedSlash.IsPositive())
			},
		},
		{
			name: "error: review not found",
			setup: func(f *testFixture) (uint64, uint64) {
				return 0, 0
			},
			msg: func(f *testFixture, collID, reviewID uint64) *types.MsgChallengeReview {
				return &types.MsgChallengeReview{
					Creator:  f.owner,
					ReviewId: 99999,
					Reason:   "bad",
				}
			},
			expErr:         true,
			expErrContains: "review not found",
		},
		{
			name: "error: challenge window passed",
			setup: func(f *testFixture) (uint64, uint64) {
				collID, reviewID := createReview(f)
				// Advance past the challenge window (default 100800 blocks)
				f.advanceBlockHeight(100801)
				return collID, reviewID
			},
			msg: func(f *testFixture, collID, reviewID uint64) *types.MsgChallengeReview {
				return &types.MsgChallengeReview{
					Creator:  f.owner,
					ReviewId: reviewID,
					Reason:   "too late",
				}
			},
			expErr:         true,
			expErrContains: "past challenge window",
		},
		{
			name: "error: self-challenge",
			setup: func(f *testFixture) (uint64, uint64) {
				return createReview(f)
			},
			msg: func(f *testFixture, collID, reviewID uint64) *types.MsgChallengeReview {
				return &types.MsgChallengeReview{
					Creator:  f.member, // member is the curator who wrote the review
					ReviewId: reviewID,
					Reason:   "self challenge",
				}
			},
			expErr:         true,
			expErrContains: "cannot challenge own review",
		},
		{
			name: "error: already challenged",
			setup: func(f *testFixture) (uint64, uint64) {
				collID, reviewID := createReview(f)
				// First challenge
				_, err := f.msgServer.ChallengeReview(f.ctx, &types.MsgChallengeReview{
					Creator:  f.owner,
					ReviewId: reviewID,
					Reason:   "first challenge",
				})
				require.NoError(t, err)
				return collID, reviewID
			},
			msg: func(f *testFixture, collID, reviewID uint64) *types.MsgChallengeReview {
				return &types.MsgChallengeReview{
					Creator:  f.sentinel,
					ReviewId: reviewID,
					Reason:   "second challenge",
				}
			},
			expErr:         true,
			expErrContains: "already challenged",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := initTestFixture(t)
			collID, reviewID := tc.setup(f)
			msg := tc.msg(f, collID, reviewID)
			resp, err := f.msgServer.ChallengeReview(f.ctx, msg)
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
				tc.check(t, f, collID, reviewID)
			}
		})
	}
}
