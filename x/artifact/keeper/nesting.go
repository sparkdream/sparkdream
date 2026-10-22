package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// Nesting (docs/x-artifact-spec.md §7.23). A nested token carries
// lock = NESTED and Token.parent; every ancestor counts it in
// Token.descendants; the Children index lists each token's direct
// children. A whole tree shares one owner and moves with its root.

// ChildKey keys the Children index: (parent, child).
type ChildKey = collections.Pair[U64Pair, U64Pair]

func refKey(r types.TokenRef) U64Pair { return collections.Join(r.ClassId, r.TokenId) }

func tokenRef(t types.Token) types.TokenRef { return types.TokenRef{ClassId: t.ClassId, TokenId: t.Id} }

// directChildren lists a token's direct children in key order.
func (k Keeper) directChildren(ctx context.Context, classID, tokenID uint64) ([]types.TokenRef, error) {
	var out []types.TokenRef
	err := k.Children.Walk(ctx, collections.NewPrefixedPairRange[U64Pair, U64Pair](collections.Join(classID, tokenID)),
		func(key ChildKey) (bool, error) {
			out = append(out, types.TokenRef{ClassId: key.K2().K1(), TokenId: key.K2().K2()})
			return false, nil
		})
	return out, err
}

// descendantsOf walks the tree under a token breadth-first (bounded by
// max_nested_tokens) and returns every descendant with its parent.
func (k Keeper) descendantsOf(ctx context.Context, t types.Token) ([]types.Token, error) {
	var out []types.Token
	if t.Descendants == 0 {
		return out, nil
	}
	queue := []types.TokenRef{tokenRef(t)}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		kids, err := k.directChildren(ctx, cur.ClassId, cur.TokenId)
		if err != nil {
			return nil, err
		}
		for _, r := range kids {
			child, err := k.getToken(ctx, r.ClassId, r.TokenId)
			if err != nil {
				return nil, err
			}
			out = append(out, child)
			queue = append(queue, r)
		}
	}
	return out, nil
}

// ContentsHash is the hash MsgBuy and MsgAcceptRental pin for a token's
// tree ("" when it holds nothing).
func (k Keeper) ContentsHash(ctx context.Context, t types.Token) (string, error) {
	desc, err := k.descendantsOf(ctx, t)
	if err != nil {
		return "", err
	}
	entries := make([]types.ContentsEntry, len(desc))
	for i, d := range desc {
		entries[i] = types.ContentsEntry{ClassID: d.ClassId, TokenID: d.Id,
			ParentClassID: d.Parent.ClassId, ParentTokenID: d.Parent.TokenId, MetadataHash: types.MetadataHash(d.Metadata)}
	}
	return types.ContentsHash(entries), nil
}

// checkContentsPin enforces the contents pin of a purchase or rental: a
// token holding anything requires an exact pin (fail closed), and a pin on
// an empty token must be empty.
func (k Keeper) checkContentsPin(ctx context.Context, t types.Token, pin string) error {
	if t.Descendants == 0 && pin == "" {
		return nil
	}
	hash, err := k.ContentsHash(ctx, t)
	if err != nil {
		return err
	}
	if pin != hash {
		return errorsmod.Wrapf(types.ErrContentsChanged, "contents hash is %q", hash)
	}
	return nil
}

// ancestors returns a token's ancestors, nearest first (bounded by
// max_nest_depth).
func (k Keeper) ancestors(ctx context.Context, t types.Token) ([]types.Token, error) {
	var out []types.Token
	cur := t
	for cur.IsNested() {
		parent, err := k.getToken(ctx, cur.Parent.ClassId, cur.Parent.TokenId)
		if err != nil {
			return nil, err
		}
		out = append(out, parent)
		cur = parent
	}
	return out, nil
}

// rootOf returns the top of the tree a token sits in (itself when not
// nested).
func (k Keeper) rootOf(ctx context.Context, t types.Token) (types.Token, error) {
	anc, err := k.ancestors(ctx, t)
	if err != nil || len(anc) == 0 {
		return t, err
	}
	return anc[len(anc)-1], nil
}

// subtreeHeight is the number of levels below a token (0 for a leaf).
func (k Keeper) subtreeHeight(ctx context.Context, t types.Token) (uint32, error) {
	if t.Descendants == 0 {
		return 0, nil
	}
	kids, err := k.directChildren(ctx, t.ClassId, t.Id)
	if err != nil {
		return 0, err
	}
	var best uint32
	for _, r := range kids {
		child, err := k.getToken(ctx, r.ClassId, r.TokenId)
		if err != nil {
			return 0, err
		}
		h, err := k.subtreeHeight(ctx, child)
		if err != nil {
			return 0, err
		}
		if h+1 > best {
			best = h + 1
		}
	}
	return best, nil
}

