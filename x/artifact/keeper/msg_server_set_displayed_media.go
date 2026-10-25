package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// SetDisplayedMedia picks which accepted version the token shows: 0 = the
// stored metadata (or schedule), n = media_versions[n-1].
func (k msgServer) SetDisplayedMedia(ctx context.Context, msg *types.MsgSetDisplayedMedia) (*types.MsgSetDisplayedMediaResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getClass(ctx, msg.ClassId)
	if err != nil {
		return nil, err
	}
	t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
	if err != nil {
		return nil, err
	}
	if !k.canActAs(ctx, msg.Owner, t.Owner) {
		return nil, types.ErrNotTokenOwner
	}
	if isTargetHidden(c, t) {
		return nil, types.ErrContentHidden
	}
	if int(msg.Version) > len(t.MediaVersions) {
		return nil, errorsmod.Wrapf(types.ErrInvalidMediaVersion, "token has %d versions", len(t.MediaVersions))
	}
	t.DisplayedVersion = msg.Version
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	emit(ctx, types.EventDisplayedMediaSet, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("version", u64(uint64(msg.Version))))
	return &types.MsgSetDisplayedMediaResponse{}, nil
}
