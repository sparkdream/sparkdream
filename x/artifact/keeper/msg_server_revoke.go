package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) Revoke(ctx context.Context, msg *types.MsgRevoke) (*types.MsgRevokeResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.TokenIds), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	if err := types.ValidateText("reason", msg.Reason, types.RevokeReasonMaxLength, false); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if !c.Flags.IssuerMayBurn() {
		return nil, types.ErrNotRevocable
	}
	refs := make([]U64Pair, len(msg.TokenIds))
	for i, id := range msg.TokenIds {
		refs[i] = collections.Join(c.Id, id)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	for _, id := range msg.TokenIds {
		t, err := k.getToken(ctx, c.Id, id)
		if err != nil {
			return nil, err
		}
		// The deposit goes to the holder, never the revoker.
		if err := k.destroyToken(ctx, &c, t, t.Owner); err != nil {
			return nil, err
		}
		emit(ctx, types.EventRevoked, classAttr(c.Id), tokenAttr(id),
			sdk.NewAttribute("holder", t.Owner), sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("refund", t.Deposit.String()))
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	return &types.MsgRevokeResponse{}, nil
}
