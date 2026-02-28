package simulation

import (
	"math/rand"
	"testing"

	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/types"
)

// ─── randomReactionType ──────────────────────────────────────────────────────

func TestRandomReactionType_ReturnsValidType(t *testing.T) {
	validTypes := map[types.ReactionType]bool{
		types.ReactionType_REACTION_TYPE_LIKE:       true,
		types.ReactionType_REACTION_TYPE_INSIGHTFUL: true,
		types.ReactionType_REACTION_TYPE_DISAGREE:   true,
		types.ReactionType_REACTION_TYPE_FUNNY:      true,
	}

	r := rand.New(rand.NewSource(42))
	for i := 0; i < 200; i++ {
		rt := randomReactionType(r)
		if !validTypes[rt] {
			t.Errorf("iteration %d: randomReactionType returned invalid type %v", i, rt)
		}
		if rt == types.ReactionType_REACTION_TYPE_UNSPECIFIED {
			t.Errorf("iteration %d: randomReactionType returned UNSPECIFIED", i)
		}
	}
}

func TestRandomReactionType_ProducesAllFourTypes(t *testing.T) {
	seen := make(map[types.ReactionType]bool)
	r := rand.New(rand.NewSource(99))

	// With 400 draws and only 4 choices the probability of missing one is negligible.
	for i := 0; i < 400; i++ {
		seen[randomReactionType(r)] = true
	}

	expected := []types.ReactionType{
		types.ReactionType_REACTION_TYPE_LIKE,
		types.ReactionType_REACTION_TYPE_INSIGHTFUL,
		types.ReactionType_REACTION_TYPE_DISAGREE,
		types.ReactionType_REACTION_TYPE_FUNNY,
	}
	for _, rt := range expected {
		if !seen[rt] {
			t.Errorf("randomReactionType never produced %v after 400 draws", rt)
		}
	}
}

func TestRandomReactionType_IsDeterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(7))
	r2 := rand.New(rand.NewSource(7))

	for i := 0; i < 50; i++ {
		if randomReactionType(r1) != randomReactionType(r2) {
			t.Errorf("iteration %d: same seed produced different results", i)
		}
	}
}

// ─── randomBody ──────────────────────────────────────────────────────────────

func TestRandomBody_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 100; i++ {
		body := randomBody(r)
		if body == "" {
			t.Errorf("iteration %d: randomBody returned empty string", i)
		}
	}
}

func TestRandomBody_IsDeterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(55))
	r2 := rand.New(rand.NewSource(55))

	for i := 0; i < 50; i++ {
		if randomBody(r1) != randomBody(r2) {
			t.Errorf("iteration %d: same seed produced different results", i)
		}
	}
}

func TestRandomBody_ProducesExpectedValues(t *testing.T) {
	knownBodies := []string{
		"Simulation blog body content.",
		"Testing the blog module with random content.",
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		"This was created during simulation testing.",
		"Sample content for blog simulation.",
	}
	knownSet := make(map[string]bool, len(knownBodies))
	for _, b := range knownBodies {
		knownSet[b] = true
	}

	r := rand.New(rand.NewSource(123))
	for i := 0; i < 200; i++ {
		body := randomBody(r)
		if !knownSet[body] {
			t.Errorf("iteration %d: randomBody returned unexpected value %q", i, body)
		}
	}
}

// ─── randomTitle ─────────────────────────────────────────────────────────────

func TestRandomTitle_NonEmpty(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 100; i++ {
		title := randomTitle(r)
		if title == "" {
			t.Errorf("iteration %d: randomTitle returned empty string", i)
		}
	}
}

func TestRandomTitle_IsDeterministic(t *testing.T) {
	r1 := rand.New(rand.NewSource(77))
	r2 := rand.New(rand.NewSource(77))

	for i := 0; i < 50; i++ {
		if randomTitle(r1) != randomTitle(r2) {
			t.Errorf("iteration %d: same seed produced different results", i)
		}
	}
}

func TestRandomTitle_ProducesExpectedValues(t *testing.T) {
	knownTitles := []string{
		"Simulation Post",
		"Test Blog Entry",
		"Random Thoughts",
		"Sample Article",
		"Quick Update",
	}
	knownSet := make(map[string]bool, len(knownTitles))
	for _, tl := range knownTitles {
		knownSet[tl] = true
	}

	r := rand.New(rand.NewSource(456))
	for i := 0; i < 200; i++ {
		title := randomTitle(r)
		if !knownSet[title] {
			t.Errorf("iteration %d: randomTitle returned unexpected value %q", i, title)
		}
	}
}

// ─── getAccountForAddress ─────────────────────────────────────────────────────

