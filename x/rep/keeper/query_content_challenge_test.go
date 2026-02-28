package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"github.com/cosmos/cosmos-sdk/types/query"
)

func TestQueryGetContentChallenge_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetContentChallenge(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestQueryGetContentChallenge_NotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.GetContentChallenge(f.ctx, &types.QueryGetContentChallengeRequest{
		Id: 999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestQueryGetContentChallenge_Found(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Test reason",
		[]string{"ev1"},
		stakeAmount,
	)
	require.NoError(t, err)

	resp, err := qs.GetContentChallenge(f.ctx, &types.QueryGetContentChallengeRequest{
		Id: ccID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, ccID, resp.ContentChallenge.Id)
	require.Equal(t, challengerAddr.String(), resp.ContentChallenge.Challenger)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, resp.ContentChallenge.Status)
	require.Equal(t, "Test reason", resp.ContentChallenge.Reason)
}

func TestQueryListContentChallenge_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ListContentChallenge(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestQueryListContentChallenge_Empty(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	resp, err := qs.ListContentChallenge(f.ctx, &types.QueryAllContentChallengeRequest{
		Pagination: &query.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Empty(t, resp.ContentChallenge)
}

func TestQueryListContentChallenge_WithData(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	// Create first challenge on post #1
	stakeAmount := math.NewInt(100000000)
	_, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"First challenge",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Create bond on post #2 and challenge it
	_, err = f.keeper.CreateAuthorBond(
		f.ctx,
		authorAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		2,
		math.NewInt(500000000),
	)
	require.NoError(t, err)

	_, err = f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		2,
		"Second challenge",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	resp, err := qs.ListContentChallenge(f.ctx, &types.QueryAllContentChallengeRequest{
		Pagination: &query.PageRequest{Limit: 10},
	})
	require.NoError(t, err)
	require.Len(t, resp.ContentChallenge, 2)
}

func TestQueryContentChallengesByTarget_NilRequest(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ContentChallengesByTarget(f.ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
}

func TestQueryContentChallengesByTarget_NotFound(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	_, err := qs.ContentChallengesByTarget(f.ctx, &types.QueryContentChallengesByTargetRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:   999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active content challenge")
}

func TestQueryContentChallengesByTarget_Found(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	resp, err := qs.ContentChallengesByTarget(f.ctx, &types.QueryContentChallengesByTargetRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:   1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, ccID, resp.ContentChallenge.Id)
	require.Equal(t, challengerAddr.String(), resp.ContentChallenge.Challenger)
}

func TestQueryContentChallengesByTarget_AfterResolution(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	stakeAmount := math.NewInt(100000000)
	ccID, err := f.keeper.CreateContentChallenge(
		f.ctx,
		challengerAddr,
		types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND,
		1,
		"Bad content",
		nil,
		stakeAmount,
	)
	require.NoError(t, err)

	// Uphold the challenge (removes target index)
	err = f.keeper.UpholdContentChallenge(f.ctx, ccID)
	require.NoError(t, err)

	// Target index should be cleared
	_, err = qs.ContentChallengesByTarget(f.ctx, &types.QueryContentChallengesByTargetRequest{
		TargetType: uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:   1,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no active content challenge")
}
