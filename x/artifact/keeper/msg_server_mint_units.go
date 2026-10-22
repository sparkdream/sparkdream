package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

// MintUnits mints units of an edition to 1..max_batch_size distinct
// recipients (class owner or listed minter). Each recipient's units follow
// the receive-policy rules of MsgMint; a new balance record costs the
// minter token_deposit.
func (k msgServer) MintUnits(ctx context.Context, msg *types.MsgMintUnits) (*types.MsgMintUnitsResponse, error) {
	minter, err := k.decodeAddr("minter", msg.Minter)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if err := checkBatch(len(msg.Entries), p); err != nil {
		return nil, err
	}
	c, err := k.getEditionsClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	if err := mintable(c); err != nil {
		return nil, err
	}
	if !isMinter(c, msg.Minter) {
		return nil, types.ErrNotMinter
	}
	e, err := k.getEdition(ctx, c.Id, msg.EditionId)
	if err != nil {
		return nil, err
	}
	if e.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	var total uint64
	seen := map[string]bool{}
	for i := range msg.Entries {
		if msg.Entries[i].Recipient == "" {
			msg.Entries[i].Recipient = msg.Minter
		}
		if seen[msg.Entries[i].Recipient] {
			return nil, errorsmod.Wrapf(types.ErrInvalidBatch, "duplicate recipient %s", msg.Entries[i].Recipient)
		}
		seen[msg.Entries[i].Recipient] = true
		total += msg.Entries[i].Amount
		if msg.Entries[i].Amount == 0 || total < msg.Entries[i].Amount {
			return nil, errorsmod.Wrap(types.ErrInvalidBatch, "amounts must be positive")
		}
	}
	if err := checkUnitSupply(e, total); err != nil {
		return nil, err
	}
	results := make([]types.UnitResult, 0, len(msg.Entries))
	for _, entry := range msg.Entries {
		res, err := k.mintUnitsTo(ctx, c, &e, minter, msg.Minter, entry.Recipient, entry.Amount, p)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if err := k.saveEdition(ctx, e); err != nil {
		return nil, err
	}
	return &types.MsgMintUnitsResponse{Results: results}, nil
}