func TestGetAccountForAddress_Found(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	accs := simtypes.RandomAccounts(r, 5)

	for _, target := range accs {
		addr := target.Address.String()
		got, found := getAccountForAddress(addr, accs)
		if !found {
			t.Errorf("expected to find account for address %s, got not-found", addr)
		}
		if got.Address.String() != addr {
			t.Errorf("returned account address %s does not match target %s", got.Address.String(), addr)
		}
	}
}

func TestGetAccountForAddress_NotFound(t *testing.T) {
	r := rand.New(rand.NewSource(20))
	accs := simtypes.RandomAccounts(r, 5)

	// Generate a separate account that is definitely not in accs.
	other := simtypes.RandomAccounts(rand.New(rand.NewSource(999)), 1)[0]
	addr := other.Address.String()

	_, found := getAccountForAddress(addr, accs)
	if found {
		t.Errorf("expected not-found for address %s, but found one", addr)
	}
}

func TestGetAccountForAddress_EmptySlice(t *testing.T) {
	_, found := getAccountForAddress("cosmos1anything", []simtypes.Account{})
	if found {
		t.Error("expected not-found for empty accounts slice, but found one")
	}
}

func TestGetAccountForAddress_ReturnedAccountMatchesTarget(t *testing.T) {
	r := rand.New(rand.NewSource(30))
	accs := simtypes.RandomAccounts(r, 3)

	// Pick the middle account to ensure we're not just returning the first.
	target := accs[1]
	got, found := getAccountForAddress(target.Address.String(), accs)
	if !found {
		t.Fatal("expected to find account, got not-found")
	}
	if !got.Equals(target) {
		t.Errorf("returned account does not equal target account")
	}
}

// ─── incrementReactionCount ───────────────────────────────────────────────────

func TestIncrementReactionCount(t *testing.T) {
	tests := []struct {
		name         string
		reactionType types.ReactionType
		initial      types.ReactionCounts
		checkField   func(types.ReactionCounts) uint64
	}{
		{
			name:         "like increments LikeCount",
			reactionType: types.ReactionType_REACTION_TYPE_LIKE,
			initial:      types.ReactionCounts{LikeCount: 0},
			checkField:   func(c types.ReactionCounts) uint64 { return c.LikeCount },
		},
		{
			name:         "insightful increments InsightfulCount",
			reactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
			initial:      types.ReactionCounts{InsightfulCount: 0},
			checkField:   func(c types.ReactionCounts) uint64 { return c.InsightfulCount },
		},
		{
			name:         "disagree increments DisagreeCount",
			reactionType: types.ReactionType_REACTION_TYPE_DISAGREE,
			initial:      types.ReactionCounts{DisagreeCount: 0},
			checkField:   func(c types.ReactionCounts) uint64 { return c.DisagreeCount },
		},
		{
			name:         "funny increments FunnyCount",
			reactionType: types.ReactionType_REACTION_TYPE_FUNNY,
			initial:      types.ReactionCounts{FunnyCount: 0},
			checkField:   func(c types.ReactionCounts) uint64 { return c.FunnyCount },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts := tc.initial
			incrementReactionCount(&counts, tc.reactionType)
			if got := tc.checkField(counts); got != 1 {
				t.Errorf("expected count 1, got %d", got)
			}
		})
	}
}

func TestIncrementReactionCount_OnlyTargetFieldChanges(t *testing.T) {
	tests := []struct {
		name         string
		reactionType types.ReactionType
	}{
		{"like", types.ReactionType_REACTION_TYPE_LIKE},
		{"insightful", types.ReactionType_REACTION_TYPE_INSIGHTFUL},
		{"disagree", types.ReactionType_REACTION_TYPE_DISAGREE},
		{"funny", types.ReactionType_REACTION_TYPE_FUNNY},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := types.ReactionCounts{
				LikeCount:       5,
				InsightfulCount: 5,
				DisagreeCount:   5,
				FunnyCount:      5,
			}
			after := before
			incrementReactionCount(&after, tc.reactionType)

			// Only the matching field should change; all others stay at 5.
			switch tc.reactionType {
			case types.ReactionType_REACTION_TYPE_LIKE:
				if after.LikeCount != 6 {
					t.Errorf("LikeCount: want 6, got %d", after.LikeCount)
				}
				if after.InsightfulCount != 5 || after.DisagreeCount != 5 || after.FunnyCount != 5 {
					t.Error("incrementing LIKE changed other fields")
				}
			case types.ReactionType_REACTION_TYPE_INSIGHTFUL:
				if after.InsightfulCount != 6 {
					t.Errorf("InsightfulCount: want 6, got %d", after.InsightfulCount)
				}
				if after.LikeCount != 5 || after.DisagreeCount != 5 || after.FunnyCount != 5 {
					t.Error("incrementing INSIGHTFUL changed other fields")
				}
			case types.ReactionType_REACTION_TYPE_DISAGREE:
				if after.DisagreeCount != 6 {
					t.Errorf("DisagreeCount: want 6, got %d", after.DisagreeCount)
				}
				if after.LikeCount != 5 || after.InsightfulCount != 5 || after.FunnyCount != 5 {
					t.Error("incrementing DISAGREE changed other fields")
				}
			case types.ReactionType_REACTION_TYPE_FUNNY:
				if after.FunnyCount != 6 {
					t.Errorf("FunnyCount: want 6, got %d", after.FunnyCount)
				}
				if after.LikeCount != 5 || after.InsightfulCount != 5 || after.DisagreeCount != 5 {
					t.Error("incrementing FUNNY changed other fields")
				}
			}
		})
	}
}

