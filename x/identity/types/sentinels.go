package types

// Sentinel denom literals used by per-module DefaultGenesis() implementations
// and rewritten in-place at chain start by the genesisinit hook (§7.3 of
// docs/x-identity-spec.md). The `%` character is not in the Cosmos SDK denom
// regex `[a-zA-Z][a-zA-Z0-9/:._-]{2,127}`, so the sentinels cannot collide
// with any legitimate denom value.
const (
	BondDenomSentinel  = "%BOND_DENOM%"
	DreamDenomSentinel = "%DREAM_DENOM%"
)
