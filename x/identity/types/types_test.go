package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/identity/types"
)

// cloneValid returns a fresh valid identity for table-driven mutation.
func cloneValid() types.ChainIdentity {
	return types.ChainIdentity{
		ChainHumanName:       "Phoenix",
		ChainTickerPrefix:    "PHX",
		BondDenom:            "upspk.phoenix",
		BondDisplaySymbol:    "PSPK",
		BondDisplayName:      "Phoenix Spark",
		BondDisplayDecimals:  6,
		DreamDenom:           "udream.phoenix",
		DreamDisplaySymbol:   "PDRM",
		DreamDisplayName:     "Phoenix Dream",
		DreamDisplayDecimals: 6,
		FoundedAt:            1735689600,
	}
}

func TestValidateAcceptsCanonical(t *testing.T) {
	require.NoError(t, cloneValid().Validate())
}

func TestChainIdentityValidateRejectsBadDenoms(t *testing.T) {
	bondBad := []string{
		"uspark",       // missing dot suffix
		"spark",        // missing u prefix and dot
		"SPARK",        // uppercase
		"uatom",        // missing dot suffix
		"u.phoenix",    // missing 2-5 letters between u and dot
		"upspkphoenix", // missing dot
		"upspk.PHX",    // uppercase in chain part
		"%BOND_DENOM%",
	}
	for _, d := range bondBad {
		t.Run("bond="+d, func(t *testing.T) {
			id := cloneValid()
			id.BondDenom = d
			err := id.Validate()
			require.Error(t, err)
			require.ErrorIs(t, err, types.ErrInvalidBondDenom)
		})
	}
	dreamBad := []string{
		"dream",            // missing u prefix and dot
		"DREAM.phoenix",    // uppercase
		"dream.phoenix",    // missing u prefix
		"udream",           // missing dot suffix
		"u.phoenix",        // missing 2-5 letters between u and dot
		"uwishesx.phoenix", // more than 5 letters between u and dot
		"%DREAM_DENOM%",
	}
	for _, d := range dreamBad {
		t.Run("dream="+d, func(t *testing.T) {
			id := cloneValid()
			id.DreamDenom = d
			err := id.Validate()
			require.Error(t, err)
			require.ErrorIs(t, err, types.ErrInvalidDreamDenom)
		})
	}
}

// The dream denom follows the same shape rule as the bond denom; the udream
// prefix is a convention, not a requirement, so a federated chain can brand
// its internal token (e.g., Aurora's uwish.aurora).
func TestValidateAcceptsCustomDreamPrefix(t *testing.T) {
	for _, d := range []string{"uwish.phoenix", "udrmz.phoenix", "uab.phoenix"} {
		t.Run(d, func(t *testing.T) {
			id := cloneValid()
			id.DreamDenom = d
			require.NoError(t, id.Validate())
		})
	}
}

func TestValidateRejectsWhitespaceAndNonASCII(t *testing.T) {
	id := cloneValid()
	id.ChainHumanName = " Phoenix"
	require.ErrorIs(t, id.Validate(), types.ErrInvalidChainName)

	id = cloneValid()
	id.ChainHumanName = "Phoenix "
	require.ErrorIs(t, id.Validate(), types.ErrInvalidChainName)

	id = cloneValid()
	id.ChainHumanName = "Phoenix\x00"
	require.ErrorIs(t, id.Validate(), types.ErrInvalidChainName)

	id = cloneValid()
	id.ChainHumanName = "Phœnix" // non-ASCII rune
	require.ErrorIs(t, id.Validate(), types.ErrInvalidChainName)
}

func TestValidateRejectsCollisions(t *testing.T) {
	// Bond and dream denoms share one shape rule, so an equal pair passes
	// both regexes and must be caught by the explicit collision check.
	id := cloneValid()
	id.DreamDenom = "upspk.phoenix" // same as bond
	require.ErrorIs(t, id.Validate(), types.ErrDenomCollision)

	id = cloneValid()
	id.DreamDisplaySymbol = "PSPK" // same as BondDisplaySymbol
	require.ErrorIs(t, id.Validate(), types.ErrSymbolCollision)
}

func TestValidateRejectsBadFoundedAt(t *testing.T) {
	id := cloneValid()
	id.FoundedAt = 0
	require.ErrorIs(t, id.Validate(), types.ErrInvalidFoundedAt)
	id.FoundedAt = -1
	require.ErrorIs(t, id.Validate(), types.ErrInvalidFoundedAt)
}

func TestValidateRejectsBadDecimals(t *testing.T) {
	id := cloneValid()
	id.BondDisplayDecimals = 19
	require.ErrorIs(t, id.Validate(), types.ErrInvalidDecimals)
	id = cloneValid()
	id.DreamDisplayDecimals = 100
	require.ErrorIs(t, id.Validate(), types.ErrInvalidDecimals)
	id = cloneValid()
	id.BondDisplayDecimals = 0
	require.NoError(t, id.Validate(), "0 decimals should be accepted (range is [0,18])")
}

