package keeper

import (
	"context"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) SetMinters(ctx context.Context, msg *types.MsgSetMinters) (*types.MsgSetMintersResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if len(msg.Add) == 0 && len(msg.Remove) == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidBatch, "nothing to add or remove")
	}
	remove := map[string]bool{}
	for _, r := range msg.Remove {
		remove[r] = true
	}
	next := make([]string, 0, len(c.Minters)+len(msg.Add))
	for _, m := range c.Minters {
		if !remove[m] {
			next = append(next, m)
		}
	}
	next = append(next, msg.Add...)
	minters, err := k.validateMinterSet(next, c.Owner, k.GetParams(ctx))
	if err != nil {
		return nil, err
	}
	c.Minters = minters
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMintersSet, classAttr(c.Id),
		sdk.NewAttribute("added", strings.Join(msg.Add, ",")),
		sdk.NewAttribute("removed", strings.Join(msg.Remove, ",")))
	return &types.MsgSetMintersResponse{}, nil
}
