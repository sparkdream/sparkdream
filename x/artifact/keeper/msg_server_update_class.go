package keeper

import (
	"context"
	"strings"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

var contentFields = map[string]bool{
	"name": true, "description": true, "uri": true, "uri_hash": true, "token_uri_base": true,
}

var routingFields = map[string]bool{
	"payout_address": true, "royalty_recipients": true, "royalty_bps": true,
}

func (k msgServer) UpdateClass(ctx context.Context, msg *types.MsgUpdateClass) (*types.MsgUpdateClassResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	if len(msg.UpdateMask) == 0 {
		return nil, errorsmod.Wrap(types.ErrInvalidMetadata, "update_mask is empty")
	}
	p := k.GetParams(ctx)
	mask := map[string]bool{}
	for _, f := range msg.UpdateMask {
		if !contentFields[f] && !routingFields[f] {
			return nil, errorsmod.Wrapf(types.ErrInvalidMetadata, "unknown update_mask field %q", f)
		}
		if contentFields[f] && c.MetadataFrozen {
			return nil, errorsmod.Wrapf(types.ErrMetadataFrozen, "field %q", f)
		}
		mask[f] = true
	}

	if mask["name"] {
		c.Name = msg.Name
	}
	if mask["description"] {
		c.Description = msg.Description
	}
	if mask["uri"] {
		c.Uri = msg.Uri
	}
	if mask["uri_hash"] {
		c.UriHash = msg.UriHash
	}
	if mask["token_uri_base"] {
		c.TokenUriBase = msg.TokenUriBase
	}
	if err := validateClassContent(c.Name, c.Symbol, c.Description, c.Uri, c.UriHash, c.TokenUriBase, p); err != nil {
		return nil, err
	}
	if mask["payout_address"] {
		if _, err := k.validateRecipient("payout_address", msg.PayoutAddress); err != nil {
			return nil, err
		}
		c.PayoutAddress = msg.PayoutAddress
	}
	if mask["royalty_recipients"] {
		if err := k.validateRoyaltyShares(msg.RoyaltyRecipients); err != nil {
			return nil, err
		}
		c.RoyaltyRecipients = msg.RoyaltyRecipients
	}
	if mask["royalty_bps"] {
		if msg.RoyaltyBps > c.RoyaltyBps {
			return nil, types.ErrRoyaltyIncrease
		}
		c.RoyaltyBps = msg.RoyaltyBps
	}
	c.MediaFlags = types.LabelClass(c)
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventClassUpdated, classAttr(c.Id), sdk.NewAttribute("fields", strings.Join(msg.UpdateMask, ",")))
	return &types.MsgUpdateClassResponse{}, nil
}