func TestValidateRejectsBadTickerPrefix(t *testing.T) {
	cases := []string{"P", "p", "PHX1", "1PHX", "PHOENIX", "PH X"}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			id := cloneValid()
			id.ChainTickerPrefix = p
			require.ErrorIs(t, id.Validate(), types.ErrInvalidTickerPrefix)
		})
	}
}

func TestValidateRejectsBadDisplaySymbols(t *testing.T) {
	cases := []string{"P", "p", "1ABC", "PS", "PSPKABCDX", "PS PK"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			id := cloneValid()
			id.BondDisplaySymbol = s
			require.Error(t, id.Validate())
		})
	}
	// dream symbol: must match same regex
	id := cloneValid()
	id.DreamDisplaySymbol = "1ABC"
	require.ErrorIs(t, id.Validate(), types.ErrInvalidDreamSymbol)
}

func TestChainIDBase(t *testing.T) {
	// Verifies the regex extracts the expected base name for the common
	// Cosmos chain-id shapes documented in §11.1 of the spec.
	cases := []struct {
		in, want string
	}{
		{"phoenix-1", "phoenix"},
		{"aurora-mainnet-1", "aurora"},
		{"cosmoshub-4", "cosmoshub"},
		{"osmosis-test-5", "osmosis"},
		{"dydx-mainnet-1", "dydx"},
		{"noble", "noble"},
		{"akashnet", "akashnet"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			// chainIDBase is unexported; we exercise it through the public
			// ValidateAgainstChainID which calls it internally.
			id := cloneValid()
			id.ChainHumanName = c.want // exact-match against base
			err := id.ValidateAgainstChainID(c.in, false)
			require.NoError(t, err, "expected base %q to match chain_human_name %q for chain_id %q", c.want, id.ChainHumanName, c.in)
		})
	}
}

func TestValidateAgainstChainIDRejectsEmpty(t *testing.T) {
	// M4: empty chain_id used to pass silently via strings.Contains("x", "")
	// returning true. Now rejected unless the bypass is set.
	id := cloneValid()
	require.Error(t, id.ValidateAgainstChainID("", false))
	require.NoError(t, id.ValidateAgainstChainID("", true))
}

func TestValidateAgainstChainID(t *testing.T) {
	id := cloneValid() // chain_human_name = "Phoenix"
	require.NoError(t, id.ValidateAgainstChainID("phoenix-1", false))
	require.NoError(t, id.ValidateAgainstChainID("phoenix-mainnet-1", false))
	require.NoError(t, id.ValidateAgainstChainID("PHOENIX", false))

	// Typo cases
	id2 := cloneValid()
	id2.ChainHumanName = "Pheonix"
	require.ErrorIs(t, id2.ValidateAgainstChainID("phoenix-1", false), types.ErrChainNameInconsistent)

	// Wrong chain entirely
	id3 := cloneValid()
	id3.ChainHumanName = "Aurora"
	require.ErrorIs(t, id3.ValidateAgainstChainID("phoenix-1", false), types.ErrChainNameInconsistent)

	// Bypass flag
	require.NoError(t, id3.ValidateAgainstChainID("phoenix-1", true))

	// Cosmos Hub case from spec: should fail without bypass (substring test)
	id4 := cloneValid()
	id4.ChainHumanName = "Cosmos Hub"
	require.Error(t, id4.ValidateAgainstChainID("cosmoshub-4", false))
}

func TestEqualCanonical(t *testing.T) {
	a := cloneValid()
	b := cloneValid()
	require.True(t, a.EqualCanonical(b))

	// Names are canonical too; mutate one and assert difference.
	b2 := cloneValid()
	b2.BondDenom = "uaspk.aurora"
	require.False(t, a.EqualCanonical(b2))

	b3 := cloneValid()
	b3.FoundedAt = 0
	require.False(t, a.EqualCanonical(b3))

	b4 := cloneValid()
	b4.BondDisplayDecimals = 18
	require.False(t, a.EqualCanonical(b4))

	// bond_display_name and dream_display_name are NOT canonical (reserved
	// for future MsgUpdateNonCanonicalMetadata).
	b5 := cloneValid()
	b5.BondDisplayName = "Different"
	require.True(t, a.EqualCanonical(b5))
	b6 := cloneValid()
	b6.DreamDisplayName = "Different"
	require.True(t, a.EqualCanonical(b6))
}

func TestDiffCanonical(t *testing.T) {
	a := cloneValid()
	b := cloneValid()
	b.BondDenom = "uaspk.aurora"
	b.FoundedAt = 1
	diff := a.DiffCanonical(b)
	require.Contains(t, diff, "bond_denom")
	require.Contains(t, diff, "founded_at")
}

func TestSentinelsAreNotValidDenoms(t *testing.T) {
	// The sentinel literals must never validate as denoms; this is what makes
	// the JSON string-replace approach safe.
	require.True(t, strings.Contains(types.BondDenomSentinel, "%"))
	require.True(t, strings.Contains(types.DreamDenomSentinel, "%"))

	id := cloneValid()
	id.BondDenom = types.BondDenomSentinel
	require.Error(t, id.Validate())
	id = cloneValid()
	id.DreamDenom = types.DreamDenomSentinel
	require.Error(t, id.Validate())
}
