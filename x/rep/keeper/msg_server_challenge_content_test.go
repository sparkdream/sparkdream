package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgChallengeContent_Success(t *testing.T) {
	f, authorAddr, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	stakeAmount := math.NewInt(100000000) // 100 DREAM
	msg := &types.MsgChallengeContent{
		Challenger:  challengerAddr.String(),
		TargetType:  uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:    1,
		Reason:      "Inaccurate content",
		Evidence:    []string{"evidence_link_1"},
		StakedDream: &stakeAmount,
	}

	resp, err := msgServer.ChallengeContent(f.ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotZero(t, resp.ContentChallengeId)

	// Verify the challenge was stored
	cc, err := f.keeper.ContentChallenge.Get(f.ctx, resp.ContentChallengeId)
	require.NoError(t, err)
	require.Equal(t, challengerAddr.String(), cc.Challenger)
	require.Equal(t, authorAddr.String(), cc.Author)
	require.Equal(t, types.ContentChallengeStatus_CONTENT_CHALLENGE_STATUS_ACTIVE, cc.Status)
}

func TestMsgChallengeContent_InvalidAddress(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	stakeAmount := math.NewInt(100)
	msg := &types.MsgChallengeContent{
		Challenger:  "invalid_address",
		TargetType:  uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:    1,
		Reason:      "Bad content",
		Evidence:    nil,
		StakedDream: &stakeAmount,
	}

	_, err := msgServer.ChallengeContent(f.ctx, msg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid challenger address")
}

func TestMsgChallengeContent_NilStakedDream(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	msg := &types.MsgChallengeContent{
		Challenger:  challengerAddr.String(),
		TargetType:  uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:    1,
		Reason:      "Bad content",
		Evidence:    nil,
		StakedDream: nil, // nil staked dream
	}

	_, err := msgServer.ChallengeContent(f.ctx, msg)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrInvalidAmount)
}

func TestMsgChallengeContent_InvalidTargetType(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	stakeAmount := math.NewInt(100)
	msg := &types.MsgChallengeContent{
		Challenger:  challengerAddr.String(),
		TargetType:  uint64(types.StakeTargetType_STAKE_TARGET_INITIATIVE),
		TargetId:    1,
		Reason:      "Bad content",
		Evidence:    nil,
		StakedDream: &stakeAmount,
	}

	_, err := msgServer.ChallengeContent(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrNotAuthorBondType)
}

func TestMsgChallengeContent_NoBond(t *testing.T) {
	f, _, challengerAddr := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	stakeAmount := math.NewInt(100)
	msg := &types.MsgChallengeContent{
		Challenger:  challengerAddr.String(),
		TargetType:  uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:    999, // No bond on this content
		Reason:      "Bad content",
		Evidence:    nil,
		StakedDream: &stakeAmount,
	}

	_, err := msgServer.ChallengeContent(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrNoAuthorBond)
}

func TestMsgChallengeContent_SelfChallenge(t *testing.T) {
	f, authorAddr, _ := setupContentChallengeFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)

	stakeAmount := math.NewInt(100)
	msg := &types.MsgChallengeContent{
		Challenger:  authorAddr.String(),
		TargetType:  uint64(types.StakeTargetType_STAKE_TARGET_BLOG_AUTHOR_BOND),
		TargetId:    1,
		Reason:      "Self challenge",
		Evidence:    nil,
		StakedDream: &stakeAmount,
	}

	_, err := msgServer.ChallengeContent(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrCannotChallengeOwnContent)
}
