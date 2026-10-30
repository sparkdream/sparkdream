package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
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

	// A council or committee policy (an executed proposal) creates tags
	// without the member gates and may use the registry headroom members
	// cannot fill, so a registry full of member tags still leaves governance
	// room to add one (REP-E-09).
	councilBody := k.isCouncilBodyPolicy(ctx, msg.Creator)

	capacity := types.DefaultMaxTotalTags
	if !councilBody {
		capacity -= types.DefaultMaxTotalTags * types.CouncilTagHeadroomBps / 10000
	}
	// The registry counter, not a walk over up to DefaultMaxTotalTags entries.
	if k.GetTagCount(ctx) >= capacity {
		return nil, errorsmod.Wrapf(types.ErrTagLimitExceeded,
			"tag registry at capacity %d", capacity)
	}

	if !councilBody {
		member, err := k.Member.Get(ctx, msg.Creator)
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrMemberNotFound, msg.Creator)
		}
		if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
			return nil, errorsmod.Wrap(types.ErrMemberNotActive, msg.Creator)
		}
		if member.TrustLevel < types.TrustLevel_TRUST_LEVEL_ESTABLISHED {
			return nil, errorsmod.Wrapf(types.ErrInsufficientTrustLevel,
				"tag creation requires trust level >= ESTABLISHED (%d), got %d",
				types.TrustLevel_TRUST_LEVEL_ESTABLISHED, member.TrustLevel)
		}
		if err := k.chargeTagCreationWindow(ctx, msg.Creator); err != nil {
			return nil, err
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
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	// GC keys off `last_used_at + DefaultTagExpiration` (see ExpireTags).
	tag := types.Tag{
		Name:       name,
		CreatedAt:  now,
		LastUsedAt: now,
	}
	if err := k.SetTag(ctx, tag); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store tag")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"tag_created",
		sdk.NewAttribute("name", name),
		sdk.NewAttribute("creator", msg.Creator),
		sdk.NewAttribute("expires_at", fmt.Sprintf("%d", now+types.DefaultTagExpiration)),
	))

	return &types.MsgCreateTagResponse{Name: name}, nil
}

// chargeTagCreationWindow counts one tag creation against the member's
// per-epoch limit (types.MaxTagCreationsPerEpoch), keyed by the canonical
// address so a migration does not reset it.
func (k msgServer) chargeTagCreationWindow(ctx context.Context, creator string) error {
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	key := k.Canonical(ctx, creator)
	count := uint64(0)
	if w, err := k.TagCreationWindow.Get(ctx, key); err == nil && w.K1() == epoch {
		count = w.K2()
	}
	if count >= types.MaxTagCreationsPerEpoch {
		return errorsmod.Wrapf(types.ErrTagCreationRateLimited,
			"%d tags created this epoch, at most %d", count, types.MaxTagCreationsPerEpoch)
	}
	return k.TagCreationWindow.Set(ctx, key, collections.Join(epoch, count+1))
}
