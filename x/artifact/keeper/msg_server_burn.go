package keeper

import (
	"context"

	"cosmossdk.io/collections"
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
		if t.Owner != msg.Owner {
			return nil, types.ErrNotTokenOwner
		}
		if !c.Flags.HolderMayBurn() {
			return nil, types.ErrHolderBurnNotAllowed
		}
		if t.Lock == types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER {
			return nil, types.ErrTokenLocked
		}
		if err := k.destroyToken(ctx, &c, t, msg.Owner); err != nil {
			return nil, err
		}
		if err := k.saveClass(ctx, c); err != nil {
			return nil, err
		}
		emit(ctx, types.EventBurned, classAttr(t.ClassId), tokenAttr(t.Id),
			sdk.NewAttribute("owner", msg.Owner), sdk.NewAttribute("refund", t.Deposit.String()))
	}
	return &types.MsgBurnResponse{}, nil
}
