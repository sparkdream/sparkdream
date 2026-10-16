package keeper

import (
	"context"
	"fmt"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// ---------------------------------------------------------------------------
// Seasonal titles (x-season-spec "Titles"). Eligibility is decided once per
// season at the start of the SNAPSHOT phase, while season XP is intact, and
// stored as SeasonTitleEligibility; the TITLES phase then grants
// `s<N>_<title>` to each eligible member. Seasonal titles are permanent:
// they never leave the member, only roll into the archive past the display
// cap.
// ---------------------------------------------------------------------------

// ComputeSeasonTitleEligibility walks every profile once and records, per
// seasonal title, the members that meet its requirement. TOP_XP titles take
// the `threshold` highest season XP holders (ties broken by address).
func (k Keeper) ComputeSeasonTitleEligibility(ctx context.Context, season uint64) error {
	var seasonal []types.Title
	if err := k.Title.Walk(ctx, nil, func(_ string, t types.Title) (bool, error) {
		if t.Seasonal {
			seasonal = append(seasonal, t)
		}
		return false, nil
	}); err != nil {
		return err
	}
	sort.Slice(seasonal, func(i, j int) bool { return seasonal[i].TitleId < seasonal[j].TitleId })
	if len(seasonal) == 0 {
		return k.SeasonTitleEligibility.Set(ctx, season, types.SeasonTitleEligibility{TitleSeason: season, Season: season})
	}

	type ranked struct {
		addr string
		xp   uint64
	}
	var byXP []ranked
	eligible := make(map[string][]string, len(seasonal))
	if err := k.MemberProfile.Walk(ctx, nil, func(addr string, p types.MemberProfile) (bool, error) {
		if p.SeasonXp > 0 {
			byXP = append(byXP, ranked{addr: addr, xp: p.SeasonXp})
		}
		for _, t := range seasonal {
			if t.RequirementType == types.RequirementType_REQUIREMENT_TYPE_TOP_XP {
				continue
			}
			if met, known := k.requirementMet(ctx, p, t.RequirementType, t.RequirementThreshold); known && met {
				eligible[t.TitleId] = append(eligible[t.TitleId], addr)
			}
		}
		return false, nil
	}); err != nil {
		return err
	}
	sort.Slice(byXP, func(i, j int) bool {
		if byXP[i].xp != byXP[j].xp {
			return byXP[i].xp > byXP[j].xp
		}
		return byXP[i].addr < byXP[j].addr
	})
	for _, t := range seasonal {
		if t.RequirementType != types.RequirementType_REQUIREMENT_TYPE_TOP_XP {
			continue
		}
		n := int(t.RequirementThreshold)
		if n > len(byXP) {
			n = len(byXP)
		}
		for _, r := range byXP[:n] {
			eligible[t.TitleId] = append(eligible[t.TitleId], r.addr)
		}
	}

	record := types.SeasonTitleEligibility{TitleSeason: season, Season: season}
	for _, t := range seasonal {
		members := eligible[t.TitleId]
		if len(members) == 0 {
			continue
		}
		sort.Strings(members)
		record.Grants = append(record.Grants, &types.TitleGrant{
			TitleId: t.TitleId, TitleName: t.Name, EligibleMembers: members,
		})
	}
	return k.SeasonTitleEligibility.Set(ctx, season, record)
}

// seasonalTitleID is the per-season title id a seasonal definition grants.
func seasonalTitleID(season uint64, titleID string) string {
	return fmt.Sprintf("s%d_%s", season, titleID)
}

// grantSeasonalTitles gives `memberStr` every seasonal title they were found
// eligible for, creating the per-season Title record on first use.
func (k Keeper) grantSeasonalTitles(ctx context.Context, season uint64, eligibility types.SeasonTitleEligibility, memberStr string) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	for _, g := range eligibility.Grants {
		if g == nil {
			continue
		}
		i := sort.SearchStrings(g.EligibleMembers, memberStr)
		if i >= len(g.EligibleMembers) || g.EligibleMembers[i] != memberStr {
			continue
		}
		id := seasonalTitleID(season, g.TitleId)
		if has, err := k.Title.Has(ctx, id); err == nil && !has {
			base, err := k.Title.Get(ctx, g.TitleId)
			if err != nil {
				base = types.Title{TitleId: g.TitleId, Name: g.TitleName}
			}
			if err := k.SetTitle(ctx, types.Title{
				TitleId:              id,
				Name:                 fmt.Sprintf("S%d %s", season, base.Name),
				Description:          base.Description,
				Rarity:               base.Rarity,
				RequirementType:      base.RequirementType,
				RequirementThreshold: base.RequirementThreshold,
				RequirementSeason:    season,
				Seasonal:             true,
			}); err != nil {
				return err
			}
		}
		if err := k.unlockTitle(ctx, env, memberStr, id); err != nil {
			return err
		}
		sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("seasonal_title_granted",
			sdk.NewAttribute("member", memberStr),
			sdk.NewAttribute("title_id", id),
			sdk.NewAttribute("season", fmt.Sprintf("%d", season)),
		))
	}
	return nil
}