func TestIncrementReactionCount_UnspecifiedIsNoop(t *testing.T) {
	counts := types.ReactionCounts{
		LikeCount:       3,
		InsightfulCount: 3,
		DisagreeCount:   3,
		FunnyCount:      3,
	}
	incrementReactionCount(&counts, types.ReactionType_REACTION_TYPE_UNSPECIFIED)
	if counts.LikeCount != 3 || counts.InsightfulCount != 3 ||
		counts.DisagreeCount != 3 || counts.FunnyCount != 3 {
		t.Error("incrementing UNSPECIFIED should not modify any field")
	}
}

func TestIncrementReactionCount_MultipleIncrements(t *testing.T) {
	counts := types.ReactionCounts{}
	for i := 0; i < 10; i++ {
		incrementReactionCount(&counts, types.ReactionType_REACTION_TYPE_LIKE)
	}
	if counts.LikeCount != 10 {
		t.Errorf("expected LikeCount 10 after 10 increments, got %d", counts.LikeCount)
	}
}

// ─── decrementReactionCount ───────────────────────────────────────────────────

func TestDecrementReactionCount_Basic(t *testing.T) {
	tests := []struct {
		name         string
		reactionType types.ReactionType
		initial      types.ReactionCounts
		checkField   func(types.ReactionCounts) uint64
	}{
		{
			name:         "like decrements LikeCount",
			reactionType: types.ReactionType_REACTION_TYPE_LIKE,
			initial:      types.ReactionCounts{LikeCount: 3},
			checkField:   func(c types.ReactionCounts) uint64 { return c.LikeCount },
		},
		{
			name:         "insightful decrements InsightfulCount",
			reactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
			initial:      types.ReactionCounts{InsightfulCount: 7},
			checkField:   func(c types.ReactionCounts) uint64 { return c.InsightfulCount },
		},
		{
			name:         "disagree decrements DisagreeCount",
			reactionType: types.ReactionType_REACTION_TYPE_DISAGREE,
			initial:      types.ReactionCounts{DisagreeCount: 2},
			checkField:   func(c types.ReactionCounts) uint64 { return c.DisagreeCount },
		},
		{
			name:         "funny decrements FunnyCount",
			reactionType: types.ReactionType_REACTION_TYPE_FUNNY,
			initial:      types.ReactionCounts{FunnyCount: 5},
			checkField:   func(c types.ReactionCounts) uint64 { return c.FunnyCount },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts := tc.initial
			before := tc.checkField(counts)
			decrementReactionCount(&counts, tc.reactionType)
			if got := tc.checkField(counts); got != before-1 {
				t.Errorf("expected count %d, got %d", before-1, got)
			}
		})
	}
}

func TestDecrementReactionCount_DoesNotGoBelowZero(t *testing.T) {
	tests := []struct {
		name         string
		reactionType types.ReactionType
		checkField   func(types.ReactionCounts) uint64
	}{
		{
			name:         "like stays at 0",
			reactionType: types.ReactionType_REACTION_TYPE_LIKE,
			checkField:   func(c types.ReactionCounts) uint64 { return c.LikeCount },
		},
		{
			name:         "insightful stays at 0",
			reactionType: types.ReactionType_REACTION_TYPE_INSIGHTFUL,
			checkField:   func(c types.ReactionCounts) uint64 { return c.InsightfulCount },
		},
		{
			name:         "disagree stays at 0",
			reactionType: types.ReactionType_REACTION_TYPE_DISAGREE,
			checkField:   func(c types.ReactionCounts) uint64 { return c.DisagreeCount },
		},
		{
			name:         "funny stays at 0",
			reactionType: types.ReactionType_REACTION_TYPE_FUNNY,
			checkField:   func(c types.ReactionCounts) uint64 { return c.FunnyCount },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts := types.ReactionCounts{} // all fields start at 0
			decrementReactionCount(&counts, tc.reactionType)
			if got := tc.checkField(counts); got != 0 {
				t.Errorf("expected count to stay at 0, got %d", got)
			}
		})
	}
}

