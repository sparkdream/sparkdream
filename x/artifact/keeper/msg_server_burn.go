package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) Burn(ctx context.Context, msg *types.MsgBurn) (*types.MsgBurnResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.Refs), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	refs := make([]U64Pair, len(msg.Refs))
	for i, r := range msg.Refs {
		refs[i] = collections.Join(r.ClassId, r.TokenId)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	for _, r := range msg.Refs {
		c, err := k.getClass(ctx, r.ClassId)
		if err != nil {
			return nil, err
		}
		t, err := k.getToken(ctx, r.ClassId, r.TokenId)
		if err != nil {
			return nil, err
		}
		if !k.canActAs(ctx, msg.Owner, t.Owner) {
			return nil, types.ErrNotTokenOwner
		}
		if !c.Flags.HolderMayBurn() {
			return nil, types.ErrHolderBurnNotAllowed
		}
		// A listing is cancelled implicitly; a pending transfer, rental or
		// nesting must end first.
		if t.Lock != types.TokenLock_TOKEN_LOCK_NONE && t.Lock != types.TokenLock_TOKEN_LOCK_LISTED {
			return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token is %s", t.Lock)
		}
		// An unrevealed token past the reveal deadline refunds what the
		// holder paid as well as the deposit (docs/x-artifact-spec.md §7.15).
		overdue := revealOverdue(c, t, now(ctx))
		if err := k.destroyToken(ctx, &c, t, msg.Owner, overdue); err != nil {
			return nil, err
		}
		if err := k.saveClass(ctx, c); err != nil {
			return nil, err
		}
		refund := t.Deposit
		if overdue && !t.Paid.IsNil() {
			refund = refund.Add(t.Paid)
		}
		emit(ctx, types.EventBurned, classAttr(t.ClassId), tokenAttr(t.Id),
			sdk.NewAttribute("owner", msg.Owner), sdk.NewAttribute("refund", refund.String()),
			sdk.NewAttribute("reveal_overdue", boolStr(overdue)))
	}
	return &types.MsgBurnResponse{}, nil
}
