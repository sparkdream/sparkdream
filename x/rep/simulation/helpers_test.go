package simulation

import (
	"math/rand"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/types"
)

// newDeterministicRand creates a deterministic *rand.Rand from a fixed seed.
func newDeterministicRand(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

// newSimAccount creates a simtypes.Account with a freshly-generated secp256k1 key.
func newSimAccount() simtypes.Account {
	privKey := secp256k1.GenPrivKey()
	pubKey := privKey.PubKey()
	addr := sdk.AccAddress(pubKey.Address())
	return simtypes.Account{
		PrivKey: privKey,
		PubKey:  pubKey,
		Address: addr,
	}
}

// ---------------------------------------------------------------------------
// randomTag
// ---------------------------------------------------------------------------

func TestRandomTag_ReturnsValidTag(t *testing.T) {
	validTags := map[string]bool{
		"backend":       true,
		"frontend":      true,
		"design":        true,
		"devops":        true,
		"documentation": true,
		"testing":       true,
	}

	r := newDeterministicRand(42)
	const iterations = 200
	for i := 0; i < iterations; i++ {
		tag := randomTag(r)
		if !validTags[tag] {
			t.Errorf("iteration %d: randomTag returned unexpected value %q", i, tag)
		}
	}
}

func TestRandomTag_AllValuesReachable(t *testing.T) {
	// With a large number of iterations every tag should appear at least once.
	seen := make(map[string]int)
	r := newDeterministicRand(99)
	for i := 0; i < 10_000; i++ {
		seen[randomTag(r)]++
	}

	expected := []string{"backend", "frontend", "design", "devops", "documentation", "testing"}
	for _, tag := range expected {
		if seen[tag] == 0 {
			t.Errorf("tag %q was never produced", tag)
		}
	}
}

// ---------------------------------------------------------------------------
// randomTags
// ---------------------------------------------------------------------------

func TestRandomTags_LengthBetweenOneAndThree(t *testing.T) {
	r := newDeterministicRand(7)
	const iterations = 500
	for i := 0; i < iterations; i++ {
		tags := randomTags(r)
		if len(tags) < 1 || len(tags) > 3 {
			t.Errorf("iteration %d: expected 1-3 tags, got %d", i, len(tags))
		}
	}
}

func TestRandomTags_AllLengthsReachable(t *testing.T) {
	seen := make(map[int]bool)
	r := newDeterministicRand(13)
	for i := 0; i < 10_000; i++ {
		tags := randomTags(r)
		seen[len(tags)] = true
	}
	for _, n := range []int{1, 2, 3} {
		if !seen[n] {
			t.Errorf("length %d was never produced by randomTags", n)
		}
	}
}

func TestRandomTags_EachElementIsValidTag(t *testing.T) {
	validTags := map[string]bool{
		"backend":       true,
		"frontend":      true,
		"design":        true,
		"devops":        true,
		"documentation": true,
		"testing":       true,
	}

	r := newDeterministicRand(21)
	for i := 0; i < 300; i++ {
		for _, tag := range randomTags(r) {
			if !validTags[tag] {
				t.Errorf("randomTags returned invalid tag %q", tag)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// randomProjectCategory
// ---------------------------------------------------------------------------

func TestRandomProjectCategory_ReturnsValidCategory(t *testing.T) {
	valid := map[types.ProjectCategory]bool{
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE: true,
		types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM:      true,
		types.ProjectCategory_PROJECT_CATEGORY_RESEARCH:       true,
		types.ProjectCategory_PROJECT_CATEGORY_CREATIVE:       true,
	}

	r := newDeterministicRand(55)
	for i := 0; i < 200; i++ {
		cat := randomProjectCategory(r)
		if !valid[cat] {
			t.Errorf("iteration %d: unexpected project category %v", i, cat)
		}
	}
}

func TestRandomProjectCategory_AllValuesReachable(t *testing.T) {
	seen := make(map[types.ProjectCategory]int)
	r := newDeterministicRand(101)
	for i := 0; i < 10_000; i++ {
		seen[randomProjectCategory(r)]++
	}

	for _, cat := range []types.ProjectCategory{
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		types.ProjectCategory_PROJECT_CATEGORY_ECOSYSTEM,
		types.ProjectCategory_PROJECT_CATEGORY_RESEARCH,
		types.ProjectCategory_PROJECT_CATEGORY_CREATIVE,
	} {
		if seen[cat] == 0 {
			t.Errorf("project category %v was never produced", cat)
		}
	}
}

// ---------------------------------------------------------------------------
// randomInitiativeTier
// ---------------------------------------------------------------------------

func TestRandomInitiativeTier_ReturnsValidTier(t *testing.T) {
	valid := map[types.InitiativeTier]bool{
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE: true,
		types.InitiativeTier_INITIATIVE_TIER_STANDARD:   true,
		types.InitiativeTier_INITIATIVE_TIER_EXPERT:     true,
		types.InitiativeTier_INITIATIVE_TIER_EPIC:       true,
	}

	r := newDeterministicRand(33)
	for i := 0; i < 200; i++ {
		tier := randomInitiativeTier(r)
		if !valid[tier] {
			t.Errorf("iteration %d: unexpected initiative tier %v", i, tier)
		}
	}
}

func TestRandomInitiativeTier_AllValuesReachable(t *testing.T) {
	seen := make(map[types.InitiativeTier]int)
	r := newDeterministicRand(202)
	for i := 0; i < 10_000; i++ {
		seen[randomInitiativeTier(r)]++
	}

	for _, tier := range []types.InitiativeTier{
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeTier_INITIATIVE_TIER_EXPERT,
		types.InitiativeTier_INITIATIVE_TIER_EPIC,
	} {
		if seen[tier] == 0 {
			t.Errorf("initiative tier %v was never produced", tier)
		}
	}
}

// ---------------------------------------------------------------------------
// randomInitiativeCategory
// ---------------------------------------------------------------------------

func TestRandomInitiativeCategory_ReturnsValidCategory(t *testing.T) {
	valid := map[types.InitiativeCategory]bool{
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE:       true,
		types.InitiativeCategory_INITIATIVE_CATEGORY_BUGFIX:        true,
		types.InitiativeCategory_INITIATIVE_CATEGORY_REFACTOR:      true,
		types.InitiativeCategory_INITIATIVE_CATEGORY_DOCUMENTATION: true,
		types.InitiativeCategory_INITIATIVE_CATEGORY_TESTING:       true,
	}

	r := newDeterministicRand(77)
	for i := 0; i < 200; i++ {
		cat := randomInitiativeCategory(r)
		if !valid[cat] {
			t.Errorf("iteration %d: unexpected initiative category %v", i, cat)
		}
	}
}

func TestRandomInitiativeCategory_AllValuesReachable(t *testing.T) {
	seen := make(map[types.InitiativeCategory]int)
	r := newDeterministicRand(303)
	for i := 0; i < 10_000; i++ {
		seen[randomInitiativeCategory(r)]++
	}

	for _, cat := range []types.InitiativeCategory{
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		types.InitiativeCategory_INITIATIVE_CATEGORY_BUGFIX,
		types.InitiativeCategory_INITIATIVE_CATEGORY_REFACTOR,
		types.InitiativeCategory_INITIATIVE_CATEGORY_DOCUMENTATION,
		types.InitiativeCategory_INITIATIVE_CATEGORY_TESTING,
	} {
		if seen[cat] == 0 {
			t.Errorf("initiative category %v was never produced", cat)
		}
	}
}

// ---------------------------------------------------------------------------
// randomInterimType
// ---------------------------------------------------------------------------

func TestRandomInterimType_ReturnsValidType(t *testing.T) {
	valid := map[types.InterimType]bool{
		types.InterimType_INTERIM_TYPE_JURY_DUTY:        true,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY: true,
		types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL: true,
		types.InterimType_INTERIM_TYPE_MENTORSHIP:       true,
		types.InterimType_INTERIM_TYPE_AUDIT:            true,
	}

	r := newDeterministicRand(44)
	for i := 0; i < 200; i++ {
		it := randomInterimType(r)
		if !valid[it] {
			t.Errorf("iteration %d: unexpected interim type %v", i, it)
		}
	}
}

func TestRandomInterimType_AllValuesReachable(t *testing.T) {
	seen := make(map[types.InterimType]int)
	r := newDeterministicRand(404)
	for i := 0; i < 10_000; i++ {
		seen[randomInterimType(r)]++
	}

	for _, it := range []types.InterimType{
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
		types.InterimType_INTERIM_TYPE_MENTORSHIP,
		types.InterimType_INTERIM_TYPE_AUDIT,
	} {
		if seen[it] == 0 {
			t.Errorf("interim type %v was never produced", it)
		}
	}
}

// ---------------------------------------------------------------------------
// randomCouncil
// ---------------------------------------------------------------------------

func TestRandomCouncil_ReturnsValidCouncil(t *testing.T) {
	valid := map[string]bool{
		"technical": true,
		"ecosystem": true,
		"commons":   true,
	}

	r := newDeterministicRand(11)
	for i := 0; i < 200; i++ {
		c := randomCouncil(r)
		if !valid[c] {
			t.Errorf("iteration %d: unexpected council %q", i, c)
		}
	}
}

func TestRandomCouncil_AllValuesReachable(t *testing.T) {
	seen := make(map[string]int)
	r := newDeterministicRand(505)
	for i := 0; i < 10_000; i++ {
		seen[randomCouncil(r)]++
	}

	for _, c := range []string{"technical", "ecosystem", "commons"} {
		if seen[c] == 0 {
			t.Errorf("council %q was never produced", c)
		}
	}
}

// ---------------------------------------------------------------------------
// randomCommittee
// ---------------------------------------------------------------------------

func TestRandomCommittee_ReturnsValidCommittee(t *testing.T) {
	valid := map[string]bool{
		"operations": true,
		"hr":         true,
		"finance":    true,
	}

	r := newDeterministicRand(22)
	for i := 0; i < 200; i++ {
		c := randomCommittee(r)
		if !valid[c] {
			t.Errorf("iteration %d: unexpected committee %q", i, c)
		}
	}
}

func TestRandomCommittee_AllValuesReachable(t *testing.T) {
	seen := make(map[string]int)
	r := newDeterministicRand(606)
	for i := 0; i < 10_000; i++ {
		seen[randomCommittee(r)]++
	}

	for _, c := range []string{"operations", "hr", "finance"} {
		if seen[c] == 0 {
			t.Errorf("committee %q was never produced", c)
		}
	}
}

// ---------------------------------------------------------------------------
// randomName
// ---------------------------------------------------------------------------

func TestRandomName_IncludesPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
	}{
		{"project prefix", "Project"},
		{"initiative prefix", "Initiative"},
		{"empty prefix", ""},
		{"multi-word prefix", "My Test"},
	}

	r := newDeterministicRand(88)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := randomName(r, tc.prefix)
			// The implementation appends "-<prefix>" so the result must end
			// with "-<prefix>" (including when prefix is empty: just "-").
			expected := "-" + tc.prefix
			if len(got) < len(expected) {
				t.Fatalf("name %q is shorter than suffix %q", got, expected)
			}
			suffix := got[len(got)-len(expected):]
			if suffix != expected {
				t.Errorf("randomName(%q) = %q, want suffix %q", tc.prefix, got, expected)
			}
		})
	}
}

