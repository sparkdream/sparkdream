package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// Deferred vote-quest progress (SEA-103). The vote hook runs inside a council
// vote with little gas headroom, and advancing every open quest there cost a
// quest read and a progress write each: at the default quest cap the hook
// alone went over a council vote's 200000 gas. The hook now records one
// (member, proposal) key, and the season EndBlocker of the same block
// applies the votes to the member's open quests. StartQuest and
// ClaimQuestReward apply the member's own votes first, so a claim in the
// same block as the vote sees them and a quest started after the vote does
// not.

// addPendingVoteQuestProgress records member's vote on proposalID for the
// EndBlocker. Nothing is written when the member has no open quest.
func (k Keeper) addPendingVoteQuestProgress(ctx context.Context, member string, proposalID uint64) error {
	open := false
	if err := k.MemberQuestProgress.Indexes.Open.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](member+questOpenSuffix),
		func(string, string) (bool, error) {
			open = true
			return true, nil
		}); err != nil {
		return err
	}
	if !open {
		return nil
	}
	return k.PendingVoteQuestProgress.Set(ctx, collections.Join(member, proposalID))
}

// applyPendingVoteQuestProgress applies and clears member's pending votes.
func (k Keeper) applyPendingVoteQuestProgress(ctx context.Context, member string) error {
	var keys []collections.Pair[string, uint64]
	if err := k.PendingVoteQuestProgress.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](member),
		func(key collections.Pair[string, uint64]) (bool, error) {
			keys = append(keys, key)
			return false, nil
		}); err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	for _, key := range keys {
		if err := k.PendingVoteQuestProgress.Remove(ctx, key); err != nil {
			return err
		}
	}
	addr, err := k.addressCodec.StringToBytes(member)
	if err != nil {
		return nil // never written with a bad address
	}
	return k.OnQuestObjectiveProgress(ctx, addr, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, uint64(len(keys)))
}

// ApplyPendingVoteQuestProgress drains the pending votes (the voters of one
// block), each member in its own cache context. A failure is logged and that
// member's votes are dropped: it is quest progress, never funds, and a
// leftover would be retried every block.
func (k Keeper) ApplyPendingVoteQuestProgress(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var members []string
	if err := k.PendingVoteQuestProgress.Walk(ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
		if n := len(members); n == 0 || members[n-1] != key.K1() {
			members = append(members, key.K1())
		}
		return false, nil
	}); err != nil {
		sdkCtx.Logger().Error("failed to walk pending vote quest progress", "error", err)
		return
	}
	for _, m := range members {
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.applyPendingVoteQuestProgress(cacheCtx, m); err != nil {
			sdkCtx.Logger().Error("failed to apply vote quest progress; dropped", "member", m, "error", err)
			if rErr := k.PendingVoteQuestProgress.Clear(ctx, collections.NewPrefixedPairRange[string, uint64](m)); rErr != nil {
				sdkCtx.Logger().Error("failed to drop vote quest progress", "member", m, "error", rErr)
			}
			continue
		}
		write()
	}
}
