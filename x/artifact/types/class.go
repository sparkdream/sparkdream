package types

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
)

// MaxRoyaltyRecipients bounds the royalty split, and with it the number of
// sends in one MsgBuy settlement.
const MaxRoyaltyRecipients = 10

// RoyaltyWeightTotal is what a class's royalty share weights sum to.
const RoyaltyWeightTotal uint32 = 10000

// HolderMayBurn reports whether the holder may burn a token (MsgBurn).
func (f ClassFlags) HolderMayBurn() bool {
	return f.BurnAuthorization != BurnAuthorization_BURN_AUTHORIZATION_ISSUER
}

// IssuerMayBurn reports whether the class owner may burn a token (MsgRevoke).
func (f ClassFlags) IssuerMayBurn() bool {
	return f.BurnAuthorization == BurnAuthorization_BURN_AUTHORIZATION_ISSUER ||
		f.BurnAuthorization == BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER
}

// Validate checks the flag combination. An issuer who can burn tokens must
// not also let them trade, or a buyer could pay for a token the seller's
// issuer then destroys.
func (f ClassFlags) Validate() error {
	if _, ok := BurnAuthorization_name[int32(f.BurnAuthorization)]; !ok {
		return errorsmod.Wrapf(ErrInvalidFlags, "unknown burn_authorization %d", f.BurnAuthorization)
	}
	if f.IssuerMayBurn() && f.Transferable {
		return errorsmod.Wrap(ErrInvalidFlags, "classes the issuer can burn must be non-transferable")
	}
	return nil
}

// ValidateRoyaltyShares checks the shape of a royalty split: 1 to
// MaxRoyaltyRecipients distinct, non-empty addresses with positive weights
// summing to RoyaltyWeightTotal. Address validity is checked by the keeper.
func ValidateRoyaltyShares(shares []RoyaltyShare) error {
	if len(shares) == 0 || len(shares) > MaxRoyaltyRecipients {
		return errorsmod.Wrapf(ErrInvalidRoyaltySplit, "need 1 to %d recipients, got %d", MaxRoyaltyRecipients, len(shares))
	}
	seen := make(map[string]bool, len(shares))
	var total uint32
	for _, s := range shares {
		if s.Address == "" {
			return errorsmod.Wrap(ErrInvalidRoyaltySplit, "empty recipient address")
		}
		if seen[s.Address] {
			return errorsmod.Wrapf(ErrInvalidRoyaltySplit, "duplicate recipient %s", s.Address)
		}
		seen[s.Address] = true
		if s.WeightBps == 0 || s.WeightBps > RoyaltyWeightTotal {
			return errorsmod.Wrapf(ErrInvalidRoyaltySplit, "weight %d for %s outside (0, %d]", s.WeightBps, s.Address, RoyaltyWeightTotal)
		}
		total += s.WeightBps
	}
	if total != RoyaltyWeightTotal {
		return errorsmod.Wrap(ErrInvalidRoyaltySplit, fmt.Sprintf("weights sum to %d, want %d", total, RoyaltyWeightTotal))
	}
	return nil
}

// SoleRoyaltyRecipient is the split that pays the whole royalty to addr.
func SoleRoyaltyRecipient(addr string) []RoyaltyShare {
	return []RoyaltyShare{{Address: addr, WeightBps: RoyaltyWeightTotal}}
}
