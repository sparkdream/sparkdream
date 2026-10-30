package types

import (
	"fmt"
	"strings"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// NativeDenomMetadata returns the canonical bank metadata x/identity writes
// at InitGenesis for the bond and DREAM denoms.
func (id ChainIdentity) NativeDenomMetadata() (spark, dream banktypes.Metadata) {
	spark = banktypes.Metadata{
		Description: fmt.Sprintf("%s, native gas and staking token of the %s federated Spark Dream chain",
			id.BondDisplayName, id.ChainHumanName),
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: id.BondDenom, Exponent: 0, Aliases: []string{"micro" + strings.ToLower(id.BondDisplaySymbol)}},
			{Denom: strings.ToLower(id.BondDisplaySymbol), Exponent: id.BondDisplayDecimals},
		},
		Base:    id.BondDenom,
		Display: strings.ToLower(id.BondDisplaySymbol),
		Name:    id.BondDisplayName,
		Symbol:  id.BondDisplaySymbol,
	}
	dream = banktypes.Metadata{
		Description: fmt.Sprintf("%s, internal reputation token of the %s federated Spark Dream chain (non-transferable, non-IBC)",
			id.DreamDisplayName, id.ChainHumanName),
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: id.DreamDenom, Exponent: 0, Aliases: []string{"micro" + strings.ToLower(id.DreamDisplaySymbol)}},
			{Denom: strings.ToLower(id.DreamDisplaySymbol), Exponent: id.DreamDisplayDecimals},
		},
		Base:    id.DreamDenom,
		Display: strings.ToLower(id.DreamDisplaySymbol),
		Name:    id.DreamDisplayName,
		Symbol:  id.DreamDisplaySymbol,
	}
	return spark, dream
}

// CanonicalMetadataFor returns the canonical metadata of base when base is
// one of the two native denoms.
func (id ChainIdentity) CanonicalMetadataFor(base string) (banktypes.Metadata, bool) {
	spark, dream := id.NativeDenomMetadata()
	switch base {
	case id.BondDenom:
		return spark, true
	case id.DreamDenom:
		return dream, true
	}
	return banktypes.Metadata{}, false
}

// CheckSealedMetadata compares meta against the canonical metadata want on
// every field the sealed identity fixes: Base, Display, Symbol, Name and the
// denom units (count, order, denom and exponent, so the decimals a wallet
// renders). Description, URI, URIHash and unit aliases are free. Returns a
// human-readable reason, empty when meta is canonical.
func CheckSealedMetadata(meta, want banktypes.Metadata) string {
	if meta.Base != want.Base {
		return fmt.Sprintf("base %q, want %q", meta.Base, want.Base)
	}
	if meta.Symbol != want.Symbol {
		return fmt.Sprintf("symbol %q, want %q", meta.Symbol, want.Symbol)
	}
	if meta.Display != want.Display {
		return fmt.Sprintf("display %q, want %q", meta.Display, want.Display)
	}
	if meta.Name != want.Name {
		return fmt.Sprintf("name %q, want %q", meta.Name, want.Name)
	}
	if len(meta.DenomUnits) != len(want.DenomUnits) {
		return fmt.Sprintf("%d denom units, want %d", len(meta.DenomUnits), len(want.DenomUnits))
	}
	for i, u := range meta.DenomUnits {
		w := want.DenomUnits[i]
		if u == nil || u.Denom != w.Denom || u.Exponent != w.Exponent {
			return fmt.Sprintf("denom unit %d differs, want %s^%d", i, w.Denom, w.Exponent)
		}
	}
	return ""
}
