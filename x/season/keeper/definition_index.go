package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/season/types"
)

// Achievement and title definitions are written only through these helpers
// so the (requirement type, id) indexes stay in step with the maps. The
// indexes let the per-action hooks (vote, forum, initiative, ...) read only
// the handful of definitions a changed counter can satisfy instead of
// decoding every definition inside the user's transaction.

// SetAchievement writes an achievement and its requirement-type index entry.
func (k Keeper) SetAchievement(ctx context.Context, a types.Achievement) error {
	if old, err := k.Achievement.Get(ctx, a.AchievementId); err == nil && old.RequirementType != a.RequirementType {
		if err := k.AchievementsByRequirement.Remove(ctx, collections.Join(int32(old.RequirementType), a.AchievementId)); err != nil {
			return err
		}
	}
	if err := k.Achievement.Set(ctx, a.AchievementId, a); err != nil {
		return err
	}
	return k.AchievementsByRequirement.Set(ctx, collections.Join(int32(a.RequirementType), a.AchievementId))
}

// RemoveAchievement deletes an achievement and its index entry.
func (k Keeper) RemoveAchievement(ctx context.Context, id string) error {
	if old, err := k.Achievement.Get(ctx, id); err == nil {
		if err := k.AchievementsByRequirement.Remove(ctx, collections.Join(int32(old.RequirementType), id)); err != nil {
			return err
		}
	}
	return k.Achievement.Remove(ctx, id)
}

// SetTitle writes a title and its requirement-type index entry.
func (k Keeper) SetTitle(ctx context.Context, t types.Title) error {
	if old, err := k.Title.Get(ctx, t.TitleId); err == nil && old.RequirementType != t.RequirementType {
		if err := k.TitlesByRequirement.Remove(ctx, collections.Join(int32(old.RequirementType), t.TitleId)); err != nil {
			return err
		}
	}
	if err := k.Title.Set(ctx, t.TitleId, t); err != nil {
		return err
	}
	return k.TitlesByRequirement.Set(ctx, collections.Join(int32(t.RequirementType), t.TitleId))
}

// RemoveTitle deletes a title and its index entry.
func (k Keeper) RemoveTitle(ctx context.Context, id string) error {
	if old, err := k.Title.Get(ctx, id); err == nil {
		if err := k.TitlesByRequirement.Remove(ctx, collections.Join(int32(old.RequirementType), id)); err != nil {
			return err
		}
	}
	return k.Title.Remove(ctx, id)
}

// achievementsWithRequirement returns the achievements indexed under the
// given requirement types, in id order per type.
func (k Keeper) achievementsWithRequirement(ctx context.Context, reqTypes []types.RequirementType) ([]types.Achievement, error) {
	var out []types.Achievement
	for _, rt := range reqTypes {
		rng := collections.NewPrefixedPairRange[int32, string](int32(rt))
		if err := k.AchievementsByRequirement.Walk(ctx, rng, func(key collections.Pair[int32, string]) (bool, error) {
			a, err := k.Achievement.Get(ctx, key.K2())
			if err == nil {
				out = append(out, a)
			}
			return false, nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// titlesWithRequirement is achievementsWithRequirement for titles.
func (k Keeper) titlesWithRequirement(ctx context.Context, reqTypes []types.RequirementType) ([]types.Title, error) {
	var out []types.Title
	for _, rt := range reqTypes {
		rng := collections.NewPrefixedPairRange[int32, string](int32(rt))
		if err := k.TitlesByRequirement.Walk(ctx, rng, func(key collections.Pair[int32, string]) (bool, error) {
			t, err := k.Title.Get(ctx, key.K2())
			if err == nil {
				out = append(out, t)
			}
			return false, nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}
