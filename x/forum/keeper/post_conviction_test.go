package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// seedActivePostWithTags inserts a synthetic Post in ACTIVE status and
// returns its id. Used by conviction-stake tests that need a target post.
func seedActivePostWithTags(t *testing.T, f *fixture, author sdk.AccAddress, tags []string) uint64 {
	t.Helper()
	id, err := f.keeper.PostSeq.Next(f.ctx)
	require.NoError(t, err)
	post := types.Post{
		PostId:    id,
		Author:    author.String(),
		Content:   "test",
		CreatedAt: sdk.UnwrapSDKContext(f.ctx).BlockTime().Unix(),
		Status:    types.PostStatus_POST_STATUS_ACTIVE,
		Tags:      tags,
	}
	require.NoError(t, f.keeper.Post.Set(f.ctx, id, post))
	return id
}

func TestOpenPostConvictionStake_RejectsSelfStake(t *testing.T) {
	f := initFixture(t)
	author := sdk.AccAddress([]byte("post-conv-author"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	_, err := f.keeper.OpenPostConvictionStake(f.ctx, author, postID, math.NewInt(10_000_000))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot stake on their own post")
}

func TestOpenPostConvictionStake_RejectsBelowMin(t *testing.T) {
	f := initFixture(t)
	author := sdk.AccAddress([]byte("post-conv-author2"))
	staker := sdk.AccAddress([]byte("post-conv-staker2"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	// 1 uDREAM is well below the 10 DREAM (10_000_000 uDREAM) default floor.
	_, err := f.keeper.OpenPostConvictionStake(f.ctx, staker, postID, math.NewInt(1))
	require.Error(t, err)
	require.Contains(t, err.Error(), "below min")
}

func TestAccruePostConvictions_CreditsAuthorAndSplitsAcrossTags(t *testing.T) {
	f := initFixture(t)

	author := sdk.AccAddress([]byte("post-conv-author3"))
	staker := sdk.AccAddress([]byte("post-conv-staker3"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend", "frontend"})

	// Anchor block time so elapsed math is deterministic.
	sdkCtx := sdk.UnwrapSDKContext(f.ctx)
	t0 := time.Unix(0, 0).UTC()
	f.ctx = sdkCtx.WithBlockTime(t0)

	amount := math.NewInt(10_000_000) // 10 DREAM
	_, err := f.keeper.OpenPostConvictionStake(f.ctx, staker, postID, amount)
	require.NoError(t, err)

	// Advance one day and run accrual.
	oneDay := int64(86400)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(t0.Add(time.Duration(oneDay) * time.Second))
	require.NoError(t, f.keeper.AccruePostConvictions(f.ctx))

	// Expected per-tag credit:
	//   amount_dream = 10
	//   elapsed_days = 1
	//   rate = 0.05 (default)
	//   total = 10 * 1 * 0.05 = 0.5
	//   per_tag = 0.5 / 2 = 0.25
	want := math.LegacyMustNewDecFromStr("0.25")
	gotBackend := f.repKeeper.forumRepBalances[forumRepKey(author.String(), "backend")]
	gotFrontend := f.repKeeper.forumRepBalances[forumRepKey(author.String(), "frontend")]
	require.True(t, gotBackend.Equal(want), "backend credit: got %s want %s", gotBackend, want)
	require.True(t, gotFrontend.Equal(want), "frontend credit: got %s want %s", gotFrontend, want)
}

func TestAccruePostConvictions_EnforcesPerEpochCap(t *testing.T) {
	f := initFixture(t)

	// Lower the cap so the default 10-DREAM-over-1-day accrual saturates it.
	params, _ := f.keeper.Params.Get(f.ctx)
	params.MaxForumRepPerTagPerEpoch = math.LegacyMustNewDecFromStr("0.1") // way below 0.25/tag/day
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	author := sdk.AccAddress([]byte("post-conv-cap-author"))
	staker := sdk.AccAddress([]byte("post-conv-cap-staker"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	t0 := time.Unix(0, 0).UTC()
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(t0)

	_, err := f.keeper.OpenPostConvictionStake(f.ctx, staker, postID, math.NewInt(10_000_000))
	require.NoError(t, err)

	// One day elapsed — uncapped accrual would credit 0.5/tag (single tag).
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(t0.Add(time.Duration(86400) * time.Second))
	require.NoError(t, f.keeper.AccruePostConvictions(f.ctx))

	got := f.repKeeper.forumRepBalances[forumRepKey(author.String(), "backend")]
	want := math.LegacyMustNewDecFromStr("0.1")
	require.True(t, got.Equal(want), "cap should clip credit at 0.1, got %s", got)
}

func TestSlashStakesForPost_ClawsBackExactRepAndBurnsStaker(t *testing.T) {
	f := initFixture(t)

	author := sdk.AccAddress([]byte("post-conv-slash-author"))
	staker := sdk.AccAddress([]byte("post-conv-slash-staker"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	t0 := time.Unix(0, 0).UTC()
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(t0)

	amount := math.NewInt(10_000_000) // 10 DREAM
	stakeID, err := f.keeper.OpenPostConvictionStake(f.ctx, staker, postID, amount)
	require.NoError(t, err)

	// Accrue some rep over a day.
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(t0.Add(time.Duration(86400) * time.Second))
	require.NoError(t, f.keeper.AccruePostConvictions(f.ctx))
	credited := f.repKeeper.forumRepBalances[forumRepKey(author.String(), "backend")]
	require.True(t, credited.IsPositive(), "accrual produced no rep — test bug")

	// Run the slash path (would be triggered from ExpireHiddenPosts on a
	// confirmed hide).
	require.NoError(t, f.keeper.SlashStakesForPost(f.ctx, postID))

	// Author rep should be clawed back to zero (or floored there).
	authorRep := f.repKeeper.forumRepBalances[forumRepKey(author.String(), "backend")]
	require.True(t, authorRep.IsZero(), "author rep should be clawed back to zero, got %s", authorRep)

	// DeductForumRep should have been called with the exact amount credited
	// — no over-slash beyond what was produced.
	require.Len(t, f.repKeeper.forumRepDeductCalls, 1)
	require.True(t, f.repKeeper.forumRepDeductCalls[0].Amount.Equal(credited),
		"clawback amount %s should equal credited %s",
		f.repKeeper.forumRepDeductCalls[0].Amount, credited)

	// 25% staker slash (default) = 2_500_000 uDREAM. Should be unlocked then
	// burned. The remaining 7_500_000 should also be unlocked (post is
	// tombstoned, no further accrual possible).
	require.Len(t, f.repKeeper.burnDreamCalls, 1)
	burned := f.repKeeper.burnDreamCalls[0].Amount
	require.Equal(t, math.NewInt(2_500_000), burned, "expected 25%% slash burn")

	// 2 unlocks: slash portion + remaining.
	require.Len(t, f.repKeeper.unlockDreamCalls, 2)
	totalUnlocked := f.repKeeper.unlockDreamCalls[0].Amount.Add(f.repKeeper.unlockDreamCalls[1].Amount)
	require.Equal(t, amount, totalUnlocked, "total unlocked should equal original stake")

	// Stake should be marked released and dropped from active indexes.
	stake, err := f.keeper.PostConvictionStake.Get(f.ctx, stakeID)
	require.NoError(t, err)
	require.True(t, stake.Released, "stake should be released after slash")
}

func TestReleasePostConvictionStake_RequiresLockWindowToPass(t *testing.T) {
	f := initFixture(t)

	author := sdk.AccAddress([]byte("post-conv-rel-author"))
	staker := sdk.AccAddress([]byte("post-conv-rel-staker"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	t0 := time.Unix(0, 0).UTC()
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(t0)

	stakeID, err := f.keeper.OpenPostConvictionStake(f.ctx, staker, postID, math.NewInt(10_000_000))
	require.NoError(t, err)

	// Before window closes: rejected.
	err = f.keeper.ReleasePostConvictionStake(f.ctx, stakeID, staker)
	require.Error(t, err)
	require.Contains(t, err.Error(), "locked until")

	// Past unlock time: succeeds.
	params, _ := f.keeper.Params.Get(f.ctx)
	f.ctx = sdk.UnwrapSDKContext(f.ctx).WithBlockTime(
		t0.Add(time.Duration(params.PostConvictionLockSeconds+1) * time.Second),
	)

	require.NoError(t, f.keeper.ReleasePostConvictionStake(f.ctx, stakeID, staker))

	stake, err := f.keeper.PostConvictionStake.Get(f.ctx, stakeID)
	require.NoError(t, err)
	require.True(t, stake.Released, "stake should be released")
}

// Ensure trust-level enforcement on the msg-server path. The default mock
// returns ESTABLISHED, so we override it to NEW for this test.
func TestStakePostConviction_RejectsBelowEstablished(t *testing.T) {
	f := initFixture(t)
	author := sdk.AccAddress([]byte("post-conv-tl-author"))
	staker := sdk.AccAddress([]byte("post-conv-tl-staker"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	f.repKeeper.GetTrustLevelFn = func(_ sdk.AccAddress) uint64 {
		return uint64(0) // TRUST_LEVEL_NEW
	}

	stakerStr, _ := f.addressCodec.BytesToString(staker)
	_, err := f.msgServer.StakePostConviction(f.ctx, &types.MsgStakePostConviction{
		Creator: stakerStr,
		PostId:  postID,
		Amount:  math.NewInt(10_000_000),
	})
	require.Error(t, err)
	require.True(t,
		containsStr(err.Error(), "ESTABLISHED+") || containsStr(err.Error(), "only active members"),
		"expected trust-level / membership rejection, got: %v", err,
	)
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
