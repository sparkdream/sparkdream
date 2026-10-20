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
	// Either party may burn a mutual attestation, so both burn paths must be
	// open; that in turn makes it soulbound.
	if f.Mutual && f.BurnAuthorization != BurnAuthorization_BURN_AUTHORIZATION_HOLDER_OR_ISSUER {
		return errorsmod.Wrap(ErrInvalidFlags, "mutual classes need burn_authorization HOLDER_OR_ISSUER")
	}
	if f.RedeemedTransferable && (!f.Redeemable || !f.Transferable) {
		return errorsmod.Wrap(ErrInvalidFlags, "redeemed_transferable needs redeemable and transferable")
	}
	return nil
}

// Validate checks an expiry policy against the compiled term bounds. The
// renewal price denom is checked by the keeper.
func (e ExpiryPolicy) Validate() error {
	if _, ok := ExpiryMode_name[int32(e.Mode)]; !ok {
		return errorsmod.Wrapf(ErrInvalidExpiry, "unknown expiry mode %d", e.Mode)
	}
	if e.Mode == ExpiryMode_EXPIRY_MODE_NONE {
		if e.Term != 0 || (!e.RenewalPrice.Amount.IsNil() && !e.RenewalPrice.Amount.IsZero()) {
			return errorsmod.Wrap(ErrInvalidExpiry, "term and renewal_price need an expiry mode")
		}
		return nil
	}
	if e.Term < MinTokenTerm || e.Term > MaxTokenTerm {
		return errorsmod.Wrapf(ErrInvalidExpiry, "term must be in [%d, %d] seconds", MinTokenTerm, MaxTokenTerm)
	}
	if !e.RenewalPrice.Amount.IsNil() && e.RenewalPrice.Amount.IsNegative() {
		return errorsmod.Wrap(ErrInvalidExpiry, "renewal_price must be non-negative")
	}
	return nil
}

// Expires reports whether the class's tokens carry an expiry.
func (e ExpiryPolicy) Expires() bool { return e.Mode != ExpiryMode_EXPIRY_MODE_NONE }

// ValidateRevealInput checks the reveal policy a creator submits: only
// kind, commitment and deadline may be set, the deadline must be at least
// MinRevealWindow after now, and the commitment is a SHA-256.
func ValidateRevealInput(r RevealPolicy, now int64) error {
	if _, ok := RevealKind_name[int32(r.Kind)]; !ok {
		return errorsmod.Wrapf(ErrInvalidReveal, "unknown reveal kind %d", r.Kind)
	}
	if r.RevealedAt != 0 || r.Seed != "" {
		return errorsmod.Wrap(ErrInvalidReveal, "revealed_at and seed are set by the chain")
	}
	if r.Kind == RevealKind_REVEAL_KIND_NONE {
		if r.Commitment != "" || r.Deadline != 0 {
			return errorsmod.Wrap(ErrInvalidReveal, "commitment and deadline need a reveal kind")
		}
		return nil
	}
	if _, err := DecodeHexHash("commitment", r.Commitment); err != nil {
		return err
	}
	if r.Deadline < now+MinRevealWindow {
		return errorsmod.Wrapf(ErrInvalidReveal, "deadline must be at least %d seconds after creation", MinRevealWindow)
	}
	return nil
}

// Reveals reports whether the class uses commit-reveal.
func (r RevealPolicy) Reveals() bool { return r.Kind != RevealKind_REVEAL_KIND_NONE }

// HasRenderer reports whether the class names a renderer package.
func (r Renderer) HasRenderer() bool { return r.PkgPath != "" }

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
