package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/blog/types"
	commontypes "sparkdream/x/common/types"
)

// applyPostMediaLabels recomputes a post's chain-owned media labels from its
// current body and content type. Call it wherever Body or ContentType change
// (create, update, tombstone, genesis import); labels are never author-set.
// See docs/content-scanning.md §3.
func applyPostMediaLabels(post *types.Post) {
	l := commontypes.LabelBody(post.ContentType, post.Body)
	post.MediaFlags, post.MediaRulesVersion, post.BodyHash = l.Flags, l.RulesVersion, l.BodyHash
}

// applyReplyMediaLabels is applyPostMediaLabels for replies.
func applyReplyMediaLabels(reply *types.Reply) {
	l := commontypes.LabelBody(reply.ContentType, reply.Body)
	reply.MediaFlags, reply.MediaRulesVersion, reply.BodyHash = l.Flags, l.RulesVersion, l.BodyHash
}

// withholdPostBody blanks the body of a media-flagged post for list and show
// queries. Clients fetch it through PostBody once scanner verdicts clear it.
func withholdPostBody(post types.Post) types.Post {
	if post.MediaFlags != 0 {
		post.Body = ""
	}
	return post
}

// withholdReplyBody is withholdPostBody for replies (fetched via ReplyBody).
func withholdReplyBody(reply types.Reply) types.Reply {
	if reply.MediaFlags != 0 {
		reply.Body = ""
	}
	return reply
}

// checkMediaWrite enforces the media posting rules on a write labelled as
// media and charges media_scan_fee (burned). Plain text passes untouched.
// authorBond is the DREAM bond attached to a create, nil on updates.
// See docs/content-scanning.md §9.
func (k Keeper) checkMediaWrite(ctx context.Context, creator sdk.AccAddress, flags uint32, authorBond *math.Int, params types.Params) error {
	if flags == 0 {
		return nil
	}
	gate := commontypes.MediaGate{
		Flags:     flags,
		Anonymous: isShieldModuleAddress(creator),
		MinTrust:  params.MediaMinTrustLevel,
		BondMin:   params.MediaAuthorBondMin,
	}
	if !gate.Anonymous && k.repKeeper != nil && k.repKeeper.IsActiveMember(ctx, creator) {
		gate.Member = true
		if tl, err := k.repKeeper.GetTrustLevel(ctx, creator); err == nil {
			gate.TrustLevel = uint32(tl)
		}
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
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, creator, types.ModuleName, fee); err != nil {
		return errorsmod.Wrap(err, "failed to charge media scan fee")
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, fee); err != nil {
		return errorsmod.Wrap(err, "failed to burn media scan fee")
	}
	return nil
}
