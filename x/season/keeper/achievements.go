package keeper

import (
	"context"
	"fmt"
	"sort"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// ---------------------------------------------------------------------------
// Achievements and non-seasonal titles (x-season-spec "Achievements",
// "Titles"). Counters live on MemberProfile; every bump is followed by an
// achievement check, which reads only the definitions the bumped counter can
// satisfy (requirement-type index, see definition_index.go) and grants any
// whose requirement the member now meets. Granting an achievement pays its
// xp_reward through the XP engine (epoch cap applies) and re-checks titles
// and achievements keyed on achievement count or level.
// ---------------------------------------------------------------------------

// profileCounter bumps one achievement counter on the member's profile.
func (k Keeper) profileCounter(ctx context.Context, memberStr string, epoch int64, bump func(p *types.MemberProfile)) error {
	profile := k.getOrCreateProfile(ctx, memberStr, epoch)
	bump(&profile)
	profile.LastActiveEpoch = epoch
	return k.MemberProfile.Set(ctx, memberStr, profile)
}

func (k Keeper) IncrementVotesCast(ctx context.Context, memberStr string) error {
	return k.profileCounter(ctx, memberStr, k.GetCurrentEpoch(ctx), func(p *types.MemberProfile) { p.VotesCast++ })
}

func (k Keeper) IncrementForumHelpfulCount(ctx context.Context, memberStr string) error {
	return k.profileCounter(ctx, memberStr, k.GetCurrentEpoch(ctx), func(p *types.MemberProfile) { p.ForumHelpfulCount++ })
}

func (k Keeper) IncrementChallengesWon(ctx context.Context, memberStr string) error {
	return k.profileCounter(ctx, memberStr, k.GetCurrentEpoch(ctx), func(p *types.MemberProfile) { p.ChallengesWon++ })
}

func (k Keeper) IncrementJuryDutyCount(ctx context.Context, memberStr string) error {
	return k.profileCounter(ctx, memberStr, k.GetCurrentEpoch(ctx), func(p *types.MemberProfile) { p.JuryDutiesCompleted++ })
}

func (k Keeper) IncrementInvitationsSuccessful(ctx context.Context, memberStr string) error {
	return k.profileCounter(ctx, memberStr, k.GetCurrentEpoch(ctx), func(p *types.MemberProfile) { p.InvitationsSuccessful++ })
}

// requirementMet evaluates a requirement against the member's profile and,
// where the counter lives in x/rep, the rep record. Returns (met, known):
// unknown or seasonal-only requirement types (TOP_XP, GENESIS) are never
// granted here.
func (k Keeper) requirementMet(ctx context.Context, profile types.MemberProfile, reqType types.RequirementType, threshold uint64) (bool, bool) {
	switch reqType {
	case types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST:
		return profile.VotesCast >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_FORUM_HELPFUL:
		return profile.ForumHelpfulCount >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_CHALLENGES_WON:
		return profile.ChallengesWon >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_JURY_DUTY:
		return profile.JuryDutiesCompleted >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL:
		return profile.InvitationsSuccessful >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL:
		return profile.SeasonLevel >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_ACHIEVEMENT_COUNT:
		return uint64(len(profile.Achievements)) >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED:
		if k.repKeeper == nil {
			return false, true
		}
		count, err := k.repKeeper.GetCompletedInitiativesCount(ctx, profile.Address)
		return err == nil && count >= threshold, true
	case types.RequirementType_REQUIREMENT_TYPE_REPUTATION_EARNED:
		if k.repKeeper == nil {
			return false, true
		}
		scores, err := k.repKeeper.GetReputationScores(ctx, profile.Address)
		if err != nil {
			return false, true
		}
		total := math.LegacyZeroDec()
		for _, s := range scores {
			if d, err := math.LegacyNewDecFromStr(s); err == nil {
				total = total.Add(d)
			}
		}
		return total.GTE(math.LegacyNewDec(int64(threshold))), true
	case types.RequirementType_REQUIREMENT_TYPE_SEASONS_ACTIVE:
		if k.repKeeper == nil {
			return false, true
		}
		addr, err := k.addressCodec.StringToBytes(profile.Address)
		if err != nil {
			return false, true
		}
		member, err := k.repKeeper.GetMember(ctx, addr)
		if err != nil {
			return false, true
		}
		season, err := k.Season.Get(ctx)
		if err != nil || season.Number < uint64(member.JoinedSeason) {
			return false, true
		}
		return season.Number-uint64(member.JoinedSeason)+1 >= threshold, true
	default:
		// TOP_XP is decided once per season in the snapshot phase; GENESIS
		// is only ever seeded.
		return false, false
	}
}

// CheckAndGrantAchievements grants every achievement the member now
// qualifies for and has not unlocked, then unlocks any non-seasonal title
// whose requirement is met. Evaluates every requirement type; prefer
// CheckAchievementsFor after a single counter change.
func (k Keeper) CheckAndGrantAchievements(ctx context.Context, member sdk.AccAddress) error {
	return k.CheckAchievementsFor(ctx, member, nil)
}

// CheckAchievementsFor is CheckAndGrantAchievements restricted to the
// requirement types in `changed` (nil means all).
func (k Keeper) CheckAchievementsFor(ctx context.Context, member sdk.AccAddress, changed []types.RequirementType) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	return k.checkAchievements(ctx, env, member, changed)
}

