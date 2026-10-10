package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// UnhideContent reverses a hide before any appeal: the hiding sentinel
// within sentinel_unhide_window_blocks (self-correct), or the Commons
// Operations authority at any time (council override, recorded as an
// overturn of the sentinel). The sentinel's committed bond is released.
func (k msgServer) UnhideContent(ctx context.Context, msg *types.MsgUnhideContent) (*types.MsgUnhideContentResponse, error) {
	if _, err := k.decodeAddr("authority", msg.Authority); err != nil {
		return nil, err
	}
	hr, err := k.HideRecords.Get(ctx, msg.HideId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrHideNotFound, "hide %d", msg.HideId)
	}
	if hr.Outcome != types.HideOutcome_HIDE_OUTCOME_PENDING || hr.Appealed {
		return nil, errorsmod.Wrap(types.ErrAppealWindowClosed, "hide is resolved or under appeal")
	}
	p := k.GetParams(ctx)
	selfCorrect := hr.Sentinel != "" && hr.Sentinel == msg.Authority &&
		height(ctx) <= hr.HiddenAt+p.SentinelUnhideWindowBlocks
	council := k.isOpsAuthority(ctx, msg.Authority)
	if !selfCorrect && !council {
		return nil, types.ErrNotAuthorized
	}
	if err := k.setTargetStatus(ctx, hr, types.ContentStatus_CONTENT_STATUS_ACTIVE); err != nil {
		return nil, err
	}
	k.releaseSentinelBond(ctx, hr)
	if !selfCorrect {
		k.recordSentinelOutcome(ctx, hr, false)
	}
	if err := k.closeHide(ctx, hr, types.HideOutcome_HIDE_OUTCOME_UNHIDDEN); err != nil {
		return nil, err
	}
	emit(ctx, types.EventUnhidden, sdk.NewAttribute("hide_id", u64(hr.Id)), sdk.NewAttribute("authority", msg.Authority))
	return &types.MsgUnhideContentResponse{}, nil
}
