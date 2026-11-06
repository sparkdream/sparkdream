package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// Nest places a token the signer owns, with anything nested in it, inside
// another token the signer owns (docs/x-artifact-spec.md §5.9.1). The child
// is NESTED-locked: it cannot move, be listed, rented or burned on its own
// until detached, and it moves with its parent.
func (k msgServer) Nest(ctx context.Context, msg *types.MsgNest) (*types.MsgNestResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if msg.Child == msg.Parent {
		return nil, errorsmod.Wrap(types.ErrNestCycle, "a token cannot hold itself")
	}
	child, err := k.getToken(ctx, msg.Child.ClassId, msg.Child.TokenId)
	if err != nil {
		return nil, err
	}
	parent, err := k.getToken(ctx, msg.Parent.ClassId, msg.Parent.TokenId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Owner, child.Owner) || !k.canActAs(ctx, msg.Owner, parent.Owner) {
		return nil, types.ErrNotTokenOwner
	}
	if child.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "child is %s", child.Lock)
	}
	childClass, err := k.getClass(ctx, child.ClassId)
	if err != nil {
		return nil, err
	}
	// A child that could not be sold on its own (hidden, expired, a spent
	// non-transferable ticket), or a tree holding one, cannot be nested:
	// it would move and sell with the receiving root.
	if err := sellable(childClass, child, now(ctx)); err != nil {
		return nil, errorsmod.Wrap(types.ErrNestNotAllowed, err.Error())
	}
	if err := k.treeSellable(ctx, child); err != nil {
		return nil, errorsmod.Wrap(types.ErrNestNotAllowed, err.Error())
	}
	anc, err := k.ancestors(ctx, parent)
	if err != nil {
		return nil, err
	}
	chain := append([]types.Token{parent}, anc...) // parent first, root last
	root := chain[len(chain)-1]
	if root.Lock != types.TokenLock_TOKEN_LOCK_NONE && root.Lock != types.TokenLock_TOKEN_LOCK_RENTED {
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "the receiving tree is %s", root.Lock)
	}
	if childClass.IsHarberger() {
		return nil, errorsmod.Wrap(types.ErrNestNotAllowed, "harberger tokens cannot be nested")
	}
	if root.Lock == types.TokenLock_TOKEN_LOCK_RENTED {
		// The renter pinned the tree they rented; a child that burns
		// before the rental ends would vanish from under them (ART-207).
		r, err := k.Rentals.Get(ctx, collections.Join(root.ClassId, root.Id))
		if err != nil {
			return nil, err
		}
		exp, err := k.treeExpiry(ctx, child)
		if err != nil {
			return nil, err
		}
		if exp != 0 && exp < r.ExpiresAt {
			return nil, errorsmod.Wrap(types.ErrNestNotAllowed, "the child would expire before the tree's rental ends")
		}
	}
	for _, a := range chain {
		if a.ClassId == child.ClassId && a.Id == child.Id {
			return nil, errorsmod.Wrap(types.ErrNestCycle, "the parent sits inside the child")
		}
		ac, err := k.getClass(ctx, a.ClassId)
		if err != nil {
			return nil, err
		}
		if ac.IsHarberger() {
			return nil, errorsmod.Wrap(types.ErrNestNotAllowed, "harberger tokens cannot hold tokens")
		}
		// A soulbound child (or a tree holding one: only non-transferable
		// tokens may hold soulbound ones) must never become movable.
		if !childClass.Flags.Transferable && ac.Flags.Transferable {
			return nil, errorsmod.Wrap(types.ErrNestNotAllowed, "a soulbound token can only sit inside non-transferable tokens")
		}
	}
	height, err := k.subtreeHeight(ctx, child)
	if err != nil {
		return nil, err
	}
	if commontypes.Saturate[uint32](len(chain))+height > p.MaxNestDepth {
		return nil, errorsmod.Wrapf(types.ErrNestTooDeep, "tree would be %d levels deep (max %d)", commontypes.Saturate[uint32](len(chain))+height, p.MaxNestDepth)
	}
	if root.Descendants+child.Descendants+1 > p.MaxNestedTokens {
		return nil, errorsmod.Wrapf(types.ErrNestTooDeep, "tree would hold %d tokens (max %d)", root.Descendants+child.Descendants+1, p.MaxNestedTokens)
	}
	if err := k.clearRentalOffer(ctx, child.ClassId, child.Id, "nested"); err != nil {
		return nil, err
	}
	// The signer acts for both owners, so they are one migrated member
	// under two addresses. A tree has a single stored owner: the child
	// (and its subtree) takes the receiving tree's (ART-102).
	if child.Owner != parent.Owner {
		if err := k.reownSubtree(ctx, &child, parent.Owner); err != nil {
			return nil, err
		}
	}
	if err := k.nest(ctx, &child, parent); err != nil {
		return nil, err
	}
	emit(ctx, types.EventNested, classAttr(child.ClassId), tokenAttr(child.Id),
		sdk.NewAttribute("parent", refString(msg.Parent)), sdk.NewAttribute("owner", msg.Owner))
	return &types.MsgNestResponse{}, nil
}
