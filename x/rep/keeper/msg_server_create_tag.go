package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"
)

func (k msgServer) CreateTag(ctx context.Context, msg *types.MsgCreateTag) (*types.MsgCreateTagResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	name := msg.Name
	if !commontypes.ValidateTagFormat(name) {
		return nil, errorsmod.Wrapf(types.ErrInvalidTagName, "tag %q does not match required format", name)
	}
	if !commontypes.ValidateTagLength(name, types.DefaultMaxTagLength) {
		return nil, errorsmod.Wrapf(types.ErrInvalidTagName, "tag %q exceeds max length %d", name, types.DefaultMaxTagLength)
	}

	exists, err := k.Tag.Has(ctx, name)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check tag existence")
	}
	if exists {
		return nil, errorsmod.Wrapf(types.ErrTagAlreadyExists, "tag %q already exists", name)
	}

	reserved, err := k.ReservedTag.Has(ctx, name)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check reserved tag")
	}
	if reserved {
		return nil, errorsmod.Wrapf(types.ErrReservedTagName, "tag %q is reserved", name)
	}

	member, err := k.Member.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrMemberNotFound, msg.Creator)
	}
	if member.TrustLevel < types.TrustLevel_TRUST_LEVEL_ESTABLISHED {
		return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
			"tag creation requires trust level >= ESTABLISHED (%d), got %d",
			types.TrustLevel_TRUST_LEVEL_ESTABLISHED, member.TrustLevel)
	}

	var total uint64
	if err := k.Tag.Walk(ctx, nil, func(_ string, _ types.Tag) (bool, error) {
		total++
		if total >= types.DefaultMaxTotalTags {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, errorsmod.Wrap(err, "failed to count tags")
	}
	if total >= types.DefaultMaxTotalTags {
		return nil, errorsmod.Wrapf(types.ErrTagLimitExceeded,
			"tag registry at capacity %d", types.DefaultMaxTotalTags)
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}
	if params.TagCreationFee.IsPositive() {
		if err := k.BurnDREAM(ctx, creatorAddr, params.TagCreationFee); err != nil {
			return nil, errorsmod.Wrapf(err, "failed to burn tag creation fee of %s DREAM", params.TagCreationFee.String())
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	tag := types.Tag{
		Name:            name,
		CreatedAt:       now,
		LastUsedAt:      now,
		ExpirationIndex: now + types.DefaultTagExpiration,
	}
	if err := k.SetTag(ctx, tag); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store tag")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"tag_created",
		sdk.NewAttribute("name", name),
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("expiration_index", fmt.Sprintf("%d", tag.ExpirationIndex)),
	))

	return &types.MsgCreateTagResponse{Name: name}, nil
}
