package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// mockVoteKeeper implements types.VoteKeeper for anonymous action tests.
type mockVoteKeeper struct {
	verifyFn func(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error
}

func (m *mockVoteKeeper) VerifyAnonymousActionProof(ctx context.Context, proof, nullifier, merkleRoot []byte, minTrustLevel uint32) error {
	if m.verifyFn != nil {
		return m.verifyFn(ctx, proof, nullifier, merkleRoot, minTrustLevel)
	}
	return nil // dev mode: accept all
}

// initAnonFixture sets up a testFixture with voteKeeper wired in before creating msgServer.
func initAnonFixture(t *testing.T, vk types.VoteKeeper) *testFixture {
	t.Helper()
	f := initTestFixture(t)
	if vk != nil {
		f.keeper.SetVoteKeeper(vk)
	}
	// Rebuild msgServer and queryServer with the updated keeper (value copy)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)
	f.queryServer = keeper.NewQueryServerImpl(f.keeper)
	return f
}

func TestAnonymousReact_Success_Upvote_Collection(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create a public active collection with community feedback
	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	resp, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1, // upvote
		Proof:         []byte("proof"),
		Nullifier:     []byte("nullifier1"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify upvote count increased
	coll, _ = f.keeper.Collection.Get(f.ctx, collID)
	require.Equal(t, uint64(1), coll.UpvoteCount)
	require.Equal(t, uint64(0), coll.DownvoteCount)
}

func TestAnonymousReact_Success_Downvote_Item(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create a collection with community feedback and add an item
	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	itemID := f.addItem(t, collID, f.owner)

	resp, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      itemID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_ITEM,
		ReactionType:  2, // downvote
		Proof:         []byte("proof"),
		Nullifier:     []byte("nullifier2"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify downvote count increased on item
	item, _ := f.keeper.Item.Get(f.ctx, itemID)
	require.Equal(t, uint64(0), item.UpvoteCount)
	require.Equal(t, uint64(1), item.DownvoteCount)
}

func TestAnonymousReact_Disabled(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Disable anonymous posting
	params, _ := f.keeper.Params.Get(f.ctx)
	params.AnonymousPostingEnabled = false
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrAnonymousPostingDisabled)
}

func TestAnonymousReact_NoVoteKeeper(t *testing.T) {
	// Pass nil for vote keeper
	f := initAnonFixture(t, nil)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrAnonymousPostingUnavailable)
}

func TestAnonymousReact_InvalidProof(t *testing.T) {
	vk := &mockVoteKeeper{
		verifyFn: func(_ context.Context, _, _, _ []byte, _ uint32) error {
			return fmt.Errorf("proof verification failed")
		},
	}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("bad_proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrInvalidZKProof)
}

func TestAnonymousReact_NullifierUsed(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	msg := &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("same_null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	}

	// First should succeed
	_, err := f.msgServer.AnonymousReact(f.ctx, msg)
	require.NoError(t, err)

	// Second with same nullifier should fail
	_, err = f.msgServer.AnonymousReact(f.ctx, msg)
	require.ErrorIs(t, err, types.ErrNullifierUsed)
}

func TestAnonymousReact_InsufficientTrustLevel(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	// Default anonymous min trust level is 2; prove only level 1
	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 1, // below required 2
	})
	require.ErrorIs(t, err, types.ErrInsufficientAnonTrustLevel)
}

func TestAnonymousReact_InvalidReactionType(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  3, // invalid
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrInvalidReactionType)
}

func TestAnonymousReact_CollectionNotFound(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      9999,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrCollectionNotFound)
}

func TestAnonymousReact_PrivateCollection(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Visibility = types.Visibility_VISIBILITY_PRIVATE
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrCannotRatePrivate)
}

func TestAnonymousReact_FeedbackDisabled(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = false
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrNotPublicActive)
}

func TestAnonymousReact_DownvoteBurnsCost(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	var escrowCalled bool
	var burnCalled bool
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
		escrowCalled = true
		return nil
	}
	f.bankKeeper.burnCoinsFn = func(_ context.Context, _ string, _ sdk.Coins) error {
		burnCalled = true
		return nil
	}

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  2, // downvote
		Proof:         []byte("proof"),
		Nullifier:     []byte("null_dv"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.True(t, escrowCalled, "downvote should escrow SPARK")
	require.True(t, burnCalled, "downvote should burn SPARK")
}

func TestAnonymousReact_DownvoteInsufficientFunds(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	// Create collection first with default (succeeding) bank mock
	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	// Now set bank mock to fail for the downvote escrow
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
		return fmt.Errorf("insufficient funds")
	}

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  2, // downvote
		Proof:         []byte("proof"),
		Nullifier:     []byte("null_fail"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrInsufficientFunds)
}

func TestAnonymousReact_UpvoteNoBurn(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	var escrowCalled bool
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
		escrowCalled = true
		return nil
	}

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1, // upvote
		Proof:         []byte("proof"),
		Nullifier:     []byte("null_up"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.False(t, escrowCalled, "upvote should not escrow/burn SPARK")
}

func TestAnonymousReact_DownvoteZeroCostNoBurn(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	params, _ := f.keeper.Params.Get(f.ctx)
	params.DownvoteCost = math.ZeroInt()
	f.keeper.Params.Set(f.ctx, params) //nolint:errcheck

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	var escrowCalled bool
	f.bankKeeper.sendCoinsFromAccountToModuleFn = func(_ context.Context, _ sdk.AccAddress, _ string, _ sdk.Coins) error {
		escrowCalled = true
		return nil
	}

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  2,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null_zc"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.NoError(t, err)
	require.False(t, escrowCalled, "zero downvote cost should not escrow")
}

func TestAnonymousReact_HiddenCollection(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)
	coll, _ := f.keeper.Collection.Get(f.ctx, collID)
	coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN
	coll.CommunityFeedbackEnabled = true
	f.keeper.Collection.Set(f.ctx, collID, coll) //nolint:errcheck

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      collID,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.ErrorIs(t, err, types.ErrNotPublicActive)
}

func TestAnonymousReact_InvalidTargetType(t *testing.T) {
	vk := &mockVoteKeeper{}
	f := initAnonFixture(t, vk)
	f.setBlockHeight(100)

	_, err := f.msgServer.AnonymousReact(f.ctx, &types.MsgAnonymousReact{
		Submitter:     f.member,
		TargetId:      1,
		TargetType:    types.FlagTargetType_FLAG_TARGET_TYPE_UNSPECIFIED,
		ReactionType:  1,
		Proof:         []byte("proof"),
		Nullifier:     []byte("null"),
		MerkleRoot:    []byte("root"),
		MinTrustLevel: 2,
	})
	require.Error(t, err)
}
