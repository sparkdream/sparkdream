package keeper

import (
	"context"
	"fmt"
	"sort"

	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"
)

// ---------------------------------------------------------------------------
// Seasonal titles (x-season-spec "Titles"). Eligibility is decided during
// the SNAPSHOT phase, while season XP is intact, one batch of profiles at a
// time (SEA-05):
//   - threshold titles are evaluated per member and recorded on that
//     member's MemberSeasonSnapshot.eligible_title_ids;
//   - TOP_XP titles keep a running top-K list in SeasonTitleEligibility,
//     capped at maxTopXPHolders.
// The TITLES phase then grants `s<N>_<title>` to each eligible member.
// Seasonal titles are permanent: they never leave the member, only roll into
// the archive past the display cap.
//
// Only "base" definitions (Seasonal && RequirementSeason == 0) are
// evaluated. The per-season copies the ceremony creates carry
// RequirementSeason = N and are never re-evaluated, so definitions do not
// multiply from season to season (SEA-02).
// ---------------------------------------------------------------------------

// maxTopXPHolders caps a TOP_XP title's threshold, bounding the ranked list
// kept in state and the per-batch work to maintain it.
const maxTopXPHolders = 100

// isSeasonalBaseTitle reports whether t is a seasonal title definition the
// ceremony evaluates each season, as opposed to a per-season copy.
func isSeasonalBaseTitle(t types.Title) bool {
	return t.Seasonal && t.RequirementSeason == 0
}

// seasonalBaseTitles returns the seasonal base definitions, sorted by id.
func (k Keeper) seasonalBaseTitles(ctx context.Context) ([]types.Title, error) {
	var seasonal []types.Title
	if err := k.Title.Walk(ctx, nil, func(_ string, t types.Title) (bool, error) {
		if isSeasonalBaseTitle(t) {
			seasonal = append(seasonal, t)
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(seasonal, func(i, j int) bool { return seasonal[i].TitleId < seasonal[j].TitleId })
	return seasonal, nil
}

// beginSeasonTitleEligibility writes the empty eligibility record for a
// season: one grant per TOP_XP base title, to be filled batch by batch.
func (k Keeper) beginSeasonTitleEligibility(ctx context.Context, season uint64) error {
	bases, err := k.seasonalBaseTitles(ctx)
	if err != nil {
		return err
	}
	record := types.SeasonTitleEligibility{TitleSeason: season, Season: season}
	for _, t := range bases {
		if t.RequirementType != types.RequirementType_REQUIREMENT_TYPE_TOP_XP {
			continue
		}
		record.Grants = append(record.Grants, &types.TitleGrant{TitleId: t.TitleId, TitleName: t.Name})
	}
	return k.SeasonTitleEligibility.Set(ctx, season, record)
}

// accumulateSeasonTitleEligibility evaluates one batch of profiles. It
// returns the threshold-title ids each profile met (keyed by address) and
// merges the batch into the running TOP_XP lists.
func (k Keeper) accumulateSeasonTitleEligibility(ctx context.Context, season uint64, batch []types.MemberProfile) (map[string][]string, error) {
	bases, err := k.seasonalBaseTitles(ctx)
	if err != nil {
		return nil, err
	}
	thresholdIDs := make(map[string][]string, len(batch))
	for _, p := range batch {
		for _, t := range bases {
			if t.RequirementType == types.RequirementType_REQUIREMENT_TYPE_TOP_XP {
				continue
			}
			if met, known := k.requirementMet(ctx, p, t.RequirementType, t.RequirementThreshold); known && met {
				thresholdIDs[p.Address] = append(thresholdIDs[p.Address], t.TitleId)
			}
		}
	}

	record, err := k.SeasonTitleEligibility.Get(ctx, season)
	if err != nil {
		return nil, err
	}
	if len(record.Grants) == 0 {
		return thresholdIDs, nil
	}
	limits := make(map[string]int, len(bases))
	for _, t := range bases {
		n := commontypes.Saturate[int](t.RequirementThreshold)
		if n > maxTopXPHolders {
			n = maxTopXPHolders
		}
		limits[t.TitleId] = n
	}

	type ranked struct {
		addr string
		xp   uint64
	}
	// Current XP of the members already ranked, read once per batch.
	xpOf := make(map[string]uint64)
	for _, p := range batch {
		xpOf[p.Address] = p.SeasonXp
	}
	for _, g := range record.Grants {
		if g == nil {
			continue
		}
		seen := make(map[string]bool, len(g.EligibleMembers)+len(batch))
		var cands []ranked
		for _, addr := range g.EligibleMembers {
			xp, ok := xpOf[addr]
			if !ok {
				if p, err := k.MemberProfile.Get(ctx, addr); err == nil {
					xp = p.SeasonXp
				}
				xpOf[addr] = xp
			}
			if xp > 0 && !seen[addr] {
				seen[addr] = true
				cands = append(cands, ranked{addr, xp})
			}
		}
		for _, p := range batch {
			if p.SeasonXp > 0 && !seen[p.Address] {
				seen[p.Address] = true
				cands = append(cands, ranked{p.Address, p.SeasonXp})
			}
		}
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].xp != cands[j].xp {
				return cands[i].xp > cands[j].xp
			}
			return cands[i].addr < cands[j].addr
		})
		n := limits[g.TitleId]
		if n > len(cands) {
			n = len(cands)
		}
		g.EligibleMembers = g.EligibleMembers[:0]
		for _, c := range cands[:n] {
			g.EligibleMembers = append(g.EligibleMembers, c.addr)
		}
	}
	if err := k.SeasonTitleEligibility.Set(ctx, season, record); err != nil {
		return nil, err
	}
	return thresholdIDs, nil
}

