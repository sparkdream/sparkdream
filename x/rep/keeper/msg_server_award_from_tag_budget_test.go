package keeper_test

import (
	"context"
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// mockForumKeeper implements types.ForumKeeper for award-from-tag-budget tests.
// Tests for the gov-action appeal resolution flow populate the extra maps
// (actionSentinels, upheldCalls, overturnedCalls) to observe the adapter
// interactions; award-from-tag-budget tests leave them nil and only use
// authors / tags.
type mockForumKeeper struct {
	authors map[uint64]string
	tags    map[uint64][]string
	// Stage C hooks (populated by appeal-resolve tests):
	actionSentinels  map[string]string // key=<actionType>:<actionTarget>
	upheldCalls      []string          // records "<actionType>:<actionTarget>"
	overturnedCalls  []string
	upheldError      error
	overturnedError  error
	getSentinelError error
	// Stage D hooks (populated by sentinel-reward distribution tests):
	counters  map[string]types.SentinelActivityCounters
	resetAddrs []string
}

func mockForumKey(actionType types.GovActionType, target string) string {
	return fmt.Sprintf("%d:%s", int32(actionType), target)
}

func (m *mockForumKeeper) PruneTagReferences(_ context.Context, _ string) error { return nil }

func (m *mockForumKeeper) GetPostAuthor(_ context.Context, postID uint64) (string, error) {
	a, ok := m.authors[postID]
	if !ok {
		return "", fmt.Errorf("post %d not found", postID)
	}
	return a, nil
}

func (m *mockForumKeeper) GetPostTags(_ context.Context, postID uint64) ([]string, error) {
	t, ok := m.tags[postID]
	if !ok {
		return nil, fmt.Errorf("post %d not found", postID)
	}
	return t, nil
}

func (m *mockForumKeeper) GetActionSentinel(_ context.Context, actionType types.GovActionType, actionTarget string) (string, error) {
	if m.getSentinelError != nil {
		return "", m.getSentinelError
	}
	if m.actionSentinels == nil {
		return "", nil
	}
	return m.actionSentinels[mockForumKey(actionType, actionTarget)], nil
}

func (m *mockForumKeeper) RecordSentinelActionUpheld(_ context.Context, actionType types.GovActionType, actionTarget string) error {
	m.upheldCalls = append(m.upheldCalls, mockForumKey(actionType, actionTarget))
	return m.upheldError
}

func (m *mockForumKeeper) RecordSentinelActionOverturned(_ context.Context, actionType types.GovActionType, actionTarget string) error {
	m.overturnedCalls = append(m.overturnedCalls, mockForumKey(actionType, actionTarget))
	return m.overturnedError
}

func (m *mockForumKeeper) GetSentinelActivityCounters(_ context.Context, addr string) (types.SentinelActivityCounters, error) {
	if m.counters == nil {
		return types.SentinelActivityCounters{}, nil
	}
	c, ok := m.counters[addr]
	if !ok {
		return types.SentinelActivityCounters{}, nil
	}
	return c, nil
}

func (m *mockForumKeeper) ResetSentinelEpochCounters(_ context.Context, addr string) error {
	m.resetAddrs = append(m.resetAddrs, addr)
	if m.counters != nil {
		if c, ok := m.counters[addr]; ok {
			c.EpochHides = 0
			c.EpochLocks = 0
			c.EpochMoves = 0
			c.EpochPins = 0
			c.EpochAppealsFiled = 0
			c.EpochAppealsResolved = 0
			m.counters[addr] = c
		}
	}
	return nil
}

func TestMsgServerAwardFromTagBudget(t *testing.T) {
	setup := func(t *testing.T) (*fixture, types.MsgServer, *mockForumKeeper, string, uint64) {
		f := initFixture(t)
		fk := &mockForumKeeper{
			authors: make(map[uint64]string),
			tags:    make(map[uint64][]string),
		}
		f.keeper.SetForumKeeper(fk)

		owner := sdk.AccAddress([]byte("tb-award-owner......")[:20])
		ownerStr, _ := f.addressCodec.BytesToString(owner)
		id, _ := f.keeper.TagBudgetSeq.Next(f.ctx)
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
			Id: id, GroupAccount: ownerStr, Tag: "gopher", PoolBalance: "1000", Active: true,
		}))

		author := sdk.AccAddress([]byte("tb-award-author.....")[:20])
		authorStr, _ := f.addressCodec.BytesToString(author)
		fk.authors[42] = authorStr
		fk.tags[42] = []string{"gopher"}

		return f, keeper.NewMsgServerImpl(f.keeper), fk, ownerStr, id
	}

	t.Run("invalid creator address", func(t *testing.T) {
		f, ms, _, _, id := setup(t)
		_ = f
		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  "invalid",
			BudgetId: id,
			PostId:   42,
			Amount:   "100",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("budget not found", func(t *testing.T) {
		f, ms, _, owner, _ := setup(t)
		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  owner,
			BudgetId: 999,
			PostId:   42,
			Amount:   "100",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotFound)
	})

	t.Run("budget not active", func(t *testing.T) {
		f, ms, _, owner, id := setup(t)
		b, _ := f.keeper.TagBudget.Get(f.ctx, id)
		b.Active = false
		require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, b))

		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  owner,
			BudgetId: id,
			PostId:   42,
			Amount:   "100",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetNotActive)
	})

	t.Run("post not found", func(t *testing.T) {
		f, ms, _, owner, id := setup(t)
		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  owner,
			BudgetId: id,
			PostId:   999,
			Amount:   "100",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrPostNotFound)
	})

	t.Run("post missing tag", func(t *testing.T) {
		f, ms, fk, owner, id := setup(t)
		fk.tags[42] = []string{"not-gopher"}

		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  owner,
			BudgetId: id,
			PostId:   42,
			Amount:   "100",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidTag)
	})

	t.Run("amount exceeds pool", func(t *testing.T) {
		f, ms, _, owner, id := setup(t)
		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  owner,
			BudgetId: id,
			PostId:   42,
			Amount:   "9999999999",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrTagBudgetInsufficient)
	})

	t.Run("successful award", func(t *testing.T) {
		f, ms, fk, owner, id := setup(t)

		_, err := ms.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
			Creator:  owner,
			BudgetId: id,
			PostId:   42,
			Amount:   "100",
			Reason:   "great post",
		})
		require.NoError(t, err)

		updated, _ := f.keeper.TagBudget.Get(f.ctx, id)
		require.Equal(t, "900", updated.PoolBalance)

		var found bool
		iter, _ := f.keeper.TagBudgetAward.Iterate(f.ctx, nil)
		for ; iter.Valid(); iter.Next() {
			a, _ := iter.Value()
			if a.BudgetId == id && a.PostId == 42 {
				found = true
				require.Equal(t, fk.authors[42], a.Recipient)
				require.Equal(t, "100", a.Amount)
				break
			}
		}
		iter.Close()
		require.True(t, found)
	})
}
