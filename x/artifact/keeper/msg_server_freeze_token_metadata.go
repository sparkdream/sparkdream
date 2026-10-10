package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) FreezeTokenMetadata(ctx context.Context, msg *types.MsgFreezeTokenMetadata) (*types.MsgFreezeTokenMetadataResponse, error) {
	if _, err := k.decodeAddr("signer", msg.Signer); err != nil {
		return nil, err
	}
	if err := checkBatch(len(msg.Refs), k.GetParams(ctx)); err != nil {
		return nil, err
	}
	refs := make([]U64Pair, len(msg.Refs))
	for i, r := range msg.Refs {
		refs[i] = collections.Join(r.ClassId, r.TokenId)
	}
	if err := checkNoDuplicateRefs(refs); err != nil {
		return nil, err
	}
	for _, r := range msg.Refs {
		c, err := k.getClass(ctx, r.ClassId)
		if err != nil {
			return nil, err
		}
		t, err := k.getToken(ctx, r.ClassId, r.TokenId)
		if err != nil {
			return nil, err
		}
		// The class owner or the holder may freeze (§5.3.7).
		if c.Owner != msg.Signer && t.Owner != msg.Signer {
			return nil, types.ErrNotTokenOwner
		}
		if t.MetadataFrozen {
			return nil, types.ErrMetadataFrozen
		}
		t.MetadataFrozen = true
		if err := k.saveToken(ctx, t); err != nil {
			return nil, err
		}
		emit(ctx, types.EventTokenMetadataFrozen, classAttr(t.ClassId), tokenAttr(t.Id),
			sdk.NewAttribute("by", msg.Signer), sdk.NewAttribute("metadata_hash", types.MetadataHash(t.Metadata)))
	}
	return &types.MsgFreezeTokenMetadataResponse{}, nil
}