// finishSeasonTitleEligibility sorts every TOP_XP list by address (the
// TITLES phase binary-searches it) and drops empty grants.
func (k Keeper) finishSeasonTitleEligibility(ctx context.Context, season uint64) error {
	record, err := k.SeasonTitleEligibility.Get(ctx, season)
	if err != nil {
		return err
	}
	grants := record.Grants[:0]
	for _, g := range record.Grants {
		if g == nil || len(g.EligibleMembers) == 0 {
			continue
		}
		sort.Strings(g.EligibleMembers)
		grants = append(grants, g)
	}
	record.Grants = grants
	return k.SeasonTitleEligibility.Set(ctx, season, record)
}

// ComputeSeasonTitleEligibility runs the whole eligibility pass over every
// profile at once, writing threshold eligibility onto each member's season
// snapshot. The transition does the same work batch by batch inside the
// SNAPSHOT phase; this one-shot form is for tests and tooling.
func (k Keeper) ComputeSeasonTitleEligibility(ctx context.Context, season uint64) error {
	if err := k.beginSeasonTitleEligibility(ctx, season); err != nil {
		return err
	}
	var all []types.MemberProfile
	if err := k.MemberProfile.Walk(ctx, nil, func(_ string, p types.MemberProfile) (bool, error) {
		all = append(all, p)
		return false, nil
	}); err != nil {
		return err
	}
	ids, err := k.accumulateSeasonTitleEligibility(ctx, season, all)
	if err != nil {
		return err
	}
	for _, p := range all {
		key := fmt.Sprintf("%d/%s", season, p.Address)
		snap, err := k.MemberSeasonSnapshot.Get(ctx, key)
		if err != nil {
			snap = types.MemberSeasonSnapshot{SeasonAddress: key}
		}
		snap.EligibleTitleIds = ids[p.Address]
		if err := k.MemberSeasonSnapshot.Set(ctx, key, snap); err != nil {
			return err
		}
	}
	return k.finishSeasonTitleEligibility(ctx, season)
}

// seasonalTitleID is the per-season title id a seasonal definition grants.
func seasonalTitleID(season uint64, titleID string) string {
	return fmt.Sprintf("s%d_%s", season, titleID)
}

// grantSeasonalTitles gives `memberStr` every seasonal title they were found
// eligible for, creating the per-season Title record on first use.
// thresholdIDs are the threshold titles recorded on the member's season
// snapshot; TOP_XP titles come from the eligibility record.
func (k Keeper) grantSeasonalTitles(ctx context.Context, season uint64, eligibility types.SeasonTitleEligibility, memberStr string, thresholdIDs []string) error {
	var baseIDs []string
	baseNames := map[string]string{}
	for _, g := range eligibility.Grants {
		if g == nil {
			continue
		}
		i := sort.SearchStrings(g.EligibleMembers, memberStr)
		if i >= len(g.EligibleMembers) || g.EligibleMembers[i] != memberStr {
			continue
		}
		baseIDs = append(baseIDs, g.TitleId)
		baseNames[g.TitleId] = g.TitleName
	}
	baseIDs = append(baseIDs, thresholdIDs...)
	if len(baseIDs) == 0 {
		return nil
	}

	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	for _, baseID := range baseIDs {
		id := seasonalTitleID(season, baseID)
		if has, err := k.Title.Has(ctx, id); err == nil && !has {
			base, err := k.Title.Get(ctx, baseID)
			if err != nil {
				base = types.Title{TitleId: baseID, Name: baseNames[baseID]}
			}
			// The copy keeps Seasonal (it is a seasonal title) but carries
			// RequirementSeason, which marks it as a per-season copy that is
			// never evaluated as a base again (SEA-02).
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
