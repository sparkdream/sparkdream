package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) Transfer(ctx context.Context, msg *types.MsgTransfer) (*types.MsgTransferResponse, error) {
	sender, err := k.decodeAddr("sender", msg.Sender)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if err := checkBatch(len(msg.Entries), p); err != nil {
		return nil, err
	}
	refs := make([]U64Pair, len(msg.Entries))
	for i, e := range msg.Entries {
		refs[i] = collections.Join(e.ClassId, e.TokenId)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}

	classes := map[uint64]types.Class{}
	results := make([]types.TransferResult, 0, len(msg.Entries))
	for _, e := range msg.Entries {
		c, ok := classes[e.ClassId]
		if !ok {
			if c, err = k.getClass(ctx, e.ClassId); err != nil {
				return nil, err
			}
			classes[e.ClassId] = c
		}
		t, err := k.getToken(ctx, e.ClassId, e.TokenId)
		if err != nil {
			return nil, err
		}
		if t.Owner != msg.Sender {
			return nil, types.ErrNotTokenOwner
		}
		if !c.Flags.Transferable {
			return nil, types.ErrNotTransferable
		}
		if t.Lock != types.TokenLock_TOKEN_LOCK_NONE {
			return nil, errorsmod.Wrapf(types.ErrTokenLocked, "token %d/%d is %s", t.ClassId, t.Id, t.Lock)
		}
		if e.Recipient == msg.Sender {
			return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "cannot transfer to yourself")
		}
		if _, err := k.validateRecipient("recipient", e.Recipient); err != nil {
			return nil, err
		}

		if k.deliversDirectly(ctx, msg.Sender, e.Recipient) {
			if err := k.moveToken(ctx, &t, e.Recipient); err != nil {
				return nil, err
			}
			results = append(results, types.TransferResult{ClassId: t.ClassId, TokenId: t.Id, Delivered: true})
			emit(ctx, types.EventTransferred, classAttr(t.ClassId), tokenAttr(t.Id),
				sdk.NewAttribute("from", msg.Sender), sdk.NewAttribute("to", e.Recipient), sdk.NewAttribute("via", "transfer"))
			continue
		}

		if err := k.reserveInboxSlot(ctx, msg.Sender, e.Recipient, p); err != nil {
			return nil, err
		}
		if err := k.burnFrom(ctx, sender, p.InboxFee); err != nil {
			return nil, err
		}
		pt := types.PendingTransfer{
			ClassId:   t.ClassId,
			TokenId:   t.Id,
			From:      msg.Sender,
			To:        e.Recipient,
			CreatedAt: now(ctx),
			ExpiresAt: now(ctx) + p.PendingTtl,
		}
		if err := k.PendingTransfers.Set(ctx, collections.Join(t.ClassId, t.Id), pt); err != nil {
			return nil, err
		}
		if err := k.indexPending(ctx, types.InboxKind_INBOX_KIND_TRANSFER, t.ClassId, t.Id, msg.Sender, e.Recipient, pt.ExpiresAt); err != nil {
			return nil, err
		}
		t.Lock = types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER
		if err := k.saveToken(ctx, t); err != nil {
			return nil, err
		}
		results = append(results, types.TransferResult{ClassId: t.ClassId, TokenId: t.Id, Delivered: false})
		emit(ctx, types.EventTransferPending, classAttr(t.ClassId), tokenAttr(t.Id),
			sdk.NewAttribute("from", msg.Sender), sdk.NewAttribute("to", e.Recipient),
			sdk.NewAttribute("expires_at", i64(pt.ExpiresAt)), sdk.NewAttribute("inbox_fee", p.InboxFee.String()))
	}
	return &types.MsgTransferResponse{Results: results}, nil
}
