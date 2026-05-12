package keeper_test

import (
	"strconv"
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// TestAuthorBondRestoration_DirectUnhide covers the MsgUnhidePost happy path:
// a sentinel hide that slashed an author bond is reversed (self-correct or
// council), and the bond is minted + re-locked so the author is whole.
func TestAuthorBondRestoration_DirectUnhide(t *testing.T) {
	bondAmount := math.NewInt(7_500_000) // 7.5 DREAM

	// Pre-seed the author bond so MsgHidePost has something to slash.
	seedBond := func(f *fixture, postID uint64, author string) {
		if f.repKeeper.authorBonds == nil {
			f.repKeeper.authorBonds = make(map[string]reptypes.Stake)
		}
		f.repKeeper.authorBonds[authorBondKey(reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, postID)] =
			reptypes.Stake{
				Staker:     author,
				TargetType: reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND,
				TargetId:   postID,
				Amount:     bondAmount,
			}
	}

	t.Run("sentinel self-correct restores bond with original amount", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorBondSelfCorrect")
		f.createTestSentinel(t, testSentinel, "2000000000")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		seedBond(f, post.PostId, testCreator)

		// Hide — slashes the bond + records AuthorBondAmount on HideRecord.
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "test",
		})
		require.NoError(t, err)

		// Verify the slash happened (bond gone from mock) and the amount was captured.
		_, getErr := f.repKeeper.GetAuthorBond(f.ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)
		require.ErrorIs(t, getErr, reptypes.ErrAuthorBondNotFound, "slash must have cleared the bond")

		hr, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, bondAmount.String(), hr.AuthorBondAmount,
			"HideRecord must capture the slashed amount so reversal can restore it")

		// Self-correct within window.
		_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testSentinel,
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		// RestoreAuthorBond was invoked, bond is back at the original amount.
		require.Len(t, f.repKeeper.restoreCalls, 1, "RestoreAuthorBond must fire exactly once")
		require.Equal(t, bondAmount, f.repKeeper.restoreCalls[0].Amount)
		require.Equal(t, testCreator, f.repKeeper.restoreCalls[0].Staker)

		bond, err := f.repKeeper.GetAuthorBond(f.ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)
		require.NoError(t, err)
		require.Equal(t, bondAmount, bond.Amount, "restored bond must match original amount")
	})

	t.Run("council unhide of sentinel hide also restores bond", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorBondCouncilUnhide")
		f.createTestSentinel(t, testSentinel, "2000000000")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		seedBond(f, post.PostId, testCreator)

		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "test",
		})
		require.NoError(t, err)

		_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(), // council, not the sentinel
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		require.Len(t, f.repKeeper.restoreCalls, 1)
		_, err = f.repKeeper.GetAuthorBond(f.ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)
		require.NoError(t, err, "council unhide of sentinel hide must restore bond")
	})

	t.Run("gov-hide council unhide DOES restore (minimal HideRecord captures AuthorBondAmount)", func(t *testing.T) {
		// Gov-authority hides now write a minimal HideRecord with Sentinel == ""
		// as a marker, so council unhides can restore the slashed author bond
		// the same way sentinel-hide reversals can. This closes the asymmetry
		// where a council mistake left the author permanently out the DREAM.
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorBondGovHide")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		seedBond(f, post.PostId, testCreator)

		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    govAuthAddr(),
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "gov hide",
		})
		require.NoError(t, err)

		// Sanity: HideRecord exists with Sentinel == "" (gov-hide marker)
		// and the snapshotted bond amount.
		hr, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.NoError(t, err, "gov hide must now create a minimal HideRecord")
		require.Empty(t, hr.Sentinel, "gov hides leave Sentinel empty as the gov-hide marker")
		require.Equal(t, bondAmount.String(), hr.AuthorBondAmount,
			"gov hide must snapshot AuthorBondAmount so council unhide can restore")

		_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: govAuthAddr(),
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		require.Len(t, f.repKeeper.restoreCalls, 1,
			"council unhide of gov hide MUST restore the bond (round-trip net-zero)")
		require.Equal(t, bondAmount, f.repKeeper.restoreCalls[0].Amount)

		bond, err := f.repKeeper.GetAuthorBond(f.ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)
		require.NoError(t, err)
		require.Equal(t, bondAmount, bond.Amount)
	})

	t.Run("no-bond post: unhide is a no-op on bond restoration", func(t *testing.T) {
		// Author bond is optional in MsgCreatePost. Posts without a bond
		// must survive the full hide → unhide cycle without spurious mints.
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorBondAbsent")
		f.createTestSentinel(t, testSentinel, "2000000000")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		// NOTE: no seedBond call — post has no author bond.

		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "test",
		})
		require.NoError(t, err)

		// HideRecord.AuthorBondAmount should be empty (no bond was attached).
		hr, err := f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Empty(t, hr.AuthorBondAmount,
			"posts without an author bond must leave HideRecord.AuthorBondAmount empty")

		_, err = f.msgServer.UnhidePost(f.ctx, &types.MsgUnhidePost{
			Creator: testSentinel,
			PostId:  post.PostId,
		})
		require.NoError(t, err)

		require.Empty(t, f.repKeeper.restoreCalls,
			"unhiding a bondless post must not invoke RestoreAuthorBond")
	})
}

