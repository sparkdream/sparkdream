package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/artifact/types"
)

func (k msgServer) Mint(ctx context.Context, msg *types.MsgMint) (*types.MsgMintResponse, error) {
	minter, err := k.decodeAddr("minter", msg.Minter)
	if err != nil {
		return nil, err
	}
	p := k.GetParams(ctx)
	if err := checkBatch(len(msg.Entries), p); err != nil {
		return nil, err
	}
	c, err := k.mintPreconditions(ctx, msg.ClassId, msg.Minter, msg.AcceptedContentLicense, uint64(len(msg.Entries)))
	if err != nil {
		return nil, err
	}
	if c.IsEditions() {
		return nil, errorsmod.Wrap(types.ErrWrongTokenModel, "editions classes mint with MsgCreateEdition and MsgMintUnits")
	}

	results := make([]types.MintResult, 0, len(msg.Entries))
	for _, e := range msg.Entries {
		recipient := e.Recipient
		if recipient == "" {
			recipient = msg.Minter
		}
		if err := validateMintContent(ctx, c, e.Metadata, e.Schedule, p); err != nil {
			return nil, err
		}
		res, err := k.mintOne(ctx, &c, minter, msg.Minter, recipient, e.Metadata, e.Schedule, p)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	return &types.MsgMintResponse{Results: results}, nil
}
