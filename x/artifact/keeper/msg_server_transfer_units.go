package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// TransferUnits moves free units to recipients, or parks them in their
// inboxes (docs/x-artifact-spec.md §5.8.2). The sender pays the record
// deposit of a recipient who holds no units yet, and gets their own back
// when the transfer empties their balance.
func (k msgServer) TransferUnits(ctx context.Context, msg *types.MsgTransferUnits) (*types.MsgTransferUnitsResponse, error) {
	sender, err := k.decodeAddr("sender", msg.Sender)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if err := checkBatch(len(msg.Entries), p); err != nil {
		return nil, err
	}
	results := make([]types.UnitResult, 0, len(msg.Entries))
	for _, entry := range msg.Entries {
		c, err := k.getEditionsClass(ctx, entry.ClassId)
		if err != nil {
			return nil, err
		}
		if !c.Flags.Transferable {
			return nil, types.ErrNotTransferable
		}
		e, err := k.getEdition(ctx, c.Id, entry.EditionId)
		if err != nil {
			return nil, err
		}
		if entry.Amount == 0 {
			return nil, errorsmod.Wrap(types.ErrInvalidBatch, "amount must be positive")
		}
		if k.sameMember(ctx, entry.Recipient, msg.Sender) {
			return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "cannot transfer to yourself")
		}
		if _, err := k.validateRecipient("recipient", entry.Recipient); err != nil {
			return nil, err
		}
		res := types.UnitResult{Recipient: entry.Recipient, Amount: entry.Amount}
		// Units may sit under a predecessor address of the sender; the
		// pending record then names that holder so resolution finds them.
		holder, err := k.balanceHolder(ctx, c.Id, e.Id, msg.Sender)
		if err != nil {
			return nil, err
		}
		if !c.Flags.NeedsConsent() && k.deliversDirectly(ctx, msg.Sender, entry.Recipient) {
			if err := k.debit(ctx, &e, holder, entry.Amount, false); err != nil {
				return nil, err
			}
			if err := k.credit(ctx, &e, entry.Recipient, entry.Amount, sender, false, math.Int{}, "", ""); err != nil {
				return nil, err
			}
			res.Delivered = true
			emit(ctx, types.EventUnitsTransferred, classAttr(c.Id), editionAttr(e.Id),
				sdk.NewAttribute("from", msg.Sender), sdk.NewAttribute("to", entry.Recipient),
				sdk.NewAttribute("amount", u64(entry.Amount)), sdk.NewAttribute("via", "transfer"))
		} else {
			if err := k.lockUnits(ctx, &e, holder, entry.Amount, false); err != nil {
				return nil, err
			}
			id, err := k.parkUnits(ctx, types.InboxKind_INBOX_KIND_UNIT_TRANSFER, c, &e, sender, holder, entry.Recipient, entry.Amount, holder, p)
			if err != nil {
				return nil, err
			}
			res.PendingId = id
		}
		if err := k.saveEdition(ctx, e); err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	return &types.MsgTransferUnitsResponse{Results: results}, nil
}
