package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/collections"
	sdkmath "cosmossdk.io/math"
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
	committedAmount  sdkmath.Int       // per-action reserved bond GetActionCommittedAmount reports (zero/unset => no slash)
	resolvedCalls    []string          // records OnSentinelActionResolved "<actionType>:<actionTarget>"
	reverseCalls     []string          // records ReverseSentinelAction invocations
	resolvedError    error
	reverseError     error
	getSentinelError error
	// anyAuthor, when set, is the author of every post not in authors.
	anyAuthor string
}

func mockForumKey(actionType types.GovActionType, target string) string {
	return fmt.Sprintf("%d:%s", int32(actionType), target)
}

func (m *mockForumKeeper) PruneTagReferences(_ context.Context, _ string) error { return nil }

func (m *mockForumKeeper) GetPostAuthor(_ context.Context, postID uint64) (string, error) {
	a, ok := m.authors[postID]
	if !ok {
		if m.anyAuthor != "" {
			return m.anyAuthor, nil
		}
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

func (m *mockForumKeeper) OnSentinelActionResolved(_ context.Context, actionType types.GovActionType, actionTarget string) error {
	m.resolvedCalls = append(m.resolvedCalls, mockForumKey(actionType, actionTarget))
	return m.resolvedError
}

func (m *mockForumKeeper) ReverseSentinelAction(_ context.Context, actionType types.GovActionType, actionTarget string) error {
	m.reverseCalls = append(m.reverseCalls, mockForumKey(actionType, actionTarget))
	return m.reverseError
}

func (m *mockForumKeeper) GetActionCommittedAmount(_ context.Context, _ types.GovActionType, _ string) (sdkmath.Int, error) {
	if m.committedAmount.IsNil() {
		return sdkmath.ZeroInt(), nil
	}
	return m.committedAmount, nil
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

// TestAwardFromTagBudgetAwarderEpochCap: one group member may award at most
// tag_budget_awarder_epoch_cap_bps (default 25%) of a pool per epoch, however
// many posts by other authors they spread it over. The allowance is per
// awarder and resets in the next epoch.
func TestAwardFromTagBudgetAwarderEpochCap(t *testing.T) {
	f := initFixture(t)
	fk := &mockForumKeeper{authors: map[uint64]string{}, tags: map[uint64][]string{}}
	f.keeper.SetForumKeeper(fk)
	ms := keeper.NewMsgServerImpl(f.keeper)

	str := func(seed string) string {
		s, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte(seed)[:20]))
		require.NoError(t, err)
		return s
	}
	group := str("tb-cap-group........")
	phoenix := str("tb-cap-phoenix......")
	aurora := str("tb-cap-aurora.......")
	for i := uint64(1); i <= 6; i++ {
		fk.authors[i] = str(fmt.Sprintf("tb-cap-author-%d.....", i))
		fk.tags[i] = []string{"gopher"}
	}
	id, err := f.keeper.TagBudgetSeq.Next(f.ctx)
	require.NoError(t, err)
	require.NoError(t, f.keeper.TagBudget.Set(f.ctx, id, types.TagBudget{
		Id: id, GroupAccount: group, Tag: "gopher", PoolBalance: "1000", Active: true,
	}))
	award := func(ctx sdk.Context, by string, post uint64, amount string) error {
		_, err := ms.AwardFromTagBudget(ctx, &types.MsgAwardFromTagBudget{
			Creator: by, BudgetId: id, PostId: post, Amount: amount,
		})
		return err
	}

	// phoenix: 200 + 50 = 250, exactly 25% of the 1000 pool.
	require.NoError(t, award(f.ctx, phoenix, 1, "200"))
	require.NoError(t, award(f.ctx, phoenix, 2, "50"))
	err = award(f.ctx, phoenix, 3, "1")
	require.ErrorIs(t, err, types.ErrTagBudgetAwarderCap)

	// aurora has her own allowance: 25% of the pool before her awards (750).
	require.NoError(t, award(f.ctx, aurora, 3, "187"))
	require.ErrorIs(t, award(f.ctx, aurora, 4, "1"), types.ErrTagBudgetAwarderCap)

	// Next epoch: phoenix may award again, and the old epoch's row is dropped.
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	next := f.ctx.WithBlockHeight(f.ctx.BlockHeight() + params.EpochBlocks)
	require.NoError(t, award(next, phoenix, 5, "100"))
	rows := 0
	require.NoError(t, f.keeper.TagBudgetAwarderEpoch.Walk(next, nil,
		func(k collections.Triple[uint64, string, int64], _ sdkmath.Int) (bool, error) {
			if k.K2() == phoenix {
				rows++
			}
			return false, nil
		}))
	require.Equal(t, 1, rows, "one row per (budget, awarder)")

	// The cap is a param: 10000 bps lifts it to the single-award cap only.
	params.TagBudgetAwarderEpochCapBps = 10000
	require.NoError(t, f.keeper.Params.Set(next, params))
	require.NoError(t, award(next, aurora, 6, "100"))

	params.TagBudgetAwarderEpochCapBps = 10001
	require.Error(t, params.Validate())
}
