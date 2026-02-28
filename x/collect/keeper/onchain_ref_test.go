package keeper_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// mockBlogKeeperForRef implements types.BlogKeeper for onchain ref tests.
type mockBlogKeeperForRef struct {
	hasPostFn  func(ctx context.Context, id uint64) bool
	hasReplyFn func(ctx context.Context, id uint64) bool
}

func (m *mockBlogKeeperForRef) HasPost(ctx context.Context, id uint64) bool {
	if m.hasPostFn != nil {
		return m.hasPostFn(ctx, id)
	}
	return true
}

func (m *mockBlogKeeperForRef) HasReply(ctx context.Context, id uint64) bool {
	if m.hasReplyFn != nil {
		return m.hasReplyFn(ctx, id)
	}
	return true
}

// Tests for validateOnChainReference are done indirectly through AddItem
// since validateOnChainReference is unexported.
// AddItem calls validateOnChainReference when ReferenceType is ON_CHAIN.

func TestOnChainRef_BlogPost_Exists(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	bk := &mockBlogKeeperForRef{
		hasPostFn: func(_ context.Context, id uint64) bool { return id == 42 },
	}
	f.keeper.SetBlogKeeper(bk)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "blog-ref-item",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "post",
			EntityId:   "42",
		},
	})
	require.NoError(t, err)
}

func TestOnChainRef_BlogPost_NotFound(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	bk := &mockBlogKeeperForRef{
		hasPostFn: func(_ context.Context, id uint64) bool { return false },
	}
	f.keeper.SetBlogKeeper(bk)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "missing-ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "post",
			EntityId:   "999",
		},
	})
	require.ErrorIs(t, err, types.ErrOnChainRefNotFound)
}

func TestOnChainRef_BlogReply_Exists(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	bk := &mockBlogKeeperForRef{
		hasReplyFn: func(_ context.Context, id uint64) bool { return id == 10 },
	}
	f.keeper.SetBlogKeeper(bk)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "reply-ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "reply",
			EntityId:   "10",
		},
	})
	require.NoError(t, err)
}

func TestOnChainRef_BlogReply_NotFound(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	bk := &mockBlogKeeperForRef{
		hasReplyFn: func(_ context.Context, id uint64) bool { return false },
	}
	f.keeper.SetBlogKeeper(bk)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "missing-reply-ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "reply",
			EntityId:   "55",
		},
	})
	require.ErrorIs(t, err, types.ErrOnChainRefNotFound)
}

func TestOnChainRef_Blog_InvalidEntityType(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	bk := &mockBlogKeeperForRef{}
	f.keeper.SetBlogKeeper(bk)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "bad-entity-type",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "comment", // invalid for blog
			EntityId:   "1",
		},
	})
	require.ErrorIs(t, err, types.ErrInvalidOnChainRef)
}

func TestOnChainRef_Blog_InvalidEntityId(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	bk := &mockBlogKeeperForRef{}
	f.keeper.SetBlogKeeper(bk)
	f.msgServer = keeper.NewMsgServerImpl(f.keeper)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "bad-id",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "post",
			EntityId:   "not_a_number",
		},
	})
	require.ErrorIs(t, err, types.ErrInvalidOnChainRef)
}

func TestOnChainRef_Blog_NilKeeper(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	// Do NOT set blog keeper - leave nil

	collID := f.createCollection(t, f.owner)

	// Should pass (skip validation when keeper is nil)
	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "nil-blog-keeper",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "blog",
			EntityType: "post",
			EntityId:   "42",
		},
	})
	require.NoError(t, err)
}

func TestOnChainRef_ForumPost_Exists(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)
	// forumKeeper is already set in initTestFixture; HasPost returns true by default

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "forum-ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "forum",
			EntityType: "post",
			EntityId:   "1",
		},
	})
	require.NoError(t, err)
}

func TestOnChainRef_ForumReply_Exists(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "forum-reply-ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "forum",
			EntityType: "reply",
			EntityId:   "1",
		},
	})
	require.NoError(t, err)
}

func TestOnChainRef_Forum_InvalidEntityType(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "bad-forum-entity",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "forum",
			EntityType: "thread", // invalid
			EntityId:   "1",
		},
	})
	require.ErrorIs(t, err, types.ErrInvalidOnChainRef)
}

func TestOnChainRef_Forum_InvalidEntityId(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "bad-forum-id",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "forum",
			EntityType: "post",
			EntityId:   "abc",
		},
	})
	require.ErrorIs(t, err, types.ErrInvalidOnChainRef)
}

func TestOnChainRef_UnknownModule(t *testing.T) {
	f := initTestFixture(t)
	f.setBlockHeight(100)

	collID := f.createCollection(t, f.owner)

	// Unknown modules should pass (forward-compatible)
	_, err := f.msgServer.AddItem(f.ctx, &types.MsgAddItem{
		Creator:      f.owner,
		CollectionId: collID,
		Title:        "unknown-module-ref",
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_ON_CHAIN,
		OnChain: &types.OnChainReference{
			Module:     "custom_module",
			EntityType: "widget",
			EntityId:   "123",
		},
	})
	require.NoError(t, err)
}