// adjustAncestors adds delta (negative to subtract) to the descendant count
// of every ancestor of a token.
func (k Keeper) adjustAncestors(ctx context.Context, t types.Token, delta int64) error {
	anc, err := k.ancestors(ctx, t)
	if err != nil {
		return err
	}
	for _, a := range anc {
		a.Descendants = uint32(int64(a.Descendants) + delta)
		if err := k.saveToken(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

// nest places child (with its subtree) under parent. The caller checked
// ownership, locks and the nesting rules.
func (k Keeper) nest(ctx context.Context, child *types.Token, parent types.Token) error {
	ref := tokenRef(parent)
	child.Parent = &ref
	child.Lock = types.TokenLock_TOKEN_LOCK_NESTED
	if err := k.saveToken(ctx, *child); err != nil {
		return err
	}
	if err := k.Children.Set(ctx, collections.Join(refKey(ref), refKey(tokenRef(*child)))); err != nil {
		return err
	}
	return k.adjustAncestors(ctx, *child, int64(child.Descendants)+1)
}

// detachFromParent takes a nested token (and its subtree) out of its
// parent: it becomes a root with no lock.
func (k Keeper) detachFromParent(ctx context.Context, child *types.Token) error {
	if !child.IsNested() {
		return nil
	}
	if err := k.adjustAncestors(ctx, *child, -(int64(child.Descendants) + 1)); err != nil {
		return err
	}
	if err := k.Children.Remove(ctx, collections.Join(refKey(*child.Parent), refKey(tokenRef(*child)))); err != nil {
		return err
	}
	parent := *child.Parent
	child.Parent = nil
	child.Lock = types.TokenLock_TOKEN_LOCK_NONE
	if err := k.saveToken(ctx, *child); err != nil {
		return err
	}
	emit(ctx, types.EventDetached, classAttr(child.ClassId), tokenAttr(child.Id),
		sdk.NewAttribute("parent", refString(parent)), sdk.NewAttribute("owner", child.Owner))
	return nil
}

// releaseChildren detaches every direct child of a token that is about to
// be destroyed: the children (with their own subtrees) stay with the
// holder as roots. The token itself must already be detached from its own
// parent.
func (k Keeper) releaseChildren(ctx context.Context, t types.Token) error {
	if t.Descendants == 0 {
		return nil
	}
	kids, err := k.directChildren(ctx, t.ClassId, t.Id)
	if err != nil {
		return err
	}
	for _, r := range kids {
		child, err := k.getToken(ctx, r.ClassId, r.TokenId)
		if err != nil {
			return err
		}
		if err := k.Children.Remove(ctx, collections.Join(refKey(tokenRef(t)), refKey(r))); err != nil {
			return err
		}
		child.Parent = nil
		child.Lock = types.TokenLock_TOKEN_LOCK_NONE
		if err := k.saveToken(ctx, child); err != nil {
			return err
		}
		emit(ctx, types.EventDetached, classAttr(child.ClassId), tokenAttr(child.Id),
			sdk.NewAttribute("parent", refString(tokenRef(t))), sdk.NewAttribute("owner", child.Owner))
	}
	return nil
}

// moveDescendants moves every token under root to newOwner, keeping the
// owner index in step (moveToken calls it for a root that holds anything).
func (k Keeper) moveDescendants(ctx context.Context, root types.Token, oldOwner, newOwner string) error {
	desc, err := k.descendantsOf(ctx, root)
	if err != nil {
		return err
	}
	for _, d := range desc {
		if err := k.TokensByOwner.Remove(ctx, collections.Join3(oldOwner, d.ClassId, d.Id)); err != nil {
			return err
		}
		d.Owner = newOwner
		if err := k.TokensByOwner.Set(ctx, collections.Join3(newOwner, d.ClassId, d.Id)); err != nil {
			return err
		}
		if err := k.saveToken(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// treeWithheld reports whether a nested token's ancestors (or their
// classes) are hidden: a hidden parent withholds its whole tree.
func (k Keeper) treeWithheld(ctx context.Context, t types.Token) bool {
	if !t.IsNested() {
		return false
	}
	anc, err := k.ancestors(ctx, t)
	if err != nil {
		return true
	}
	for _, a := range anc {
		if a.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return true
		}
		if c, err := k.Classes.Get(ctx, a.ClassId); err != nil || c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return true
		}
	}
	return false
}

func refString(r types.TokenRef) string { return u64(r.ClassId) + "/" + u64(r.TokenId) }
