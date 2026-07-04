package keeper_test

import (
	"context"
	"testing"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

func TestHidePost(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create a sentinel with sufficient bond
	f.createTestSentinel(t, testSentinel, "2000000000")

	tests := []struct {
		name        string
		msg         *types.MsgHidePost
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful hide by sentinel",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "This is spam",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgHidePost{
				Creator:    "invalid-address",
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "moderation paused",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			setup: func() {
				params := types.DefaultParams()
				params.ModerationPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "moderation is paused",
		},
		{
			name: "post not found",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     9999,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "invalid reason code",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_UNSPECIFIED),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "invalid reason code",
		},
		{
			name: "not a sentinel",
			msg: &types.MsgHidePost{
				Creator:    testCreator2,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			expectError: true,
			errContains: "not a registered sentinel",
		},
		{
			name: "post already hidden",
			msg: &types.MsgHidePost{
				Creator:    testSentinel,
				PostId:     post.PostId,
				ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
				ReasonText: "Test",
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Status = types.PostStatus_POST_STATUS_HIDDEN
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			},
			expectError: true,
			errContains: "already hidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params and post status
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
			p.Status = types.PostStatus_POST_STATUS_ACTIVE
			_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

			// Reset sentinel activity
			f.createTestSentinel(t, testSentinel, "2000000000")

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.HidePost(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify post was hidden
				hiddenPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
				require.NoError(t, err)
				require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, hiddenPost.Status)
				require.Equal(t, tt.msg.Creator, hiddenPost.HiddenBy)

				// Verify hide record was created
				hideRecord, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Creator, hideRecord.Sentinel)
			}
		})
	}
}

func TestHidePostByGovAuthority(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Get authority address
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Hide by gov authority
	resp, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    authority,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION),
		ReasonText: "Policy violation",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify post was hidden
	hiddenPost, err := f.keeper.Post.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, hiddenPost.Status)

	// Gov hides now write a minimal HideRecord (Sentinel == "" marker) so
	// council-driven reversals can restore the slashed author bond. The
	// record must be present and explicitly marked as a gov hide; the
	// sentinel-specific bond-snapshot fields must remain empty.
	hr, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
	require.NoError(t, err, "gov hides must write a minimal HideRecord")
	require.Empty(t, hr.Sentinel, "gov-hide marker: Sentinel must be empty")
	require.Empty(t, hr.SentinelBondSnapshot, "gov hides do not reserve sentinel bond")
	require.Empty(t, hr.CommittedAmount, "gov hides do not commit slash amount")
	require.Equal(t, commontypes.ModerationReason_MODERATION_REASON_POLICY_VIOLATION, hr.ReasonCode)
}

func TestHidePostSentinelBondCommitment(t *testing.T) {
	f := initFixture(t)

	// Create a category and post
	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create a sentinel with specific bond
	initialBond := "2000000000"
	f.createTestSentinel(t, testSentinel, initialBond)

	// Hide the post
	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "Spam",
	})
	require.NoError(t, err)

	// Verify the shared RoleActivity counters were updated (rep-owned).
	ra := f.repKeeper.roleActivities[testSentinel]
	require.Equal(t, uint64(1), ra.TotalActions[reptypes.ActionKindForumHide])
	require.Equal(t, uint64(1), ra.EpochActions[reptypes.ActionKindForumHide])

	// Committed bond (now on the rep sentinel record) should be increased.
	repSentinel, ok := f.repKeeper.sentinels[testSentinel]
	require.True(t, ok)
	require.NotEqual(t, "0", repSentinel.TotalCommittedBond)
}

// TestHidePost_UnbondingSentinelRejected exercises the UNBONDING quantity gate:
// a sentinel unbonding its ENTIRE bond leaves zero staying bond — below the
// min_sentinel_bond floor — so it cannot back fresh hides. The portion being
// withdrawn is treated as already gone.
func TestHidePost_UnbondingSentinelRejected(t *testing.T) {
	f := initFixture(t)

	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	f.createTestSentinel(t, testSentinel, "2000000000")
	// Flip the sentinel into UNBONDING state directly on the mock rep keeper,
	// withdrawing the full bond so the staying bond is zero.
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "2000000000"
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	msg := &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "Test",
	}
	_, err := f.msgServer.HidePost(f.ctx, msg)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSentinelUnbonding)
}

