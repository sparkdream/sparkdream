package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) UpdateToken(ctx context.Context, msg *types.MsgUpdateToken) (*types.MsgUpdateTokenResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if !c.Flags.TokenMetadataMutable || c.MetadataFrozen {
		return nil, types.ErrMetadataFrozen
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	if err := types.ValidateTokenMetadata(msg.Metadata, k.GetParams(ctx)); err != nil {
		return nil, err
	}
	hash := types.MetadataHash(msg.Metadata)

	// A pending mint is updated in place; the recipient can pin the hash.
	key := collections.Join(msg.ClassId, msg.TokenId)
	if pm, err := k.PendingMints.Get(ctx, key); err == nil {
		pm.Metadata = msg.Metadata
		if err := k.PendingMints.Set(ctx, key, pm); err != nil {
			return nil, err
		}
	} else {
		t, err := k.getToken(ctx, msg.ClassId, msg.TokenId)
		if err != nil {
			return nil, err
		}
		if t.MetadataFrozen {
			return nil, types.ErrMetadataFrozen
		}
		if t.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
			return nil, types.ErrContentHidden
		}
		t.Metadata = msg.Metadata
		t.MediaFlags = types.LabelToken(t.Metadata)
		if err := k.saveToken(ctx, t); err != nil {
			return nil, err
		}
	}
	emit(ctx, types.EventTokenUpdated, classAttr(msg.ClassId), tokenAttr(msg.TokenId),
		sdk.NewAttribute("by", msg.Owner), sdk.NewAttribute("metadata_hash", hash))
	return &types.MsgUpdateTokenResponse{MetadataHash: hash}, nil
}
