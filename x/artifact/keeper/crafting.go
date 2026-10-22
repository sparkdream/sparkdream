package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

// Crafting (docs/x-artifact-spec.md §7.25): a recipe on the target class
// burns a set of input tokens or units and mints one output into the
// class. Burned inputs refund their deposits to the crafter; the output
// takes a fresh deposit. Recipes can be removed at any time, but none can
// be added once the class has been crafted into, so holders of the inputs
// know what their tokens can become.

// validateCraftInputs checks a recipe's ingredients: 1..MaxCraftInputs
// distinct (class, edition) pairs of existing classes whose holders may
// burn, quantities positive, and at most max_batch_size unique tokens.
func (k Keeper) validateCraftInputs(ctx context.Context, inputs []types.CraftInput, p types.Params) error {
	if len(inputs) == 0 || len(inputs) > types.MaxCraftInputs {
		return errorsmod.Wrapf(types.ErrInvalidRecipe, "need 1 to %d inputs", types.MaxCraftInputs)
	}
	seen := map[[2]uint64]bool{}
	var tokens uint64
	for _, in := range inputs {
		key := [2]uint64{in.ClassId, in.EditionId}
		if seen[key] {
			return errorsmod.Wrapf(types.ErrInvalidRecipe, "duplicate input %d/%d", in.ClassId, in.EditionId)
		}
		seen[key] = true
		if in.Quantity == 0 {
			return errorsmod.Wrap(types.ErrInvalidRecipe, "input quantities must be positive")
		}
		ic, err := k.getClass(ctx, in.ClassId)
		if err != nil {
			return err
		}
		if !ic.Flags.HolderMayBurn() {
			return errorsmod.Wrapf(types.ErrInvalidRecipe, "holders of class %d cannot burn its tokens", in.ClassId)
		}
		if ic.IsEditions() {
			if _, err := k.getEdition(ctx, in.ClassId, in.EditionId); err != nil {
				return err
			}
			continue
		}
		if in.EditionId != 0 {
			return errorsmod.Wrapf(types.ErrInvalidRecipe, "class %d has no editions", in.ClassId)
		}
		tokens += in.Quantity
	}
	if tokens > uint64(p.MaxBatchSize) {
		return errorsmod.Wrapf(types.ErrInvalidRecipe, "at most %d input tokens", p.MaxBatchSize)
	}
	return nil
}

// validateCraftOutput checks what a recipe mints into class c: metadata for
// a UNIQUE class, an existing edition and a positive quantity for an
// EDITIONS class.
func (k Keeper) validateCraftOutput(ctx context.Context, c types.Class, meta types.TokenMetadata, editionID, quantity uint64, p types.Params) error {
	if c.IsEditions() {
		if !meta.IsEmpty() {
			return errorsmod.Wrap(types.ErrInvalidRecipe, "edition outputs take the edition's metadata")
		}
		if quantity == 0 {
			return errorsmod.Wrap(types.ErrInvalidRecipe, "output_quantity must be positive")
		}
		_, err := k.getEdition(ctx, c.Id, editionID)
		return err
	}
	if editionID != 0 || quantity > 1 {
		return errorsmod.Wrap(types.ErrInvalidRecipe, "a unique class crafts one token")
	}
	if c.Reveal.Reveals() {
		return errorsmod.Wrap(types.ErrInvalidRecipe, "commit-reveal classes cannot be crafted into")
	}
	return types.ValidateTokenMetadata(meta, p)
}