func TestDecrementReactionCount_OnlyTargetFieldChanges(t *testing.T) {
	tests := []struct {
		name         string
		reactionType types.ReactionType
	}{
		{"like", types.ReactionType_REACTION_TYPE_LIKE},
		{"insightful", types.ReactionType_REACTION_TYPE_INSIGHTFUL},
		{"disagree", types.ReactionType_REACTION_TYPE_DISAGREE},
		{"funny", types.ReactionType_REACTION_TYPE_FUNNY},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := types.ReactionCounts{
				LikeCount:       5,
				InsightfulCount: 5,
				DisagreeCount:   5,
				FunnyCount:      5,
			}
			after := before
			decrementReactionCount(&after, tc.reactionType)

			switch tc.reactionType {
			case types.ReactionType_REACTION_TYPE_LIKE:
				if after.LikeCount != 4 {
					t.Errorf("LikeCount: want 4, got %d", after.LikeCount)
				}
				if after.InsightfulCount != 5 || after.DisagreeCount != 5 || after.FunnyCount != 5 {
					t.Error("decrementing LIKE changed other fields")
				}
			case types.ReactionType_REACTION_TYPE_INSIGHTFUL:
				if after.InsightfulCount != 4 {
					t.Errorf("InsightfulCount: want 4, got %d", after.InsightfulCount)
				}
				if after.LikeCount != 5 || after.DisagreeCount != 5 || after.FunnyCount != 5 {
					t.Error("decrementing INSIGHTFUL changed other fields")
				}
			case types.ReactionType_REACTION_TYPE_DISAGREE:
				if after.DisagreeCount != 4 {
					t.Errorf("DisagreeCount: want 4, got %d", after.DisagreeCount)
				}
				if after.LikeCount != 5 || after.InsightfulCount != 5 || after.FunnyCount != 5 {
					t.Error("decrementing DISAGREE changed other fields")
				}
			case types.ReactionType_REACTION_TYPE_FUNNY:
				if after.FunnyCount != 4 {
					t.Errorf("FunnyCount: want 4, got %d", after.FunnyCount)
				}
				if after.LikeCount != 5 || after.InsightfulCount != 5 || after.DisagreeCount != 5 {
					t.Error("decrementing FUNNY changed other fields")
				}
			}
		})
	}
}

func TestDecrementReactionCount_UnspecifiedIsNoop(t *testing.T) {
	counts := types.ReactionCounts{
		LikeCount:       3,
		InsightfulCount: 3,
		DisagreeCount:   3,
		FunnyCount:      3,
	}
	decrementReactionCount(&counts, types.ReactionType_REACTION_TYPE_UNSPECIFIED)
	if counts.LikeCount != 3 || counts.InsightfulCount != 3 ||
		counts.DisagreeCount != 3 || counts.FunnyCount != 3 {
		t.Error("decrementing UNSPECIFIED should not modify any field")
	}
}

func TestDecrementReactionCount_MultipleDecrements(t *testing.T) {
	counts := types.ReactionCounts{FunnyCount: 5}
	for i := 0; i < 5; i++ {
		decrementReactionCount(&counts, types.ReactionType_REACTION_TYPE_FUNNY)
	}
	if counts.FunnyCount != 0 {
		t.Errorf("expected FunnyCount 0 after decrementing to zero, got %d", counts.FunnyCount)
	}
	// One more decrement should not underflow.
	decrementReactionCount(&counts, types.ReactionType_REACTION_TYPE_FUNNY)
	if counts.FunnyCount != 0 {
		t.Errorf("expected FunnyCount to stay at 0, got %d", counts.FunnyCount)
	}
}

// ─── increment / decrement round-trip ────────────────────────────────────────

func TestIncrementThenDecrementReturnsOriginal(t *testing.T) {
	reactionTypes := []types.ReactionType{
		types.ReactionType_REACTION_TYPE_LIKE,
		types.ReactionType_REACTION_TYPE_INSIGHTFUL,
		types.ReactionType_REACTION_TYPE_DISAGREE,
		types.ReactionType_REACTION_TYPE_FUNNY,
	}

	original := types.ReactionCounts{
		LikeCount:       2,
		InsightfulCount: 4,
		DisagreeCount:   1,
		FunnyCount:      8,
	}

	for _, rt := range reactionTypes {
		counts := original
		incrementReactionCount(&counts, rt)
		decrementReactionCount(&counts, rt)
		if counts != original {
			t.Errorf("after increment+decrement for %v: got %+v, want %+v", rt, counts, original)
		}
	}
}
