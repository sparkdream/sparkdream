package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) SetMaxSupply(ctx context.Context, msg *types.MsgSetMaxSupply) (*types.MsgSetMaxSupplyResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.MintingClosed {
		return nil, types.ErrMintingClosed
	}
	if msg.MaxSupply == 0 {
		return nil, errorsmod.Wrap(types.ErrSupplyIncrease, "a cap cannot be removed")
	}
	if c.MaxSupply != 0 && msg.MaxSupply >= c.MaxSupply {
		return nil, errorsmod.Wrapf(types.ErrSupplyIncrease, "new cap must be below %d", c.MaxSupply)
	}
	if msg.MaxSupply < issued(c) {
		return nil, errorsmod.Wrapf(types.ErrSupplyExceeded, "%d tokens already issued", issued(c))
	}
	old := c.MaxSupply
	c.MaxSupply = msg.MaxSupply
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMaxSupplySet, classAttr(c.Id),
		sdk.NewAttribute("old", u64(old)), sdk.NewAttribute("new", u64(c.MaxSupply)))
	return &types.MsgSetMaxSupplyResponse{}, nil
}