// TestHidePost_PartialUnbondingSentinelAllowed is the motivating case: a
// sentinel bonded 700 DREAM queues a 100 DREAM unbond, leaving 600 staying —
// above the 500 floor — and must still be able to hide via the sentinel path.
func TestHidePost_PartialUnbondingSentinelAllowed(t *testing.T) {
	f := initFixture(t)

	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	f.createTestSentinel(t, testSentinel, "700000000") // 700 DREAM
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "100000000" // withdraw 100, 600 stays
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	msg := &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "Test",
	}
	_, err := f.msgServer.HidePost(f.ctx, msg)
	require.NoError(t, err)

	// The hide took the sentinel path: a HideRecord crediting the sentinel
	// exists (Sentinel != "" gov-hide marker).
	rec, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
	require.NoError(t, err)
	require.Equal(t, testSentinel, rec.Sentinel)
}

// TestHidePost_PartialUnbondingBelowFloorRejected covers the boundary: a partial
// unbond that drops the staying bond under the floor (700 bonded, 250 unbond →
// 450 < 500) is rejected with ErrSentinelUnbonding.
func TestHidePost_PartialUnbondingBelowFloorRejected(t *testing.T) {
	f := initFixture(t)

	cat := f.createTestCategory(t, "General")
	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	f.createTestSentinel(t, testSentinel, "700000000")
	br := f.repKeeper.sentinels[testSentinel]
	br.PendingUnbondAmount = "250000000" // 450 stays, below 500 floor
	br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_UNBONDING
	f.repKeeper.sentinels[testSentinel] = br

	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     post.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "Test",
	})
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrSentinelUnbonding)
}

// TestHidePost_ParamDrivenEpochCap proves the per-epoch hide cap is read from
// params (not the hardcoded DefaultMaxHidesPerEpoch): lowering it to 1 makes the
// second hide in the epoch fail.
func TestHidePost_ParamDrivenEpochCap(t *testing.T) {
	f := initFixture(t)
	cat := f.createTestCategory(t, "General")
	f.createTestSentinel(t, testSentinel, "2000000000")

	params := types.DefaultParams()
	params.MaxHidesPerEpoch = 1
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	p1 := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     p1.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "x",
	})
	require.NoError(t, err)

	p2 := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	_, err = f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
		Creator:    testSentinel,
		PostId:     p2.PostId,
		ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
		ReasonText: "x",
	})
	require.ErrorIs(t, err, types.ErrHideLimitExceeded)
}