func TestRandomName_HasRandomPrefix(t *testing.T) {
	// The 8-character random string before the "-prefix" must be non-empty.
	r := newDeterministicRand(99)
	const prefix = "X"
	for i := 0; i < 50; i++ {
		got := randomName(r, prefix)
		// Minimum length: 8 random chars + "-" + len(prefix)
		wantMin := 8 + 1 + len(prefix)
		if len(got) < wantMin {
			t.Errorf("iteration %d: name %q shorter than expected minimum %d", i, got, wantMin)
		}
	}
}

func TestRandomName_DifferentCallsDifferentResults(t *testing.T) {
	r := newDeterministicRand(17)
	first := randomName(r, "Tag")
	second := randomName(r, "Tag")
	// Two sequential calls consume different random bytes, so results must differ.
	if first == second {
		t.Errorf("expected different names on sequential calls, got %q twice", first)
	}
}

// ---------------------------------------------------------------------------
// calculateBudgetByTier
// ---------------------------------------------------------------------------

func TestCalculateBudgetByTier_Ranges(t *testing.T) {
	tests := []struct {
		tier types.InitiativeTier
		min  int64
		max  int64
	}{
		{types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, 100, 499},
		{types.InitiativeTier_INITIATIVE_TIER_STANDARD, 600, 1499},
		{types.InitiativeTier_INITIATIVE_TIER_EXPERT, 1500, 3499},
		{types.InitiativeTier_INITIATIVE_TIER_EPIC, 3500, 7499},
	}

	for _, tc := range tests {
		t.Run(tc.tier.String(), func(t *testing.T) {
			r := newDeterministicRand(12345)
			for i := 0; i < 1000; i++ {
				budget := calculateBudgetByTier(r, tc.tier)
				v := budget.Int64()
				if v < tc.min || v > tc.max {
					t.Errorf("tier %v: budget %d out of expected range [%d, %d]",
						tc.tier, v, tc.min, tc.max)
				}
			}
		})
	}
}

