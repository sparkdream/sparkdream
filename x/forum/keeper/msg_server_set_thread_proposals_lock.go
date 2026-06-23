package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/forum/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetThreadProposalsLock lets the thread author open or close their thread to
// sentinel accepted-reply proposals. Locking gives the author a durable "this
// thread accepts no curated answer" state — for an open discussion, or a thread
// with no good reply — without being forced into an irreversible acceptance.
// Locking also supersedes any pending proposal. Author-only.
func (k msgServer) SetThreadProposalsLock(ctx context.Context, msg *types.MsgSetThreadProposalsLock) (*types.MsgSetThreadProposalsLockResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Load thread root and verify it is a root post owned by the caller.
	thread, err := k.Post.Get(ctx, msg.ThreadId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPostNotFound, fmt.Sprintf("thread %d not found", msg.ThreadId))
	}
	if thread.ParentId != 0 {
		return nil, errorsmod.Wrap(types.ErrNotRootPost, "thread_id must be a root post")
	}
	if thread.Author != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrNotThreadAuthor, "only the thread author can lock proposals")
	}

	// Get or create thread metadata.
	metadata, err := k.ThreadMetadata.Get(ctx, msg.ThreadId)
	if err != nil {
		metadata = types.ThreadMetadata{
			ThreadId:       msg.ThreadId,
			PinnedReplyIds: []uint64{},
			PinnedRecords:  []*types.PinnedReplyRecord{},
		}
	}

	metadata.ProposalsLocked = msg.Locked

	// Locking supersedes any pending sentinel proposal (the author has decided no
	// curation should happen). Unlocking leaves state otherwise untouched.
	var superseded string
	if msg.Locked {
		superseded = k.clearPendingProposal(ctx, &metadata)
	}

	if err := k.ThreadMetadata.Set(ctx, msg.ThreadId, metadata); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update thread metadata")
	}

	eventType := "thread_proposals_unlocked"
	if msg.Locked {
		eventType = "thread_proposals_locked"
	}
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			eventType,
			sdk.NewAttribute("thread_id", fmt.Sprintf("%d", msg.ThreadId)),
			sdk.NewAttribute("creator", msg.Creator),
		),
	)
	k.emitProposalSuperseded(ctx, msg.ThreadId, superseded, &metadata)

	return &types.MsgSetThreadProposalsLockResponse{}, nil
}
