package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
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
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if !c.Flags.IssuerMayBurn() {
		return nil, types.ErrNotRevocable
	}
	// Either party may burn a mutual attestation: the class owner, or the
	// minter who signed it (checked per token below).
	isOwner := k.canActAs(ctx, msg.Owner, c.Owner)
	if !isOwner && !c.Flags.Mutual {
		return nil, types.ErrNotClassOwner
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
		if !isOwner && !k.canActAs(ctx, msg.Owner, t.Minter) {
			return nil, errorsmod.Wrap(types.ErrNotAuthorized, "only the class owner or the attesting minter may revoke a mutual attestation")
		}
		// The deposit (and any escrowed mint price) goes to the holder, never
		// the revoker.
		if err := k.destroyToken(ctx, &c, t, t.Owner, true); err != nil {
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