func TestCalculateBudgetByTier_DefaultCase(t *testing.T) {
	r := newDeterministicRand(0)
	// Use an explicitly unrecognised tier value to exercise the default branch.
	budget := calculateBudgetByTier(r, types.InitiativeTier(999))
	if !budget.Equal(math.NewInt(1000)) {
		t.Errorf("default tier: expected budget 1000, got %s", budget)
	}
}

func TestCalculateBudgetByTier_IsPositive(t *testing.T) {
	tiers := []types.InitiativeTier{
		types.InitiativeTier_INITIATIVE_TIER_APPRENTICE,
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeTier_INITIATIVE_TIER_EXPERT,
		types.InitiativeTier_INITIATIVE_TIER_EPIC,
	}
	r := newDeterministicRand(9)
	for _, tier := range tiers {
		for i := 0; i < 200; i++ {
			budget := calculateBudgetByTier(r, tier)
			if !budget.IsPositive() {
				t.Errorf("tier %v: non-positive budget %s", tier, budget)
			}
		}
	}
}

func TestCalculateBudgetByTier_HigherTierHigherMinimum(t *testing.T) {
	// Run many samples per tier and check that the minimum observed value for
	// a higher tier exceeds the minimum possible value of the lower tier.
	tiers := []struct {
		tier types.InitiativeTier
		min  int64
	}{
		{types.InitiativeTier_INITIATIVE_TIER_APPRENTICE, 100},
		{types.InitiativeTier_INITIATIVE_TIER_STANDARD, 600},
		{types.InitiativeTier_INITIATIVE_TIER_EXPERT, 1500},
		{types.InitiativeTier_INITIATIVE_TIER_EPIC, 3500},
	}

	r := newDeterministicRand(54321)
	for i, tc := range tiers {
		_ = i
		for j := 0; j < 500; j++ {
			budget := calculateBudgetByTier(r, tc.tier)
			if budget.Int64() < tc.min {
				t.Errorf("tier %v sample below minimum %d: got %d", tc.tier, tc.min, budget.Int64())
			}
		}
	}
}

