package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/forum/types"
)

// applyPostMediaLabels recomputes a post's chain-owned media labels from its
// current content and content type. Call it wherever Content, ContentType or
// a deletion status change (create, edit, delete, tombstone, genesis import);
// labels are never author-set. A deleted post's content is a placeholder the
// chain wrote ("[deleted]" or ""), never media, whatever its original
// content_type. See docs/content-scanning.md §3.
func applyPostMediaLabels(post *types.Post) {
	l := commontypes.LabelBody(post.ContentType, post.Content)
	if post.Status == types.PostStatus_POST_STATUS_DELETED {
		l = commontypes.LabelTombstone(post.Content)
	}
	post.MediaFlags, post.MediaRulesVersion, post.BodyHash = l.Flags, l.RulesVersion, l.BodyHash
}

// withholdPostContent blanks the content of a media-flagged post for list and
// show queries. Clients fetch it through PostContent once scanner verdicts
// clear it.
func withholdPostContent(post types.Post) types.Post {
	if post.MediaFlags != 0 {
		post.Content = ""
	}
	return post
}

// checkMediaWrite enforces the media posting rules on a write labelled as
// media and charges media_scan_fee (burned). Plain text passes untouched.
// authorBond is the DREAM bond attached to a create, nil on edits.
// See docs/content-scanning.md §9.
func (k Keeper) checkMediaWrite(ctx context.Context, creator string, creatorAddr sdk.AccAddress, flags uint32, authorBond *math.Int, params types.Params) error {
	if flags == 0 {
		return nil
	}
	gate := commontypes.MediaGate{
		Flags:     flags,
		Anonymous: creatorAddr.Equals(shieldModuleAddress),
		MinTrust:  params.MediaMinTrustLevel,
		BondMin:   params.MediaAuthorBondMin,
	}
	if !gate.Anonymous && k.IsMember(ctx, creator) {
		gate.Member = true
		gate.TrustLevel = uint32(k.GetTrustLevel(ctx, creator))
	}
	if authorBond != nil {
		gate.AuthorBond = *authorBond
	}
	if reason := commontypes.CheckMediaPermitted(gate); reason != "" {
		return errorsmod.Wrap(types.ErrMediaNotPermitted, reason)
	}

	if params.MediaScanFee.IsNil() || !params.MediaScanFee.IsPositive() {
		return nil
	}
	fee := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), params.MediaScanFee))
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creatorAddr, types.ModuleName, fee); err != nil {
		return errorsmod.Wrap(err, "failed to charge media scan fee")
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, fee); err != nil {
		return errorsmod.Wrap(err, "failed to burn media scan fee")
	}
	return nil
}
