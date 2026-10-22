package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Rentals (docs/x-artifact-spec.md §7.22). An owner offers a token's user
// right; the renter accepts and pays; the token is RENTED-locked until the
// rental ends. The owner keeps ownership throughout but cannot move, list,
// burn or end the rental early unless the offer said so.

// clearRentalOffer drops a token's open rental offer, if any.
func (k Keeper) clearRentalOffer(ctx context.Context, classID, tokenID uint64, reason string) error {
	key := collections.Join(classID, tokenID)
	o, err := k.RentalOffers.Get(ctx, key)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if err := k.RentalOffers.Remove(ctx, key); err != nil {
		return err
	}
	if err := k.RentalOfferExpiry.Remove(ctx, collections.Join3(o.ExpiresAt, classID, tokenID)); err != nil {
		return err
	}
	emit(ctx, types.EventRentalOfferCancelled, classAttr(classID), tokenAttr(tokenID),
		sdk.NewAttribute("owner", o.Owner), sdk.NewAttribute("reason", reason))
	return nil
}

// endRental removes an active rental and unlocks its token.
func (k Keeper) endRental(ctx context.Context, r types.Rental, reason string) error {
	key := collections.Join(r.ClassId, r.TokenId)
	if err := k.Rentals.Remove(ctx, key); err != nil {
		return err
	}
	if err := k.RentalsByUser.Remove(ctx, collections.Join3(r.User, r.ClassId, r.TokenId)); err != nil {
		return err
	}
	if err := k.RentalExpiry.Remove(ctx, collections.Join3(r.ExpiresAt, r.ClassId, r.TokenId)); err != nil {
		return err
	}
	if t, err := k.getToken(ctx, r.ClassId, r.TokenId); err == nil && t.Lock == types.TokenLock_TOKEN_LOCK_RENTED {
		t.Lock = types.TokenLock_TOKEN_LOCK_NONE
		if err := k.saveToken(ctx, t); err != nil {
			return err
		}
	}
	emit(ctx, types.EventRentalEnded, classAttr(r.ClassId), tokenAttr(r.TokenId),
		sdk.NewAttribute("owner", r.Owner), sdk.NewAttribute("user", r.User), sdk.NewAttribute("reason", reason))
	return nil
}

// endRentalOf ends the rental of a token if it has one.
func (k Keeper) endRentalOf(ctx context.Context, classID, tokenID uint64, reason string) error {
	r, err := k.Rentals.Get(ctx, collections.Join(classID, tokenID))
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	return k.endRental(ctx, r, reason)
}

// rentable checks the class-level rules for renting a token out.
func rentable(c types.Class) error {
	if !c.Flags.Transferable {
		return types.ErrNotRentable
	}
	if c.IsHarberger() {
		return types.ErrNotRentable
	}
	return nil
}