// ---------------------------------------------------------------------------
// PtrInt
// ---------------------------------------------------------------------------

func TestPtrInt_ReturnsPointerToValue(t *testing.T) {
	values := []int64{0, 1, -1, 1_000_000, 9_999_999_999}
	for _, v := range values {
		original := math.NewInt(v)
		ptr := PtrInt(original)
		if ptr == nil {
			t.Errorf("PtrInt(%d) returned nil", v)
			continue
		}
		if !ptr.Equal(original) {
			t.Errorf("PtrInt(%d): *ptr = %s, want %d", v, ptr, v)
		}
	}
}

func TestPtrInt_IsCopy(t *testing.T) {
	// PtrInt takes the argument by value and returns a pointer to the local
	// copy, so mutating the original (by reassigning) must not affect *ptr.
	original := math.NewInt(42)
	ptr := PtrInt(original)

	// Reassign the local variable; the pointer must still hold 42.
	original = math.NewInt(100)

	if !ptr.Equal(math.NewInt(42)) {
		t.Errorf("PtrInt result changed after reassigning original: got %s", ptr)
	}
}

func TestPtrInt_ZeroValue(t *testing.T) {
	zero := math.ZeroInt()
	ptr := PtrInt(zero)
	if ptr == nil {
		t.Fatal("PtrInt(ZeroInt()) returned nil")
	}
	if !ptr.IsZero() {
		t.Errorf("expected zero, got %s", ptr)
	}
}

func TestPtrInt_DistinctCallsDistinctPointers(t *testing.T) {
	val := math.NewInt(7)
	p1 := PtrInt(val)
	p2 := PtrInt(val)
	if p1 == p2 {
		t.Error("two PtrInt calls with equal values returned the same pointer address")
	}
}

// ---------------------------------------------------------------------------
// getAccountFromMember
// ---------------------------------------------------------------------------

func TestGetAccountFromMember_MatchingAccount(t *testing.T) {
	acc := newSimAccount()
	member := &types.Member{Address: acc.Address.String()}

	found, ok := getAccountFromMember(member, []simtypes.Account{acc})
	if !ok {
		t.Fatal("expected ok=true for matching account")
	}
	if !found.Address.Equals(acc.Address) {
		t.Errorf("returned account address %s does not match %s", found.Address, acc.Address)
	}
}

