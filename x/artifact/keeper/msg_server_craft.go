package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Craft burns a recipe's inputs and mints its output to the crafter
// (docs/x-artifact-spec.md §5.12.2). The first craft locks the class's
// recipe list. Crafting is minting: it respects minting_closed, the class
// cap and the edition cap. The crafter asked for the output, so it is
// delivered directly.
func (k msgServer) Craft(ctx context.Context, msg *types.MsgCraft) (*types.MsgCraftResponse, error) {
	crafter, err := k.decodeAddr("crafter", msg.Crafter)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := mintable(c); err != nil {
		return nil, err
	}
	r, err := k.CraftRecipes.Get(ctx, collections.Join(c.Id, msg.RecipeId))
	if err != nil {
		return nil, types.ErrRecipeNotFound
	}

	// Count the supplied unique tokens per class against the recipe.
	want := map[uint64]uint64{}
	for _, in := range r.Inputs {
		ic, err := k.getClass(ctx, in.ClassId)
		if err != nil {
			return nil, err
		}
		if !ic.Flags.HolderMayBurn() {
			return nil, errorsmod.Wrapf(types.ErrCraftInputMismatch, "holders of class %d cannot burn its tokens", in.ClassId)
		}
		if !ic.IsEditions() {
			want[in.ClassId] += in.Quantity
		}
	}
	refs := make([]U64Pair, len(msg.InputTokens))
	got := map[uint64]uint64{}
	for i, ref := range msg.InputTokens {
		refs[i] = collections.Join(ref.ClassId, ref.TokenId)
		got[ref.ClassId]++
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	if len(got) != len(want) {
		return nil, errorsmod.Wrap(types.ErrCraftInputMismatch, "input tokens do not match the recipe")
	}
	for id, n := range want {
		if got[id] != n {
			return nil, errorsmod.Wrapf(types.ErrCraftInputMismatch, "recipe takes %d tokens of class %d, got %d", n, id, got[id])
		}
	}

	// Burn the unique inputs; their deposits go to the crafter.
	for _, ref := range msg.InputTokens {
		ic, err := k.getClass(ctx, ref.ClassId)
		if err != nil {
			return nil, err
		}
		t, err := k.getToken(ctx, ref.ClassId, ref.TokenId)
		if err != nil {
			return nil, err
		}
		if t.Owner != msg.Crafter {
			return nil, types.ErrNotTokenOwner
		}
		if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
			return nil, errorsmod.Wrapf(types.ErrTokenLocked, "input %d/%d is %s", t.ClassId, t.Id, t.Lock)
		}
		if err := k.destroyToken(ctx, &ic, t, msg.Crafter, revealOverdue(ic, t, now(ctx))); err != nil {
			return nil, err
		}
		if err := k.saveClass(ctx, ic); err != nil {
			return nil, err
		}
		emit(ctx, types.EventBurned, classAttr(t.ClassId), tokenAttr(t.Id),
			sdk.NewAttribute("owner", msg.Crafter), sdk.NewAttribute("refund", t.Deposit.String()),
			sdk.NewAttribute("reveal_overdue", "false"), sdk.NewAttribute("via", "craft"))
	}
	// Burn the edition inputs.
	for _, in := range r.Inputs {
		if in.EditionId == 0 {
			continue
		}
		e, err := k.getEdition(ctx, in.ClassId, in.EditionId)
		if err != nil {
			return nil, err
		}
		if err := k.debit(ctx, &e, msg.Crafter, in.Quantity, false); err != nil {
			return nil, err
		}
		e.Supply -= in.Quantity
		e.Burned += in.Quantity
		if err := k.saveEdition(ctx, e); err != nil {
			return nil, err
		}
		emit(ctx, types.EventUnitsBurned, classAttr(in.ClassId), editionAttr(in.EditionId),
			sdk.NewAttribute("owner", msg.Crafter), sdk.NewAttribute("amount", u64(in.Quantity)), sdk.NewAttribute("via", "craft"))
	}

	// The inputs may have changed the target class; reload it.
	if c, err = k.getClass(ctx, msg.ClassId); err != nil {
		return nil, err
	}
	resp := &types.MsgCraftResponse{}
	if c.IsEditions() {
		e, err := k.getEdition(ctx, c.Id, r.OutputEditionId)
		if err != nil {
			return nil, err
		}
		if err := checkUnitSupply(e, r.OutputQuantity); err != nil {
			return nil, err
		}
		if err := k.credit(ctx, &e, msg.Crafter, r.OutputQuantity, crafter, false, math.Int{}, ""); err != nil {
			return nil, err
		}
		e.Supply += r.OutputQuantity
		if err := k.saveEdition(ctx, e); err != nil {
			return nil, err
		}
		resp.EditionId, resp.Amount = e.Id, r.OutputQuantity
	} else {
		if err := checkSupply(c, 1); err != nil {
			return nil, err
		}
		deposit, err := k.takeDeposit(ctx, c, crafter, p)
		if err != nil {
			return nil, err
		}
		tokenID := c.NextTokenId
		c.NextTokenId++
		if _, err := k.createToken(ctx, c, tokenID, msg.Crafter, msg.Crafter, r.OutputMetadata, nil, false, deposit, math.ZeroInt()); err != nil {
			return nil, err
		}
		c.Supply++
		resp.TokenId = tokenID
	}
	c.CraftingLocked = true
	r.Crafts++
	if err := k.CraftRecipes.Set(ctx, collections.Join(c.Id, r.Id), r); err != nil {
		return nil, err
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventCrafted, classAttr(c.Id), sdk.NewAttribute("recipe_id", u64(r.Id)),
		sdk.NewAttribute("crafter", msg.Crafter), tokenAttr(resp.TokenId), editionAttr(resp.EditionId),
		sdk.NewAttribute("amount", u64(resp.Amount)))
	return resp, nil
}