// TestAuthorBondRestoration_AppealOverturn covers the cross-module path:
// MsgResolveGovActionAppeal with OVERTURNED verdict invokes
// ReverseSentinelAction, which must restore the slashed bond. Exercised via
// the forum keeper's ReverseSentinelAction directly (the rep-side call is
// already covered in msg_server_resolve_gov_action_appeal_test.go).
func TestAuthorBondRestoration_AppealOverturn(t *testing.T) {
	bondAmount := math.NewInt(12_345_678)

	t.Run("ReverseSentinelAction restores bond from HideRecord", func(t *testing.T) {
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorBondAppealOverturn")
		f.createTestSentinel(t, testSentinel, "2000000000")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)

		// Seed pre-hide bond.
		if f.repKeeper.authorBonds == nil {
			f.repKeeper.authorBonds = make(map[string]reptypes.Stake)
		}
		f.repKeeper.authorBonds[authorBondKey(reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)] =
			reptypes.Stake{Staker: testCreator, Amount: bondAmount}

		// Run real hide (slashes + records amount).
		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "wrong",
		})
		require.NoError(t, err)

		// Simulate the OVERTURNED branch firing ReverseSentinelAction.
		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED, // hide path
			strconv.FormatUint(post.PostId, 10),
		))

		// Bond restored.
		require.Len(t, f.repKeeper.restoreCalls, 1)
		require.Equal(t, bondAmount, f.repKeeper.restoreCalls[0].Amount)
		bond, err := f.repKeeper.GetAuthorBond(f.ctx, reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)
		require.NoError(t, err)
		require.Equal(t, bondAmount, bond.Amount)

		// Post is back to ACTIVE; HideRecord cleaned up.
		p, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, p.Status)
		_, err = f.keeper.HideRecord.Get(f.ctx, post.PostId)
		require.Error(t, err, "HideRecord must be cleaned up")
	})

	t.Run("dangling category skip avoids both status flip AND bond restore", func(t *testing.T) {
		// Belt-and-suspenders: even if a category is deleted before an
		// overturned-appeal reversal lands, the rep keeper must not mint
		// DREAM on the author's behalf for a now-orphaned post — the
		// dangling-reference guard at the top of the hide branch short-
		// circuits before any state mutation.
		f := initFixture(t)
		cat := f.createTestCategory(t, "AuthorBondCategoryGone")
		f.createTestSentinel(t, testSentinel, "2000000000")
		post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		if f.repKeeper.authorBonds == nil {
			f.repKeeper.authorBonds = make(map[string]reptypes.Stake)
		}
		f.repKeeper.authorBonds[authorBondKey(reptypes.StakeTargetType_STAKE_TARGET_FORUM_AUTHOR_BOND, post.PostId)] =
			reptypes.Stake{Staker: testCreator, Amount: bondAmount}

		_, err := f.msgServer.HidePost(f.ctx, &types.MsgHidePost{
			Creator:    testSentinel,
			PostId:     post.PostId,
			ReasonCode: uint64(commontypes.ModerationReason_MODERATION_REASON_SPAM),
			ReasonText: "wrong",
		})
		require.NoError(t, err)

		delete(f.commonsKeeper.categories, cat.CategoryId)

		require.NoError(t, f.keeper.ReverseSentinelAction(
			f.ctx,
			reptypes.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED,
			strconv.FormatUint(post.PostId, 10),
		), "soft skip")

		require.Empty(t, f.repKeeper.restoreCalls,
			"reverse must not mint a bond into a post whose category was deleted")
		// Post stays HIDDEN.
		p, err := f.keeper.Post.Get(f.ctx, post.PostId)
		require.NoError(t, err)
		require.Equal(t, types.PostStatus_POST_STATUS_HIDDEN, p.Status)
	})
}