func TestGetAccountFromMember_NoMatch(t *testing.T) {
	memberAcc := newSimAccount()
	otherAcc := newSimAccount()
	member := &types.Member{Address: memberAcc.Address.String()}

	// Pass only the other account — no match should be found.
	_, ok := getAccountFromMember(member, []simtypes.Account{otherAcc})
	if ok {
		t.Error("expected ok=false when member address not in account list")
	}
}

func TestGetAccountFromMember_EmptyAccountList(t *testing.T) {
	acc := newSimAccount()
	member := &types.Member{Address: acc.Address.String()}

	_, ok := getAccountFromMember(member, []simtypes.Account{})
	if ok {
		t.Error("expected ok=false for empty account list")
	}
}

func TestGetAccountFromMember_MultipleAccounts_PicksCorrect(t *testing.T) {
	acc1 := newSimAccount()
	acc2 := newSimAccount()
	acc3 := newSimAccount()

	// Target is acc2.
	member := &types.Member{Address: acc2.Address.String()}

	found, ok := getAccountFromMember(member, []simtypes.Account{acc1, acc2, acc3})
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !found.Address.Equals(acc2.Address) {
		t.Errorf("expected acc2 (%s), got %s", acc2.Address, found.Address)
	}
}

func TestGetAccountFromMember_ReturnsEmptyAccountOnMiss(t *testing.T) {
	acc := newSimAccount()
	member := &types.Member{Address: acc.Address.String()}

	// Provide a list that does not contain the member's account.
	other := newSimAccount()
	result, ok := getAccountFromMember(member, []simtypes.Account{other})
	if ok {
		t.Error("expected ok=false")
	}
	// Returned account should be zero-value (nil address).
	if result.Address != nil {
		t.Errorf("expected nil address in zero-value account, got %s", result.Address)
	}
}

// ---------------------------------------------------------------------------
// Determinism — same seed yields same sequence
// ---------------------------------------------------------------------------

func TestRandomFunctions_AreDeterministic(t *testing.T) {
	const seed = int64(2024)

	r1 := newDeterministicRand(seed)
	r2 := newDeterministicRand(seed)

	for i := 0; i < 100; i++ {
		if randomTag(r1) != randomTag(r2) {
			t.Errorf("randomTag not deterministic at iteration %d", i)
		}
	}

	r1 = newDeterministicRand(seed)
	r2 = newDeterministicRand(seed)
	for i := 0; i < 100; i++ {
		t1 := randomTags(r1)
		t2 := randomTags(r2)
		if len(t1) != len(t2) {
			t.Errorf("randomTags length mismatch at iteration %d: %d vs %d", i, len(t1), len(t2))
		}
		for j := range t1 {
			if t1[j] != t2[j] {
				t.Errorf("randomTags element mismatch at iteration %d, index %d", i, j)
			}
		}
	}

	r1 = newDeterministicRand(seed)
	r2 = newDeterministicRand(seed)
	for i := 0; i < 100; i++ {
		if randomProjectCategory(r1) != randomProjectCategory(r2) {
			t.Errorf("randomProjectCategory not deterministic at iteration %d", i)
		}
		if randomInitiativeTier(r1) != randomInitiativeTier(r2) {
			t.Errorf("randomInitiativeTier not deterministic at iteration %d", i)
		}
		if randomInitiativeCategory(r1) != randomInitiativeCategory(r2) {
			t.Errorf("randomInitiativeCategory not deterministic at iteration %d", i)
		}
		if randomInterimType(r1) != randomInterimType(r2) {
			t.Errorf("randomInterimType not deterministic at iteration %d", i)
		}
		if randomCouncil(r1) != randomCouncil(r2) {
			t.Errorf("randomCouncil not deterministic at iteration %d", i)
		}
		if randomCommittee(r1) != randomCommittee(r2) {
			t.Errorf("randomCommittee not deterministic at iteration %d", i)
		}
	}

	r1 = newDeterministicRand(seed)
	r2 = newDeterministicRand(seed)
	for i := 0; i < 100; i++ {
		tier := types.InitiativeTier_INITIATIVE_TIER_STANDARD
		b1 := calculateBudgetByTier(r1, tier)
		b2 := calculateBudgetByTier(r2, tier)
		if !b1.Equal(b2) {
			t.Errorf("calculateBudgetByTier not deterministic at iteration %d", i)
		}
	}
}
