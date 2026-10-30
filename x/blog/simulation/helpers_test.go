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
		"Phoenix aurora zenith: notes from the simulation.",
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

// ─── selection and gate helpers ──────────────────────────────────────────────

func TestPick(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	if _, ok := pick(r, []int{}); ok {
		t.Fatal("pick on an empty slice must report false")
	}
	items := []int{3, 5, 7}
	for i := 0; i < 50; i++ {
		v, ok := pick(r, items)
		if !ok || (v != 3 && v != 5 && v != 7) {
			t.Fatalf("pick returned %d, %v", v, ok)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    uint64
		want string
	}{
		{"zenith", 0, "zenith"},
		{"zenith", 3, "zen"},
		{"zenith", 6, "zenith"},
		{"zenith", 10, "zenith"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestStatusPredicates(t *testing.T) {
	if !activePost(types.Post{Status: types.PostStatus_POST_STATUS_ACTIVE}) {
		t.Error("an ACTIVE post is active")
	}
	if activePost(types.Post{Status: types.PostStatus_POST_STATUS_HIDDEN}) {
		t.Error("a HIDDEN post is not active")
	}
	if !activeReply(types.Reply{Status: types.ReplyStatus_REPLY_STATUS_ACTIVE}) {
		t.Error("an ACTIVE reply is active")
	}
	if activeReply(types.Reply{Status: types.ReplyStatus_REPLY_STATUS_DELETED}) {
		t.Error("a DELETED reply is not active")
	}
}