// checkAchievements is the shared implementation. Hooks run inside user
// transactions, so after bumping one counter only the definitions that
// counter can satisfy are read; granting an achievement then re-checks the
// ACHIEVEMENT_COUNT and MIN_LEVEL definitions, which a grant can move.
func (k Keeper) checkAchievements(ctx context.Context, env xpEnv, member sdk.AccAddress, changed []types.RequirementType) error {
	memberStr, err := k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	profile := k.getOrCreateProfile(ctx, memberStr, env.epoch)
	modified, err := k.checkAchievementsOnProfile(ctx, env, &profile, changed)
	if err != nil {
		return err
	}
	if !modified {
		return nil
	}
	return k.MemberProfile.Set(ctx, memberStr, profile)
}

// checkAchievementsOnProfile is checkAchievements against a caller-held
// profile. It mutates `profile` (achievements, XP, level, titles) and
// reports whether anything changed; the caller persists it, so a hook that
// bumped a counter on the same profile pays for one write in total.
func (k Keeper) checkAchievementsOnProfile(ctx context.Context, env xpEnv, profile *types.MemberProfile, changed []types.RequirementType) (bool, error) {
	modified := false
	relevant := func(t types.RequirementType) bool {
		if changed == nil {
			return true
		}
		for _, c := range changed {
			if c == t {
				return true
			}
		}
		return false
	}
	for {
		unlocked := make(map[string]bool, len(profile.Achievements))
		for _, id := range profile.Achievements {
			unlocked[id] = true
		}
		var candidates []types.Achievement
		if changed == nil {
			if err := k.Achievement.Walk(ctx, nil, func(_ string, a types.Achievement) (bool, error) {
				candidates = append(candidates, a)
				return false, nil
			}); err != nil {
				return modified, err
			}
		} else {
			var err error
			if candidates, err = k.achievementsWithRequirement(ctx, changed); err != nil {
				return modified, err
			}
		}
		var newlyMet []types.Achievement
		for _, a := range candidates {
			if unlocked[a.AchievementId] || !relevant(a.RequirementType) {
				continue
			}
			if met, known := k.requirementMet(ctx, *profile, a.RequirementType, a.RequirementThreshold); known && met {
				newlyMet = append(newlyMet, a)
			}
		}
		if len(newlyMet) == 0 {
			break
		}
		sort.Slice(newlyMet, func(i, j int) bool { return newlyMet[i].AchievementId < newlyMet[j].AchievementId })
		for _, a := range newlyMet {
			if err := k.grantAchievementOnProfile(ctx, env, profile, a); err != nil {
				return modified, err
			}
			modified = true
		}
		// A grant pays XP (level) and adds to the achievement count: those
		// two requirement types are the only ones it can newly satisfy.
		if changed != nil {
			changed = []types.RequirementType{
				types.RequirementType_REQUIREMENT_TYPE_ACHIEVEMENT_COUNT,
				types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
			}
		}
	}
	titled, err := k.unlockTitlesOnProfile(ctx, env, profile, changed, relevant)
	return modified || titled, err
}

