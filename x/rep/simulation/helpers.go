package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/types"
)

// Pure helpers: random field values for the messages the operations build.
// None of them reads or writes state.

// randomTag returns a random tag from common tags
func randomTag(r *rand.Rand) string {
	tags := []string{"backend", "frontend", "design", "devops", "documentation", "testing"}
	return tags[r.Intn(len(tags))]
}

// randomTags returns 1-3 distinct tags (handlers refuse a tag listed twice).
func randomTags(r *rand.Rand) []string {
	numTags := r.Intn(3) + 1
	seen := make(map[string]bool, numTags)
	tags := make([]string, 0, numTags)
	for i := 0; i < numTags*4 && len(tags) < numTags; i++ {
		if t := randomTag(r); !seen[t] {
			seen[t] = true
			tags = append(tags, t)
		}
	}
	return tags
}

// randomProjectCategory returns a random project category
func randomProjectCategory(r *rand.Rand) types.ProjectCategory {
	categories := []types.ProjectCategory{
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM,
		types.ProjectCategory_PROJECT_CATEGORY_RESEARCH,
		types.ProjectCategory_PROJECT_CATEGORY_CREATIVE,
	}
	return categories[r.Intn(len(categories))]
}

// randomInitiativeTier returns a random initiative tier
func randomInitiativeTier(r *rand.Rand) types.InitiativeTier {
	tiers := []types.InitiativeTier{
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeTier_INITIATIVE_TIER_EXPERT,
		types.InitiativeTier_INITIATIVE_TIER_EPIC,
	}
	return tiers[r.Intn(len(tiers))]
}

// randomInitiativeCategory returns a random initiative category
func randomInitiativeCategory(r *rand.Rand) types.InitiativeCategory {
	categories := []types.InitiativeCategory{
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_BUGFIX,
		types.InitiativeCategory_INITIATIVE_CATEGORY_REFACTOR,
		types.InitiativeCategory_INITIATIVE_CATEGORY_DOCUMENTATION,
		types.InitiativeCategory_INITIATIVE_CATEGORY_TESTING,
	}
	return categories[r.Intn(len(categories))]
}

// randomInterimType returns a random interim type
func randomInterimType(r *rand.Rand) types.InterimType {
	interimTypes := []types.InterimType{
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
		types.InterimType_INTERIM_TYPE_MENTORSHIP,
		types.InterimType_INTERIM_TYPE_AUDIT,
	}
	return interimTypes[r.Intn(len(interimTypes))]
}

// randomCouncil returns a random council name
func randomCouncil(r *rand.Rand) string {
	councils := []string{"technical", "ecosystem", "commons"}
	return councils[r.Intn(len(councils))]
}

// randomCommittee returns a random committee name
func randomCommittee(r *rand.Rand) string {
	committees := []string{"operations", "hr", "finance"}
	return committees[r.Intn(len(committees))]
}

// PtrInt returns a pointer to a math.Int
func PtrInt(i math.Int) *math.Int {
	return &i
}

// getAccountFromMember converts a member address string to a simtypes.Account
func getAccountFromMember(member *types.Member, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == member.Address {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// randomName generates a random name with prefix
func randomName(r *rand.Rand, prefix string) string {
	return simtypes.RandStringOfLength(r, 8) + "-" + prefix
}

// calculateBudgetByTier returns appropriate budget for initiative tier
func calculateBudgetByTier(r *rand.Rand, tier types.InitiativeTier) math.Int {
	switch tier {
	case types.InitiativeTier_INITIATIVE_TIER_APPRENTICE:
		return math.NewInt(int64(r.Intn(400) + 100)) // 100-500
	case types.InitiativeTier_INITIATIVE_TIER_STANDARD:
		return math.NewInt(int64(r.Intn(900) + 600)) // 600-1500
	case types.InitiativeTier_INITIATIVE_TIER_EXPERT:
		return math.NewInt(int64(r.Intn(2000) + 1500)) // 1500-3500
	case types.InitiativeTier_INITIATIVE_TIER_EPIC:
		return math.NewInt(int64(r.Intn(4000) + 3500)) // 3500-7500
	default:
		return math.NewInt(1000)
	}
}
