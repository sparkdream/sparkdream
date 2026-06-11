package types

import (
	"fmt"
	"regexp"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Regexes used by ChainIdentity.Validate.
var (
	tickerPrefixRE = regexp.MustCompile(`^[A-Z]{2,5}$`)
	bondDenomRE    = regexp.MustCompile(`^u[a-z]{2,5}\.[a-z][a-z0-9-]{2,15}$`)
	dreamDenomRE   = regexp.MustCompile(`^udream\.[a-z][a-z0-9-]{2,15}$`)
	displaySymRE   = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,7}$`)

	// chainIDStripRE captures common Cosmos chain-id suffix patterns. Group 1
	// is the "base" name with any "-<word>" and "-<digits>" suffixes removed.
	chainIDStripRE = regexp.MustCompile(`^(.+?)(-[a-z]+)?(-\d+)?$`)
)

// Validate enforces intrinsic field rules listed in §11 of the spec.
// Callable from anywhere; chain-id consistency (§11.1) is checked separately
// via ValidateAgainstChainID, which needs ctx.ChainID().
func (c ChainIdentity) Validate() error {
	if err := validatePrintableASCII(c.ChainHumanName, 1, 32); err != nil {
		return errorsmod.Wrap(ErrInvalidChainName, err.Error())
	}
	if !tickerPrefixRE.MatchString(c.ChainTickerPrefix) {
		return errorsmod.Wrapf(ErrInvalidTickerPrefix, "%q does not match ^[A-Z]{2,5}$", c.ChainTickerPrefix)
	}

	if err := sdk.ValidateDenom(c.BondDenom); err != nil {
		return errorsmod.Wrapf(ErrInvalidBondDenom, "%q: %s", c.BondDenom, err.Error())
	}
	if !bondDenomRE.MatchString(c.BondDenom) {
		return errorsmod.Wrapf(ErrInvalidBondDenom, "%q does not match ^u[a-z]{2,5}\\.[a-z][a-z0-9-]{2,15}$", c.BondDenom)
	}
	if !displaySymRE.MatchString(c.BondDisplaySymbol) {
		return errorsmod.Wrapf(ErrInvalidBondSymbol, "%q does not match ^[A-Z][A-Z0-9]{2,7}$", c.BondDisplaySymbol)
	}
	if err := validatePrintableASCII(c.BondDisplayName, 1, 64); err != nil {
		return errorsmod.Wrap(ErrInvalidBondDisplayName, err.Error())
	}
	if c.BondDisplayDecimals > 18 {
		return errorsmod.Wrapf(ErrInvalidDecimals, "bond_display_decimals=%d", c.BondDisplayDecimals)
	}

	if err := sdk.ValidateDenom(c.DreamDenom); err != nil {
		return errorsmod.Wrapf(ErrInvalidDreamDenom, "%q: %s", c.DreamDenom, err.Error())
	}
	if !dreamDenomRE.MatchString(c.DreamDenom) {
		return errorsmod.Wrapf(ErrInvalidDreamDenom, "%q does not match ^udream\\.[a-z][a-z0-9-]{2,15}$", c.DreamDenom)
	}
	if !displaySymRE.MatchString(c.DreamDisplaySymbol) {
		return errorsmod.Wrapf(ErrInvalidDreamSymbol, "%q does not match ^[A-Z][A-Z0-9]{2,7}$", c.DreamDisplaySymbol)
	}
	if err := validatePrintableASCII(c.DreamDisplayName, 1, 64); err != nil {
		return errorsmod.Wrap(ErrInvalidDreamDisplayName, err.Error())
	}
	if c.DreamDisplayDecimals > 18 {
		return errorsmod.Wrapf(ErrInvalidDecimals, "dream_display_decimals=%d", c.DreamDisplayDecimals)
	}

	if c.BondDenom == c.DreamDenom {
		return errorsmod.Wrapf(ErrDenomCollision, "%q", c.BondDenom)
	}
	if c.BondDisplaySymbol == c.DreamDisplaySymbol {
		return errorsmod.Wrapf(ErrSymbolCollision, "%q", c.BondDisplaySymbol)
	}
	if c.FoundedAt <= 0 {
		return errorsmod.Wrapf(ErrInvalidFoundedAt, "founded_at=%d", c.FoundedAt)
	}
	return nil
}

// ValidateAgainstChainID performs the soft consistency check between the
// consensus-level chain_id and the chosen chain_human_name (§11.1). The
// check is permissive — it catches typos and copy-paste errors but does not
// enforce strict equality (federated chains will legitimately diverge).
func (c ChainIdentity) ValidateAgainstChainID(chainID string, allowMismatch bool) error {
	if allowMismatch {
		return nil
	}
	if chainID == "" {
		return errorsmod.Wrap(ErrChainNameInconsistent, "chain_id is empty (set GenesisState.allow_chain_id_mismatch=true if intentional)")
	}
	a := strings.ToLower(chainIDBase(chainID))
	b := strings.ToLower(c.ChainHumanName)
	if a == b || strings.Contains(a, b) || strings.Contains(b, a) {
		return nil
	}
	return errorsmod.Wrapf(ErrChainNameInconsistent,
		"chain_human_name=%q must be derivable from chain_id=%q (set GenesisState.allow_chain_id_mismatch=true to override)",
		c.ChainHumanName, chainID)
}

// chainIDBase strips common Cosmos chain-id suffix patterns and returns the
// base name. Examples: "phoenix-1" -> "phoenix", "aurora-mainnet-1" ->
// "aurora", "noble" -> "noble".
func chainIDBase(s string) string {
	m := chainIDStripRE.FindStringSubmatch(s)
	if len(m) < 2 {
		return s
	}
	return m[1]
}

// EqualCanonical compares the canonical (immutable) fields of two
// ChainIdentity values. All nine fields named in §6.5 are considered
// canonical; only bond_display_name and dream_display_name are excluded
// (reserved for a future MsgUpdateNonCanonicalMetadata, §18).
func (c ChainIdentity) EqualCanonical(other ChainIdentity) bool {
	return c.BondDenom == other.BondDenom &&
		c.DreamDenom == other.DreamDenom &&
		c.BondDisplaySymbol == other.BondDisplaySymbol &&
		c.DreamDisplaySymbol == other.DreamDisplaySymbol &&
		c.BondDisplayDecimals == other.BondDisplayDecimals &&
		c.DreamDisplayDecimals == other.DreamDisplayDecimals &&
		c.ChainHumanName == other.ChainHumanName &&
		c.ChainTickerPrefix == other.ChainTickerPrefix &&
		c.FoundedAt == other.FoundedAt
}

// DiffCanonical returns a human-readable per-field diff of canonical fields
// that differ between c and other. Returns "" when EqualCanonical(other) is
// true.
func (c ChainIdentity) DiffCanonical(other ChainIdentity) string {
	parts := []string{}
	if c.BondDenom != other.BondDenom {
		parts = append(parts, fmt.Sprintf("bond_denom: %q -> %q", c.BondDenom, other.BondDenom))
	}
	if c.DreamDenom != other.DreamDenom {
		parts = append(parts, fmt.Sprintf("dream_denom: %q -> %q", c.DreamDenom, other.DreamDenom))
	}
	if c.BondDisplaySymbol != other.BondDisplaySymbol {
		parts = append(parts, fmt.Sprintf("bond_display_symbol: %q -> %q", c.BondDisplaySymbol, other.BondDisplaySymbol))
	}
	if c.DreamDisplaySymbol != other.DreamDisplaySymbol {
		parts = append(parts, fmt.Sprintf("dream_display_symbol: %q -> %q", c.DreamDisplaySymbol, other.DreamDisplaySymbol))
	}
	if c.BondDisplayDecimals != other.BondDisplayDecimals {
		parts = append(parts, fmt.Sprintf("bond_display_decimals: %d -> %d", c.BondDisplayDecimals, other.BondDisplayDecimals))
	}
	if c.DreamDisplayDecimals != other.DreamDisplayDecimals {
		parts = append(parts, fmt.Sprintf("dream_display_decimals: %d -> %d", c.DreamDisplayDecimals, other.DreamDisplayDecimals))
	}
	if c.ChainHumanName != other.ChainHumanName {
		parts = append(parts, fmt.Sprintf("chain_human_name: %q -> %q", c.ChainHumanName, other.ChainHumanName))
	}
	if c.ChainTickerPrefix != other.ChainTickerPrefix {
		parts = append(parts, fmt.Sprintf("chain_ticker_prefix: %q -> %q", c.ChainTickerPrefix, other.ChainTickerPrefix))
	}
	if c.FoundedAt != other.FoundedAt {
		parts = append(parts, fmt.Sprintf("founded_at: %d -> %d", c.FoundedAt, other.FoundedAt))
	}
	return strings.Join(parts, "; ")
}

// validatePrintableASCII verifies s has length within [minLen, maxLen],
// contains only printable ASCII bytes (0x20 to 0x7e inclusive), and has no
// leading or trailing whitespace. Byte-loop (not rune-loop) is intentional
// (§11).
func validatePrintableASCII(s string, minLen, maxLen int) error {
	if len(s) < minLen || len(s) > maxLen {
		return fmt.Errorf("length %d outside [%d, %d]", len(s), minLen, maxLen)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e {
			return fmt.Errorf("byte 0x%02x at offset %d is not printable ASCII", c, i)
		}
	}
	if s != strings.TrimSpace(s) {
		return fmt.Errorf("leading or trailing whitespace not allowed: %q", s)
	}
	return nil
}