// grantAchievementOnProfile appends the achievement to the caller-held
// profile and pays its XP reward onto it.
func (k Keeper) grantAchievementOnProfile(ctx context.Context, env xpEnv, profile *types.MemberProfile, a types.Achievement) error {
	profile.Achievements = append(profile.Achievements, a.AchievementId)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("achievement_unlocked",
		sdk.NewAttribute("member", profile.Address),
		sdk.NewAttribute("achievement_id", a.AchievementId),
		sdk.NewAttribute("xp_reward", fmt.Sprintf("%d", a.XpReward)),
	))
	if a.XpReward == 0 {
		return nil
	}
	if err := k.grantXPToProfile(ctx, env, profile, a.XpReward, types.XPSourceAchievement, a.AchievementId); err != nil && !errorsmod.IsOf(err, types.ErrXPCapReached) {
		return err
	}
	return nil
}

// unlockTitlesOnProfile unlocks non-seasonal titles whose requirement the
// caller-held profile meets, restricted to the requirement types `relevant`
// accepts, and reports whether the profile changed. Seasonal titles are
// granted only by the season transition.
func (k Keeper) unlockTitlesOnProfile(ctx context.Context, env xpEnv, profile *types.MemberProfile, changed []types.RequirementType, relevant func(types.RequirementType) bool) (bool, error) {
	var candidates []types.Title
	if changed == nil {
		if err := k.Title.Walk(ctx, nil, func(_ string, t types.Title) (bool, error) {
			candidates = append(candidates, t)
			return false, nil
		}); err != nil {
			return false, err
		}
	} else {
		var err error
		if candidates, err = k.titlesWithRequirement(ctx, changed); err != nil {
			return false, err
		}
	}
	var toUnlock []string
	for _, t := range candidates {
		if t.Seasonal || !relevant(t.RequirementType) || hasTitle(*profile, t.TitleId) {
			continue
		}
		if met, known := k.requirementMet(ctx, *profile, t.RequirementType, t.RequirementThreshold); known && met {
			toUnlock = append(toUnlock, t.TitleId)
		}
	}
	if len(toUnlock) == 0 {
		return false, nil
	}
	sort.Strings(toUnlock)
	for _, id := range toUnlock {
		unlockTitleOnProfile(ctx, env, profile, id)
	}
	return true, nil
}

// hasTitle reports whether the profile holds the title, displayed or archived.
func hasTitle(profile types.MemberProfile, titleID string) bool {
	for _, t := range profile.UnlockedTitles {
		if t == titleID {
			return true
		}
	}
	for _, t := range profile.ArchivedTitles {
		if t == titleID {
			return true
		}
	}
	return false
}

// UnlockTitle adds a title to the member's displayable list, archiving the
// oldest entries past max_displayable_titles and pruning the archive past
// max_archived_titles. Idempotent.
func (k Keeper) UnlockTitle(ctx context.Context, memberStr string, titleID string) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	return k.unlockTitle(ctx, env, memberStr, titleID)
}

func (k Keeper) unlockTitle(ctx context.Context, env xpEnv, memberStr string, titleID string) error {
	profile := k.getOrCreateProfile(ctx, memberStr, env.epoch)
	if hasTitle(profile, titleID) {
		return nil
	}
	unlockTitleOnProfile(ctx, env, &profile, titleID)
	return k.MemberProfile.Set(ctx, memberStr, profile)
}

// unlockTitleOnProfile appends a title to the caller-held profile, archiving
// past the display cap and pruning past the archive cap. The caller
// persists the profile.
func unlockTitleOnProfile(ctx context.Context, env xpEnv, profile *types.MemberProfile, titleID string) {
	if hasTitle(*profile, titleID) {
		return
	}
	memberStr := profile.Address
	profile.UnlockedTitles = append(profile.UnlockedTitles, titleID)
	for env.params.MaxDisplayableTitles > 0 && uint32(len(profile.UnlockedTitles)) > env.params.MaxDisplayableTitles {
		oldest := profile.UnlockedTitles[0]
		profile.UnlockedTitles = profile.UnlockedTitles[1:]
		profile.ArchivedTitles = append(profile.ArchivedTitles, oldest)
		if profile.DisplayTitle == oldest {
			profile.DisplayTitle = ""
		}
	}
	for env.params.MaxArchivedTitles > 0 && uint32(len(profile.ArchivedTitles)) > env.params.MaxArchivedTitles {
		profile.ArchivedTitles = profile.ArchivedTitles[1:]
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("title_unlocked",
		sdk.NewAttribute("member", memberStr),
		sdk.NewAttribute("title_id", titleID),
	))
}
