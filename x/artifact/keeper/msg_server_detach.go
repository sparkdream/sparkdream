package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

// Detach takes a nested token, with its own children, out of its parent
// (docs/x-artifact-spec.md §5.9.2). Refused while the tree's root is
// listed, pending transfer or rented, so neither a buyer nor a renter can
// lose contents they relied on.
func (k msgServer) Detach(ctx context.Context, msg *types.MsgDetach) (*types.MsgDetachResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	child, err := k.getToken(ctx, msg.Child.ClassId, msg.Child.TokenId)
	if err != nil {
		return nil, err
	}
	if !child.IsNested() {
		return nil, errorsmod.Wrap(types.ErrNestNotAllowed, "token is not nested")
	}
	if !k.canActAs(ctx, msg.Owner, child.Owner) {
		return nil, types.ErrNotTokenOwner
	}
	root, err := k.rootOf(ctx, child)
	if err != nil {
		return nil, err
	}
	if root.Lock != types.TokenLock_TOKEN_LOCK_NONE {
		return nil, errorsmod.Wrapf(types.ErrTokenLocked, "the tree is %s", root.Lock)
	}
	if err := k.detachFromParent(ctx, &child); err != nil {
		return nil, err
	}
	return &types.MsgDetachResponse{}, nil
}
