package keeper

import (
	"context"
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) SetMintPolicy(ctx context.Context, msg *types.MsgSetMintPolicy) (*types.MsgSetMintPolicyResponse, error) {
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
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	if err := k.validateMintPolicy(ctx, msg.MintPolicy, c.Flags); err != nil {
		return nil, err
	}
	c.MintPolicy = k.normalizePolicy(ctx, msg.MintPolicy)
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMintPolicySet, classAttr(c.Id),
		sdk.NewAttribute("enabled", strconv.FormatBool(c.MintPolicy.PublicMintEnabled)),
		sdk.NewAttribute("price", c.MintPolicy.Price.String()),
		sdk.NewAttribute("start_time", i64(c.MintPolicy.StartTime)),
		sdk.NewAttribute("end_time", i64(c.MintPolicy.EndTime)),
		sdk.NewAttribute("per_address_limit", u64(uint64(c.MintPolicy.PerAddressLimit))))
	return &types.MsgSetMintPolicyResponse{}, nil
}