// TestHidePost_AuthorityDisambiguation covers the case where an account is BOTH
// a bonded forum sentinel AND a Commons Operations Committee member. Without an
// explicit authority the hide must default to the accountable sentinel path
// (bonded, author-appealable) rather than silently upgrading to the council
// (gov) path. See docs/x-forum-spec.md (Shared ModerationAuthority).
func TestHidePost_AuthorityDisambiguation(t *testing.T) {
	makeSentinelAndCouncil := func(t *testing.T) (*fixture, uint64) {
		t.Helper()
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		f.createTestSentinel(t, testSentinel, "2000000000")
		// Make the bonded sentinel ALSO a council member. The default
		// IsCouncilAuthorizedFn only matches the gov authority address; extend
		// it so testSentinel resolves as council too.
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
			return addr == authority || addr == testSentinel
		}
		return f, post.PostId
	}

	// AUTO by a sentinel-and-council account must take the sentinel path:
	// HideRecord.Sentinel == creator (NOT the empty gov-hide marker), bond
	// committed, forum counters bumped.
	t.Run("auto prefers sentinel path", func(t *testing.T) {
		f, postID := makeSentinelAndCouncil(t)
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     postID,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "spam",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.NoError(t, err)
		hr, err := f.keeper.HideRecord.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, testSentinel, hr.Sentinel, "AUTO must default to the sentinel path")
		require.NotEmpty(t, hr.SentinelBondSnapshot, "sentinel hide must snapshot bond")
		require.NotEmpty(t, hr.CommittedAmount, "sentinel hide must commit slash amount")
		require.Equal(t, uint64(1),
			f.repKeeper.roleActivities[testSentinel].TotalActions[reptypes.ActionKindForumHide])
	})

	// Explicit COUNCIL by the same account is the deliberate "act as committee"
	// choice: gov-hide marker (Sentinel == ""), no bond committed.
	t.Run("explicit council is opt-in gov hide", func(t *testing.T) {
		f, postID := makeSentinelAndCouncil(t)
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     postID,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "spam",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.NoError(t, err)
		hr, err := f.keeper.HideRecord.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Empty(t, hr.Sentinel, "explicit COUNCIL must write the gov-hide marker")
		require.Empty(t, hr.CommittedAmount, "gov hide must not commit slash amount")
		// Shared sentinel counters must NOT be bumped by a council hide.
		require.Equal(t, uint64(0),
			f.repKeeper.roleActivities[testSentinel].TotalActions[reptypes.ActionKindForumHide],
			"council hide must not bump sentinel counters")
	})

	// Explicit SENTINEL by the same account takes the sentinel path.
	t.Run("explicit sentinel forces sentinel path", func(t *testing.T) {
		f, postID := makeSentinelAndCouncil(t)
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     postID,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "spam",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
		})
		require.NoError(t, err)
		hr, err := f.keeper.HideRecord.Get(f.ctx, postID)
		require.NoError(t, err)
		require.Equal(t, testSentinel, hr.Sentinel)
	})
}

// TestHidePost_ExplicitAuthorityErrors covers the hard-error edges: an explicit
// authority the caller cannot satisfy must fail with no silent fallback.
func TestHidePost_ExplicitAuthorityErrors(t *testing.T) {
	// Explicit SENTINEL by a non-sentinel council member → ErrNotSentinel
	// (no silent fallback to council).
	t.Run("explicit sentinel by council-only fails", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    authority, // council, but not a sentinel
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "x",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_SENTINEL,
		})
		require.ErrorIs(t, err, types.ErrNotSentinel)
	})

	// Explicit COUNCIL by a sentinel-only account → ErrNotAuthorized.
	t.Run("explicit council by sentinel-only fails", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		f.createTestSentinel(t, testSentinel, "2000000000")
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel, // sentinel, but not council
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "x",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_COUNCIL,
		})
		require.ErrorIs(t, err, types.ErrNotAuthorized)
	})

	// AUTO by a DEMOTED sentinel that is also council falls through to the
	// council path (demoted bond is not eligible).
	t.Run("auto falls through to council when sentinel demoted", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		f.createTestSentinel(t, testSentinel, "2000000000")
		br := f.repKeeper.sentinels[testSentinel]
		br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
		f.repKeeper.sentinels[testSentinel] = br
		authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, addr string, _ string, _ string) bool {
			return addr == authority || addr == testSentinel
		}
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "x",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.NoError(t, err)
		hr, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Empty(t, hr.Sentinel, "demoted sentinel that is council hides as council under AUTO")
	})

	// AUTO by a demoted, non-council account surfaces the specific sentinel
	// reason rather than a generic unauthorized error.
	t.Run("auto by demoted non-council surfaces demotion", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "General")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		f.createTestSentinel(t, testSentinel, "2000000000")
		br := f.repKeeper.sentinels[testSentinel]
		br.BondStatus = reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_DEMOTED
		f.repKeeper.sentinels[testSentinel] = br
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "x",
			Authority:  types.ModerationAuthority_MODERATION_AUTHORITY_AUTO,
		})
		require.ErrorIs(t, err, types.ErrSentinelDemoted)
	})
}
