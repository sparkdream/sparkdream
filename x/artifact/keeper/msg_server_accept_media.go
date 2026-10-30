package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// AcceptMedia moves a proposal into the token's accepted media versions and
// optionally makes it the displayed one (docs/x-artifact-spec.md §7.17).
func (k msgServer) AcceptMedia(ctx context.Context, msg *types.MsgAcceptMedia) (*types.MsgAcceptMediaResponse, error) {
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
	key := collections.Join3(t.ClassId, t.Id, msg.ProposalId)
	mp, err := k.MediaProposals.Get(ctx, key)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrMediaProposalNotFound, "%d/%d #%d", t.ClassId, t.Id, msg.ProposalId)
	}
	if commontypes.Saturate[uint32](len(t.MediaVersions)) >= k.GetParams(ctx).MaxMediaVersionsPerToken {
		return nil, types.ErrTooManyMediaVersions
	}
	if err := k.MediaProposals.Remove(ctx, key); err != nil {
		return nil, err
	}
	t.MediaVersions = append(t.MediaVersions, mp.Metadata)
	version := commontypes.Saturate[uint32](len(t.MediaVersions))
	if msg.Display {
		t.DisplayedVersion = version
	}
	t.MediaFlags = types.LabelTokenAll(t)
	if err := k.saveToken(ctx, t); err != nil {
		return nil, err
	}
	emit(ctx, types.EventMediaAccepted, classAttr(t.ClassId), tokenAttr(t.Id),
		sdk.NewAttribute("proposal_id", u64(mp.Id)), sdk.NewAttribute("version", u64(uint64(version))),
		sdk.NewAttribute("displayed", u64(uint64(t.DisplayedVersion))))
	return &types.MsgAcceptMediaResponse{Version: version}, nil
}
